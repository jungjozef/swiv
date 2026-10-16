"""Render an Amiga frame from the emulated custom-chip state.

Runs the copper list (COP1LC) line by line, tracking the registers that
affect the picture: bitplane pointers and modulos, BPLCON0/1/2, the display
window, the 32 colour registers and the 8 hardware sprites.  Supports normal
and EHB playfields; HAM and dual-playfield are not implemented (reported).

    from render import render_frame, save_png
"""
import struct
import zlib

import numpy as np

CUSTOM = 0xDFF000
VLINES = 312


def save_png(path, rgb):
    """Write an HxWx3 uint8 array as PNG without external libraries."""
    h, w, _ = rgb.shape
    raw = b''.join(b'\x00' + rgb[y].tobytes() for y in range(h))

    def chunk(tag, data):
        c = struct.pack('>I', len(data)) + tag + data
        return c + struct.pack('>I', zlib.crc32(tag + data) & 0xFFFFFFFF)

    png = b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', w, h, 8, 2, 0, 0, 0))
    png += chunk(b'IDAT', zlib.compress(raw, 6)) + chunk(b'IEND', b'')
    open(path, 'wb').write(png)


class State:
    def __init__(self, am):
        rw = lambda r: am.rw(CUSTOM + r)
        self.bplcon0, self.bplcon1, self.bplcon2 = rw(0x100), rw(0x102), rw(0x104)
        self.mod1, self.mod2 = rw(0x108), rw(0x10A)
        self.ddfstrt, self.ddfstop = rw(0x92), rw(0x94)
        self.diwstrt, self.diwstop = rw(0x8E), rw(0x90)
        self.bpl = [(rw(0xE0 + 4 * i) << 16) | rw(0xE2 + 4 * i) for i in range(6)]
        self.spr = [(rw(0x120 + 4 * i) << 16) | rw(0x122 + 4 * i) for i in range(8)]
        self.col = [rw(0x180 + 2 * i) & 0xFFF for i in range(32)]
        self.cop = (rw(0x80) << 16) | rw(0x82)
        self.events = []        # (line, sprite index, pointer) from the copper
        self.hi = {}            # pending high halves of pointer registers

    def write(self, reg, val, line):
        if reg == 0x100: self.bplcon0 = val
        elif reg == 0x102: self.bplcon1 = val
        elif reg == 0x104: self.bplcon2 = val
        elif reg == 0x108: self.mod1 = val
        elif reg == 0x10A: self.mod2 = val
        elif reg == 0x92: self.ddfstrt = val
        elif reg == 0x94: self.ddfstop = val
        elif reg == 0x8E: self.diwstrt = val
        elif reg == 0x90: self.diwstop = val
        elif 0xE0 <= reg < 0xF8:
            i, half = (reg - 0xE0) // 4, (reg & 2)
            p = self.bpl[i]
            self.bpl[i] = (val << 16 | (p & 0xFFFF)) if not half else ((p & 0xFFFF0000) | val)
        elif 0x120 <= reg < 0x140:
            i, half = (reg - 0x120) // 4, (reg & 2)
            p = self.spr[i]
            p = (val << 16 | (p & 0xFFFF)) if not half else ((p & 0xFFFF0000) | val)
            self.spr[i] = p
            if half:
                self.events.append((line, i, p))
        elif 0x180 <= reg < 0x1C0:
            self.col[(reg - 0x180) // 2] = val & 0xFFF


def render_frame(am, notes=None):
    """Return (rgb HxWx3 uint8 array of the display window, info dict)."""
    notes = notes if notes is not None else []
    st = State(am)
    n_planes = lambda: (st.bplcon0 >> 12) & 7
    ddf_words = lambda: ((st.ddfstop - st.ddfstrt) >> 3) + 1 + (1 if st.bplcon1 & 0xFF else 0)

    idx = np.zeros((VLINES, 384), dtype=np.uint8)       # playfield colour index per line
    pals = np.zeros((VLINES, 32), dtype=np.uint16)       # palette per line
    ehb = False

    def render_lines(a, b):
        nonlocal ehb
        vs = (st.diwstrt >> 8) & 0xFF
        ve = ((st.diwstop >> 8) & 0xFF) | (0x100 if not (st.diwstop & 0x8000) else 0)
        np_ = min(n_planes(), 6)
        hires = bool(st.bplcon0 & 0x8000)
        if st.bplcon0 & 0x0800 and np_ == 6:
            notes.append('HAM not supported')
        if st.bplcon0 & 0x0400:
            notes.append('dual playfield not supported')
        wbytes = ddf_words() * 2
        if wbytes <= 0 or wbytes > 96:
            for line in range(a, min(b, VLINES)):
                pals[line] = st.col
            return
        for line in range(a, min(b, VLINES)):
            pals[line] = st.col
            if not (vs <= line < ve) or np_ == 0 or hires:
                continue
            acc = np.zeros(wbytes * 8, dtype=np.uint8)
            for p in range(np_):
                addr = st.bpl[p]
                row = np.frombuffer(am.rb(addr, wbytes), dtype=np.uint8)
                acc |= (np.unpackbits(row) << p).astype(np.uint8)
                st.bpl[p] = (addr + wbytes + (st.mod1 if p % 2 == 0 else st.mod2)) & 0xFFFFFFFF
            if np_ == 6:
                ehb = True
            sh = (st.bplcon1 & 0xF) if st.bplcon1 else 0
            if sh:
                acc = np.concatenate([np.zeros(sh, dtype=np.uint8), acc])[:wbytes * 8]
            w = min(len(acc), 384)
            idx[line, :w] = acc[:w]

    bpl_start = list(st.bpl)
    # ---- run the copper ----
    line = 0
    wrapped = False
    pc = st.cop
    steps = 0
    mem = lambda a: am.rw(a)
    while pc and steps < 20000:
        steps += 1
        ir1, ir2 = mem(pc), mem(pc + 2)
        pc += 4
        if not (ir1 & 1):                         # MOVE
            st.write(ir1 & 0x1FE, ir2, line)
        elif not (ir2 & 1):                       # WAIT
            vp = (ir1 >> 8) & 0xFF
            vm = ((ir2 >> 8) & 0x7F) | 0x80
            if ir1 == 0xFFFF and ir2 == 0xFFFE:
                break
            target = line
            if wrapped:
                # past line 255 the 8-bit vertical position has wrapped: the
                # game follows `WAIT 255,$E0` with waits for lines 256+vp.
                target = max(line, 256 + (vp & vm & 0x7F))
            else:
                while target < VLINES and (target & vm) < (vp & vm):
                    target += 1
                if vp == 255 and (ir1 & 0xFE) >= 0xE0:
                    wrapped = True
            if target > line:
                render_lines(line, target)
                line = target
        else:                                     # SKIP: assume not skipped
            pass
    render_lines(line, VLINES)

    # ---- window ----
    vs = (st.diwstrt >> 8) & 0xFF
    ve = ((st.diwstop >> 8) & 0xFF) | (0x100 if not (st.diwstop & 0x8000) else 0)
    hs = st.diwstrt & 0xFF
    he = (st.diwstop & 0xFF) | 0x100
    width = max(1, min(384, he - hs))
    height = max(1, min(VLINES, ve - vs))

    # ---- colour lookup (with EHB) ----
    out_idx = idx[vs:vs + height, :width].astype(np.int32)
    pal = pals[vs:vs + height].astype(np.int32)
    if ehb:
        half = out_idx >= 32
        base = out_idx & 31
        c = np.take_along_axis(pal, base, axis=1)
        r, g, b = (c >> 8) & 15, (c >> 4) & 15, c & 15
        r = np.where(half, r >> 1, r); g = np.where(half, g >> 1, g); b = np.where(half, b >> 1, b)
    else:
        c = np.take_along_axis(pal, out_idx & 31, axis=1)
        r, g, b = (c >> 8) & 15, (c >> 4) & 15, c & 15
    rgb = np.stack([r, g, b], axis=-1).astype(np.uint8) * 17

    # ---- sprites ----
    nspr = _draw_sprites(am, st, rgb, pals, vs, hs)

    info = dict(sprite_log=list(_draw_sprites.log), bpl_start=bpl_start, idx=out_idx, pals=pal, planes=n_planes(), window=(hs, vs, width, height), sprites=nspr,
                bplcon0=st.bplcon0, copper=st.cop, ehb=ehb)
    return rgb, info


def _draw_sprites(am, st, rgb, pals, vs, hs):
    h, w, _ = rgb.shape
    events = [(0, i, p) for i, p in enumerate(st.spr)] + st.events
    drawn = 0
    seen = set()
    _draw_sprites.log = []
    for line0, n, ptr in events:
        if not ptr or ptr >= 0x100000 or (n, ptr) in seen:
            continue
        seen.add((n, ptr))
        p = ptr
        for _ in range(64):
            w0, w1 = am.rw(p), am.rw(p + 2)
            if w0 == 0 and w1 == 0:
                break
            vstart = (w0 >> 8) | (0x100 if w1 & 4 else 0)
            vstop = (w1 >> 8) | (0x100 if w1 & 2 else 0)
            hstart = ((w0 & 0xFF) << 1) | (w1 & 1)
            attach = bool(w1 & 0x80)
            nlines = max(0, vstop - vstart)
            p += 4
            for k in range(nlines):
                a, b = am.rw(p), am.rw(p + 2)
                p += 4
                y = vstart + k - vs
                if not (0 <= y < h) or vstart + k < line0:
                    continue
                for bit in range(16):
                    x = hstart + bit - hs
                    if not (0 <= x < w):
                        continue
                    v = ((a >> (15 - bit)) & 1) | (((b >> (15 - bit)) & 1) << 1)
                    if v == 0:
                        continue
                    pal = pals[vs + y]
                    col = int(pal[16 + 4 * (n // 2) + v])
                    rgb[y, x] = [((col >> 8) & 15) * 17, ((col >> 4) & 15) * 17, (col & 15) * 17]
            drawn += 1
            _draw_sprites.log.append((n, vstart, hstart, nlines, attach))
    return drawn
