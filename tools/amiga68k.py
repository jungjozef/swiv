"""Amiga-ish 68000 harness on Musashi (machine68k).

Replaces tools/amiemu.py (Unicorn): Musashi keeps condition codes exact across
stops and uses real 68000 exception frames.  Provides RAM, custom/CIA register
pages, a fake ExecBase whose vectors trap into Python, a floppy drive that
serves standard MFM tracks from an ADF, and VBlank interrupts.
"""
import struct
import sys
import os

import machine68k as m

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from mfm import encode_track

REG = {n.lower(): getattr(m.Register, n) for n in
       ['D0', 'D1', 'D2', 'D3', 'D4', 'D5', 'D6', 'D7',
        'A0', 'A1', 'A2', 'A3', 'A4', 'A5', 'A6', 'A7']}

RAM_KIB = 16 * 1024
CHIP = 0x100000              # chip RAM the "machine" reports
EXEC = 0xF00800              # fake ExecBase (its vectors live below it)
STUB = 0xF80000
CYCLES_PER_FRAME = 312 * 454  # PAL
CYCLES_PER_LINE = 454

EXEC_LVO = {
    -0xc6: 'AllocMem', -0xcc: 'AllocAbs', -0xd2: 'FreeMem', -0xd8: 'AvailMem',
    -0x1bc: 'OpenDevice', -0x1c2: 'CloseDevice', -0x1c8: 'DoIO',
    -0x1ce: 'SendIO', -0x1d4: 'CheckIO', -0x1da: 'WaitIO', -0x216: 'TypeOfMem',
    -0x84: 'Forbid', -0x8a: 'Permit', -0x78: 'Disable', -0x7e: 'Enable',
}


class Amiga:
    def __init__(self, adf_path, verbose=True):
        self.adf = open(adf_path, 'rb').read()
        self.verbose = verbose
        self.mach = m.Machine(m.CPUType.M68000, RAM_KIB)
        self.cpu, self.mem = self.mach.cpu, self.mach.mem
        self.reads = []
        self.disk_reads = []
        self.calls = []
        self.intena = self.intreq = self.dmacon = self.adkcon = 0
        self.dsksync = 0x4489
        self.dskpt = 0
        self.dsklen_prev = 0
        self.cyl = 10
        self.prb = 0xFF
        self.cycles = 0
        self.fire0 = self.fire1 = False   # port 0/1 fire buttons (CIA-A PRA bits 6/7)
        self.joy0 = self.joy1 = 0         # JOY0DAT / JOY1DAT words
        self.frame_cycles = 0
        self.vbl_count = 0
        self.watch = {}            # pc -> callable(pc), run from the instruction hook
        self.stopped = None
        self.heap = 0xA0000
        self._pages()
        self._init_exec()
        self.ioreq = 0x2000

    # -- raw memory / registers --------------------------------------------
    def r(self, n): return self.cpu.r_reg(REG[n])
    def w(self, n, v): self.cpu.w_reg(REG[n], v & 0xFFFFFFFF)
    def rb(self, a, n): return bytes(self.mem.r_block(a, n))
    def wb(self, a, b): self.mem.w_block(a, bytes(b))
    def rl(self, a): return self.mem.r32(a)
    def wl(self, a, v): self.mem.w32(a, v & 0xFFFFFFFF)
    def rw(self, a): return self.mem.r16(a)
    def ww(self, a, v): self.mem.w16(a, v & 0xFFFF)
    def log(self, *a):
        if self.verbose:
            print(*a)

    # -- memory mapped pages -------------------------------------------------
    def _pages(self):
        for page in (0xDF, 0xBF):
            base = page << 16
            for w in (0, 1, 2):
                self.mem.set_special_range_read_func(base, w, self._mk_read(w))
                self.mem.set_special_range_write_func(base, w, self._mk_write(w))

    def _mk_read(self, w):
        def f(addr):
            if w == 2:
                return (self._read(addr, 1) << 16) | self._read(addr + 2, 1)
            return self._read(addr, w)
        return f

    def _mk_write(self, w):
        def f(addr, val):
            if w == 2:
                self._write(addr, val >> 16, 1)
                self._write(addr + 2, val & 0xFFFF, 1)
            else:
                self._write(addr, val, w)
        return f

    @staticmethod
    def _setclr(cur, val):
        return (cur | (val & 0x7FFF)) if val & 0x8000 else (cur & ~(val & 0x7FFF))

    def _read(self, addr, w):
        if 0xDFF000 <= addr < 0xE00000:
            reg = addr & 0x1FE
            if reg == 0x1C: v = self.intena
            elif reg == 0x1E: v = self.intreq
            elif reg == 0x02: v = self.dmacon & ~0x4000      # blitter never busy
            elif reg == 0x0A: v = self.joy0
            elif reg == 0x0C: v = self.joy1
            elif reg in (0x04, 0x06):
                line = (self.frame_cycles // CYCLES_PER_LINE) % 312
                hpos = (self.frame_cycles % CYCLES_PER_LINE) // 2
                v = ((line >> 8) & 1) | 0x8000 if reg == 0x04 else ((line & 0xFF) << 8) | (hpos & 0xFF)
            else:
                v = self.mem.r16(reg + 0xDFF000)
            if w == 0:
                return (v >> 8) & 0xFF if not (addr & 1) else v & 0xFF
            return v & 0xFFFF
        # CIAs
        if addr == 0xBFE001:
            v = 0xFF & ~0x20                       # /RDY asserted
            if self.fire0:
                v &= ~0x40
            if self.fire1:
                v &= ~0x80
            if self.cyl == 0:
                v &= ~0x10                         # /TK0
            return v
        return self.mem.r8(addr) if w == 0 else self.mem.r16(addr)

    def _write(self, addr, val, w):
        if 0xDFF000 <= addr < 0xE00000:
            reg = addr & 0x1FE
            if w == 0:                              # byte write: merge into word
                cur = self.mem.r16(0xDFF000 + reg)
                val = ((val & 0xFF) << 8 | (cur & 0xFF)) if not (addr & 1) else ((cur & 0xFF00) | (val & 0xFF))
            val &= 0xFFFF
            if reg == 0x9A: self.intena = self._setclr(self.intena, val)
            elif reg == 0x9C: self.intreq = self._setclr(self.intreq, val)
            elif reg == 0x96: self.dmacon = self._setclr(self.dmacon, val)
            elif reg == 0x9E: self.adkcon = self._setclr(self.adkcon, val)
            elif reg == 0x7E: self.dsksync = val
            elif reg == 0x20: self.dskpt = (self.dskpt & 0xFFFF) | (val << 16)
            elif reg == 0x22: self.dskpt = (self.dskpt & 0xFFFF0000) | val
            elif reg == 0x24:
                if val & 0x8000 and self.dsklen_prev & 0x8000 and not (val & 0x4000):
                    self._disk_dma(val & 0x3FFF)
                    self.intreq |= 0x0002          # DSKBLK
                self.dsklen_prev = val
            self.mem.w16(0xDFF000 + reg, val)
            if reg == 0x58:
                self._blit(val)
            return
        if addr == 0xBFD100:
            old, new = self.prb, val & 0xFF
            self.prb = new
            if (new & 0x08) == 0 and (old & 1) == 0 and (new & 1) == 1:   # STEP rising edge
                self.cyl = max(0, self.cyl - 1) if new & 2 else min(81, self.cyl + 1)
        if w == 0:
            self.mem.w8(addr, val)
        else:
            self.mem.w16(addr, val)

    # -- blitter ----------------------------------------------------------------
    def _blit(self, size):
        mem = self.mem
        g = lambda r: mem.r16(0xDFF000 + r)
        con0, con1 = g(0x40), g(0x42)
        h = (size >> 6) & 0x3FF or 1024
        wd = size & 0x3F or 64
        use = ((con0 >> 11) & 1, (con0 >> 10) & 1, (con0 >> 9) & 1, (con0 >> 8) & 1)
        lf = con0 & 0xFF
        ash, bsh = con0 >> 12, con1 >> 12
        fwm, lwm = g(0x44), g(0x46)
        ptr = [(g(0x50) << 16) | g(0x52), (g(0x4C) << 16) | g(0x4E),
               (g(0x48) << 16) | g(0x4A), (g(0x54) << 16) | g(0x56)]
        mod = [self._s16(g(0x64)), self._s16(g(0x62)), self._s16(g(0x60)), self._s16(g(0x66))]
        dat = [g(0x74), g(0x72), g(0x70)]
        if con1 & 1:
            self._blit_line(con0, con1, size, ptr, mod, dat)
            self.intreq |= 0x40
            return
        desc = bool(con1 & 2)
        step = -2 if desc else 2
        fill = bool(con1 & 0x18)
        excl = bool(con1 & 0x10)
        aold = bold = 0
        r16, w16 = mem.r16, mem.w16
        for _ in range(h):
            carry = (con1 >> 2) & 1
            for x in range(wd):
                a = r16(ptr[0]) if use[0] else dat[0]
                if use[0]:
                    ptr[0] = (ptr[0] + step) & 0xFFFFFF
                b = r16(ptr[1]) if use[1] else dat[1]
                if use[1]:
                    ptr[1] = (ptr[1] + step) & 0xFFFFFF
                c = r16(ptr[2]) if use[2] else dat[2]
                if use[2]:
                    ptr[2] = (ptr[2] + step) & 0xFFFFFF
                first = (x == wd - 1) if desc else (x == 0)
                last = (x == 0) if desc else (x == wd - 1)
                if first:
                    a &= fwm
                if last:
                    a &= lwm
                if desc:
                    sa = (((a << 16) | aold) >> (16 - ash)) & 0xFFFF if ash else a
                    sb = (((b << 16) | bold) >> (16 - bsh)) & 0xFFFF if bsh else b
                else:
                    sa = (((aold << 16) | a) >> ash) & 0xFFFF
                    sb = (((bold << 16) | b) >> bsh) & 0xFFFF
                aold, bold = a, b
                d = 0
                for i in range(8):
                    if lf >> i & 1:
                        d |= ((sa if i & 4 else ~sa) & (sb if i & 2 else ~sb) & (c if i & 1 else ~c)) & 0xFFFF
                if fill:
                    # fill works on the destination word bit by bit (LSB first ascending, MSB first desc)
                    out = 0
                    cin = carry
                    order = range(16) if not desc else range(15, -1, -1)
                    for bit in order:
                        v = (d >> bit) & 1
                        o = v ^ cin
                        if excl:
                            pass
                        cin = cin ^ v
                        out |= (o if not excl else (cin ^ v) if False else o) << bit
                    d = out
                    carry = cin
                if use[3]:
                    w16(ptr[3], d)
                    ptr[3] = (ptr[3] + step) & 0xFFFFFF
            for i in range(4):
                if use[i]:
                    ptr[i] = (ptr[i] + (-mod[i] if desc else mod[i])) & 0xFFFFFF
        # leave the pointer registers where the hardware would
        for reg, v in ((0x50, ptr[0]), (0x4C, ptr[1]), (0x48, ptr[2]), (0x54, ptr[3])):
            mem.w16(0xDFF000 + reg, v >> 16)
            mem.w16(0xDFF000 + reg + 2, v & 0xFFFF)
        self.intreq |= 0x40
        self.blits = getattr(self, 'blits', 0) + 1

    @staticmethod
    def _s16(v):
        return v - 0x10000 if v & 0x8000 else v

    def _blit_line(self, con0, con1, size, ptr, mod, dat):
        """Line mode (single-pixel Bresenham), enough for common uses."""
        mem = self.mem
        self.lines = getattr(self, 'lines', 0) + 1
        h = (size >> 6) & 0x3FF
        octant = (con1 >> 2) & 7
        sing = bool(con1 & 2)
        x = (con0 >> 12) & 0xF
        err = self._s16(mem.r16(0xDFF052) if False else (ptr[0] & 0xFFFF))
        # A pointer low word holds the error accumulator, B data the pattern.
        bltamod, bltbmod = mod[0], mod[1]
        pat = dat[1]
        cptr = ptr[2]
        dptr = ptr[3]
        cmod = mod[2]
        # decode octant: (sul, sud, aul) per HRM table
        sud = bool(octant & 1)    # 'steep': major axis vertical
        sul = bool(octant & 2)
        aul = bool(octant & 4)
        lf = con0 & 0xFF
        bit = 15 - x
        use_c = bool(con0 & 0x200)
        for i in range(h):
            # plot
            c = mem.r16(cptr)
            b = 0xFFFF if (pat >> (15 - (i & 15))) & 1 else 0
            a = 0x8000 >> (15 - bit) if False else (1 << bit)
            d = 0
            for k in range(8):
                if lf >> k & 1:
                    d |= ((a if k & 4 else ~a) & (b if k & 2 else ~b) & (c if k & 1 else ~c)) & 0xFFFF
            mem.w16(dptr, d)
            # step
            if err < 0:
                err += bltbmod
                if sud:
                    cptr += cmod if not sul else -cmod
                else:
                    bit += -1 if aul else 1
            else:
                err += bltamod
                if sud:
                    bit += -1 if aul else 1
                    cptr += cmod if not sul else -cmod
                else:
                    cptr += cmod if not sul else -cmod
                    bit += -1 if aul else 1
            if bit < 0:
                bit = 15; cptr -= 2
            elif bit > 15:
                bit = 0; cptr += 2
            dptr = cptr

    def _track_no(self):
        side = 0 if (self.prb & 4) else 1           # /SIDE low = upper head
        return min(self.cyl, 79) * 2 + side

    def _disk_dma(self, length_words):
        t = self._track_no()
        raw = encode_track(self.adf[t * 5632:(t + 1) * 5632], t)
        words = []
        for l in raw:
            words.append(l >> 16)
            words.append(l & 0xFFFF)
        start = 0
        if self.adkcon & 0x0400:
            start = next((j for j, wd in enumerate(words) if wd == self.dsksync), 0)
        data = b''.join(struct.pack('>H', words[(start + j) % len(words)]) for j in range(length_words))
        self.wb(self.dskpt, data)
        self.disk_reads.append((t, self.dskpt, length_words))
        self.log('  [disk] DMA track %d (cyl %d) -> %06x, %d words' % (t, self.cyl, self.dskpt, length_words))

    # -- exec -------------------------------------------------------------------
    def _init_exec(self):
        self.wl(4, EXEC)
        self.tids = {}
        for k in range(1, 0x120):
            lvo = -6 * k
            tid = self.mach.traps.alloc(self._mk_trap(k, lvo))
            self.tids[k] = tid
            self.wb(EXEC + lvo, b'\x4e\xf9' + struct.pack('>I', STUB + 8 * k))
            self.wb(STUB + 8 * k, struct.pack('>H', 0xA000 | tid) + b'\x4e\x75')

    def _mk_trap(self, k, lvo):
        def f(opcode, pc):
            name = EXEC_LVO.get(lvo, 'LVO%d' % lvo)
            self.calls.append(name)
            fn = getattr(self, 'ex_' + name, None)
            if fn:
                fn()
            else:
                self.log('  [exec] unhandled', name, 'a0=%x a1=%x d0=%x d1=%x' % (
                    self.r('a0'), self.r('a1'), self.r('d0'), self.r('d1')))
                self.w('d0', 0)
        return f

    def ex_AllocAbs(self):
        self.log('  [exec] AllocAbs size=%x at=%x' % (self.r('d0'), self.r('a1')))
        self.w('d0', self.r('a1'))

    def ex_AllocMem(self):
        size = (self.r('d0') + 7) & ~7
        addr = self.heap
        self.heap += size
        self.w('d0', addr)

    def ex_FreeMem(self): pass
    def ex_Forbid(self): pass
    def ex_Permit(self): pass
    def ex_Disable(self): pass
    def ex_Enable(self): pass
    def ex_TypeOfMem(self): self.w('d0', 3 if self.r('a1') < CHIP else 0)
    def ex_AvailMem(self): self.w('d0', CHIP - 0x10000)
    def ex_OpenDevice(self): self.w('d0', 0)
    def ex_CloseDevice(self): pass
    def ex_WaitIO(self): self.w('d0', 0)
    def ex_CheckIO(self): self.w('d0', 1)

    def ex_DoIO(self):
        io = self.r('a1')
        cmd = self.rw(io + 0x1C)
        length, data, off = self.rl(io + 0x24), self.rl(io + 0x28), self.rl(io + 0x2C)
        if cmd == 2:
            self.wb(data, self.adf[off:off + length].ljust(length, b'\0'))
            self.wl(io + 0x20, length)
            self.reads.append((off, length, data))
            self.log('  [disk] READ off=%06x len=%06x -> %06x' % (off, length, data))
        self.mem.w8(io + 0x1F, 0)
        self.w('d0', 0)

    ex_SendIO = ex_DoIO

    # -- running -------------------------------------------------------------------
    def _irq_pending(self):
        act = self.intreq & self.intena & 0x3FFF
        if not act or not (self.intena & 0x4000):
            return None
        mask = (self.cpu.r_sr() >> 8) & 7
        for lvl, bits in ((6, 0x2000), (5, 0x1800), (4, 0x0780), (3, 0x0070), (2, 0x0008), (1, 0x0007)):
            if act & bits and lvl > mask:
                return lvl
        return None

    def _enter_irq(self, lvl):
        sr = self.cpu.r_sr()
        ret = self.cpu.r_pc()
        self.cpu.w_sr(((sr | 0x2000) & ~0x8000 & ~0x0700) | (lvl << 8))
        sp = (self.cpu.r_sp() - 6) & 0xFFFFFFFF
        self.ww(sp, sr)
        self.wl(sp + 2, ret)
        self.cpu.w_sp(sp)
        self.cpu.w_pc(self.rl((24 + lvl) * 4))

    def run(self, max_cycles=10**12, stop=None, slice_cycles=2000):
        """Run until max_cycles have elapsed, or the pc reaches an address in
        `stop`.  Returns the stop address or None."""
        stop = set(stop or ())
        self.stopped = None
        watch = self.watch
        if stop or watch:
            def hook(pc):
                f = watch.get(pc)
                if f:
                    f(pc)
                if pc in stop and self.stopped is None:
                    self.stopped = pc
                    self.mach.abort_execute()
            self.cpu.set_instr_hook_callback(hook)
        try:
            end = self.cycles + max_cycles
            while self.cycles < end and self.stopped is None:
                n = min(slice_cycles, CYCLES_PER_FRAME - self.frame_cycles, end - self.cycles)
                res = self.mach.execute(n)
                used = max(res.cycles, 1)
                self.cycles += used
                self.frame_cycles += used
                if self.frame_cycles >= CYCLES_PER_FRAME:
                    self.frame_cycles -= CYCLES_PER_FRAME
                    self.intreq |= 0x20 | 0x10      # VERTB + a copper interrupt
                    self.vbl_count += 1
                lvl = self._irq_pending()
                if lvl:
                    self._enter_irq(lvl)
        finally:
            if stop or watch:
                self.cpu.set_instr_hook_callback(None)
        return self.stopped

    @staticmethod
    def joy_word(dx, dy):
        """JOYxDAT value for a joystick direction (dx, dy in -1/0/1)."""
        b9 = 1 if dx < 0 else 0
        b1 = 1 if dx > 0 else 0
        up, down = (1 if dy < 0 else 0), (1 if dy > 0 else 0)
        b8 = b9 ^ up
        b0 = b1 ^ down
        return (b9 << 9) | (b8 << 8) | (b1 << 1) | b0

    # -- convenience ------------------------------------------------------------
    def cstr(self, a, n=64):
        b = self.rb(a, n)
        return b.split(b'\0')[0].decode('latin-1')

    def boot_kernel(self, block_off=0xD9400):
        """Start the original Sales Curve kernel the way the boot block does."""
        B = 0x70000
        self.wb(B, self.adf[block_off:block_off + 0x2C00])
        self.wb(0x90, self.adf[0x42:0xB2])        # trackloader stub the boot block copies
        self.w('a0', B); self.w('a3', B)
        self.w('a1', self.ioreq); self.w('a4', self.ioreq); self.w('a6', 0x100)
        self.cpu.w_sr(0x2000); self.cpu.w_isp(0x7000)
        self.cpu.w_sr(0x0000); self.cpu.w_usp(B + 0x70)
        self.cpu.w_pc(B + 0x70)
