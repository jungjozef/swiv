"""Recursive-descent 68000 disassembler for SWIV's AMPROG.OBJ.

Produces an annotated listing: labels for branch/call/data targets, strings,
kernel service names (jsr d16(a6)) and custom-chip register names.

    python tools/disasm.py assets/AMPROG.OBJ 0xefc0 reverse/amprog.s [entry ...]
"""
import re
import struct
import sys
from capstone import Cs, CS_ARCH_M68K, CS_MODE_M68K_000

# Kernel service vectors: displacement from a6 -> name.  Names are inferred
# from the kernel routines they branch to (see tools/README.md).
KERNEL = {
    -0x5fa: 'k_FindFile',      # -> $796
    -0x5f6: 'k_CloseFile',     # -> $7b6
    -0x5f2: 'k_Read',          # -> $b14  a0=dest d0=count (LZ unpack)
    -0x5e6: 'k_OpenFile',      # -> $6e4
    -0x5e2: 'k_Seek',          # -> $75a
    -0x5de: 'k_AllocAny',      # -> $dfe  d0=size -> a0 (fast list, then chip)
    -0x5da: 'k_AllocFast',     # -> $de0
    -0x5d6: 'k_AllocChip',     # -> $e0e  d0=size -> a0
    -0x5d2: 'k_Free',          # -> $e48  a0=ptr d0=size
    -0x5ce: 'k_AvailMem',      # -> $ea2
    -0x5ca: 'k_FreeRange',     # -> $f02
    -0x5c6: 'k_AddTaskA',      # -> $100e
    -0x5c2: 'k_AddTaskB',      # -> $1030
    -0x5be: 'k_AddTaskC',      # -> $1052
    -0x5ae: 'k_TaskX1',        # -> $101a
    -0x5aa: 'k_TaskX2',        # -> $1032
    -0x59e: 'k_Yield',         # -> $f76  switch to next task
    -0x592: 'k_Wait92',        # -> $fb2
    -0x58e: 'k_Sig8e',         # -> $f54
    -0x58a: 'k_Wait8a',        # -> $f2e
    -0x586: 'k_Sig86',         # -> $f3a
    -0x582: 'k_Sig82',         # -> $f4a
}

CUSTOM = {
    0x002: 'DMACONR', 0x004: 'VPOSR', 0x006: 'VHPOSR', 0x00a: 'JOY0DAT',
    0x00c: 'JOY1DAT', 0x00e: 'CLXDAT', 0x010: 'ADKCONR', 0x016: 'POTGOR',
    0x018: 'SERDATR', 0x01a: 'DSKBYTR', 0x01c: 'INTENAR', 0x01e: 'INTREQR',
    0x020: 'DSKPTH', 0x022: 'DSKPTL', 0x024: 'DSKLEN', 0x02a: 'VPOSW',
    0x02c: 'VHPOSW', 0x030: 'SERDAT', 0x032: 'SERPER', 0x034: 'POTGO',
    0x040: 'BLTCON0', 0x042: 'BLTCON1', 0x044: 'BLTAFWM', 0x046: 'BLTALWM',
    0x048: 'BLTCPTH', 0x04a: 'BLTCPTL', 0x04c: 'BLTBPTH', 0x04e: 'BLTBPTL',
    0x050: 'BLTAPTH', 0x052: 'BLTAPTL', 0x054: 'BLTDPTH', 0x056: 'BLTDPTL',
    0x058: 'BLTSIZE', 0x060: 'BLTCMOD', 0x062: 'BLTBMOD', 0x064: 'BLTAMOD',
    0x066: 'BLTDMOD', 0x070: 'BLTCDAT', 0x072: 'BLTBDAT', 0x074: 'BLTADAT',
    0x07e: 'DSKSYNC', 0x080: 'COP1LCH', 0x082: 'COP1LCL', 0x084: 'COP2LCH',
    0x086: 'COP2LCL', 0x088: 'COPJMP1', 0x08a: 'COPJMP2', 0x08e: 'DIWSTRT',
    0x090: 'DIWSTOP', 0x092: 'DDFSTRT', 0x094: 'DDFSTOP', 0x096: 'DMACON',
    0x098: 'CLXCON', 0x09a: 'INTENA', 0x09c: 'INTREQ', 0x09e: 'ADKCON',
    0x0a0: 'AUD0LCH', 0x0a2: 'AUD0LCL', 0x0a4: 'AUD0LEN', 0x0a6: 'AUD0PER',
    0x0a8: 'AUD0VOL', 0x0aa: 'AUD0DAT', 0x0b0: 'AUD1LCH', 0x0b2: 'AUD1LCL',
    0x0b4: 'AUD1LEN', 0x0b6: 'AUD1PER', 0x0b8: 'AUD1VOL', 0x0c0: 'AUD2LCH',
    0x0c2: 'AUD2LCL', 0x0c4: 'AUD2LEN', 0x0c6: 'AUD2PER', 0x0c8: 'AUD2VOL',
    0x0d0: 'AUD3LCH', 0x0d2: 'AUD3LCL', 0x0d4: 'AUD3LEN', 0x0d6: 'AUD3PER',
    0x0d8: 'AUD3VOL', 0x0e0: 'BPL1PTH', 0x0e2: 'BPL1PTL', 0x0e4: 'BPL2PTH',
    0x0e6: 'BPL2PTL', 0x0e8: 'BPL3PTH', 0x0ea: 'BPL3PTL', 0x0ec: 'BPL4PTH',
    0x0ee: 'BPL4PTL', 0x0f0: 'BPL5PTH', 0x0f2: 'BPL5PTL', 0x100: 'BPLCON0',
    0x102: 'BPLCON1', 0x104: 'BPLCON2', 0x108: 'BPL1MOD', 0x10a: 'BPL2MOD',
    0x110: 'BPL1DAT', 0x120: 'SPR0PTH', 0x122: 'SPR0PTL', 0x124: 'SPR1PTH',
    0x126: 'SPR1PTL', 0x128: 'SPR2PTH', 0x12a: 'SPR2PTL', 0x12c: 'SPR3PTH',
    0x12e: 'SPR3PTL', 0x130: 'SPR4PTH', 0x132: 'SPR4PTL', 0x134: 'SPR5PTH',
    0x136: 'SPR5PTL', 0x138: 'SPR6PTH', 0x13a: 'SPR6PTL', 0x13c: 'SPR7PTH',
    0x13e: 'SPR7PTL',
}
for i in range(32):
    CUSTOM[0x180 + 2 * i] = 'COLOR%02d' % i
for i in range(8):
    CUSTOM[0x140 + 8 * i] = 'SPR%dPOS' % i
    CUSTOM[0x142 + 8 * i] = 'SPR%dCTL' % i
    CUSTOM[0x144 + 8 * i] = 'SPR%dDATA' % i
    CUSTOM[0x146 + 8 * i] = 'SPR%dDATB' % i

md = Cs(CS_ARCH_M68K, CS_MODE_M68K_000)
md.detail = False
HEX = re.compile(r'\$([0-9a-f]+)')


class Prog:
    def __init__(self, data, base):
        self.d = data
        self.base = base
        self.end = base + len(data)
        self.insn = {}      # addr -> (size, mnem, ops)
        self.labels = {}    # addr -> kind
        self.refs = {}      # addr -> set(from addr)

    def inside(self, a):
        return self.base <= a < self.end

    def decode(self, a):
        o = a - self.base
        if o < 0 or o + 2 > len(self.d) or (a & 1):
            return None
        try:
            i = next(md.disasm(self.d[o:o + 12], a))
        except StopIteration:
            return None
        if i.mnemonic in ('dc.w', 'illegal') or i.size == 0:
            return None
        return i

    def ref(self, tgt, frm, kind):
        if not self.inside(tgt):
            return
        self.labels.setdefault(tgt, kind)
        if kind == 'sub' or (kind == 'code' and self.labels[tgt] != 'sub'):
            self.labels[tgt] = kind if self.labels[tgt] != 'sub' else 'sub'
        self.refs.setdefault(tgt, set()).add(frm)

    def trace(self, entries):
        work = list(entries)
        while work:
            a = work.pop()
            while self.inside(a) and a not in self.insn:
                i = self.decode(a)
                if i is None:
                    break
                m, ops = i.mnemonic, i.op_str
                self.insn[a] = (i.size, m, ops)
                nxt = a + i.size
                tg = None
                if m.startswith(('bra', 'bsr', 'bcc', 'bcs', 'beq', 'bne', 'bmi', 'bpl',
                                 'bvc', 'bvs', 'bge', 'bgt', 'ble', 'blt', 'bhi', 'bls',
                                 'db')) and not m.startswith(('bchg', 'bclr', 'bset', 'btst', 'bfc', 'bfe', 'bff', 'bfi', 'bfs', 'bkpt')):
                    mm = HEX.findall(ops)
                    if mm:
                        tg = int(mm[-1], 16)
                elif m in ('jsr', 'jmp') or m.startswith(('jsr', 'jmp')):
                    if '(pc,' not in ops and '(a' not in ops:
                        mm = HEX.findall(ops)
                        if mm:
                            tg = int(mm[0], 16)
                    elif '(pc)' in ops:
                        mm = HEX.findall(ops)
                        if mm:
                            tg = int(mm[0], 16)
                kind_sub = m.startswith(('bsr', 'jsr'))
                if tg is not None and self.inside(tg):
                    self.ref(tg, a, 'sub' if kind_sub else 'code')
                    if tg not in self.insn:
                        work.append(tg)
                # data references via pc-relative / absolute operands
                for mm in re.finditer(r'\$([0-9a-f]+)\(pc\)', ops):
                    t = int(mm.group(1), 16)
                    if self.inside(t) and t != tg:
                        self.ref(t, a, 'data')
                for mm in re.finditer(r'\$([0-9a-f]+)\.l', ops):
                    t = int(mm.group(1), 16)
                    if self.inside(t) and t != tg:
                        self.ref(t, a, 'data')
                # pc-indexed jump: following words are a table of bra
                if m == 'jmp' and '(pc,' in ops:
                    t = nxt
                    while True:
                        j = self.decode(t)
                        if j is None or not j.mnemonic.startswith('bra'):
                            break
                        self.insn[t] = (j.size, j.mnemonic, j.op_str)
                        mm = HEX.findall(j.op_str)
                        if mm:
                            tt = int(mm[-1], 16)
                            self.ref(tt, t, 'code')
                            if tt not in self.insn:
                                work.append(tt)
                        t += j.size
                if m.startswith(('bra', 'jmp', 'rts', 'rte', 'rtr', 'stop')):
                    break
                a = nxt

    # ---- speculative discovery ------------------------------------------
    TERM = ('bra', 'jmp', 'rts', 'rte', 'rtr')

    def try_flow(self, entry):
        """Follow control flow from entry without committing.  Returns a dict
        of new instructions, or None if anything looks wrong."""
        new = {}
        work = [entry]
        covered = set()
        while work:
            a = work.pop()
            while True:
                if a in self.insn or a in new:
                    break
                if not self.inside(a) or (a & 1):
                    return None
                i = self.decode(a)
                if i is None:
                    return None
                # must not start inside an already-decoded instruction
                for k in range(1, i.size):
                    if (a + k) in self.insn or (a + k) in new:
                        return None
                if a in covered:
                    return None
                covered.update(range(a, a + i.size))
                if len(new) > 4000:
                    return None
                m, ops = i.mnemonic, i.op_str
                new[a] = (i.size, m, ops)
                tg = None
                if m.startswith(('bra', 'bsr', 'bcc', 'bcs', 'beq', 'bne', 'bmi', 'bpl',
                                 'bvc', 'bvs', 'bge', 'bgt', 'ble', 'blt', 'bhi', 'bls', 'db')) \
                        and not m.startswith(('bchg', 'bclr', 'bset', 'btst')):
                    mm = HEX.findall(ops)
                    if mm:
                        tg = int(mm[-1], 16)
                elif m in ('jsr', 'jmp') and '(a' not in ops and '(pc,' not in ops:
                    mm = HEX.findall(ops)
                    if mm:
                        tg = int(mm[0], 16)
                if tg is not None:
                    if not self.inside(tg):
                        # calls/branches outside the program are fine only for hardware-ish
                        # absolute jsr (kernel); reject wild branches
                        if not m.startswith(('jsr', 'jmp')):
                            return None
                    elif (tg & 1):
                        return None
                    else:
                        work.append(tg)
                if m.startswith(self.TERM):
                    break
                a += i.size
        return new

    def speculate(self, max_rounds=8):
        added_total = 0
        for rnd in range(max_rounds):
            cands = set()
            d, base = self.d, self.base
            n = len(d)
            for o in range(0, n - 6, 2):
                a = base + o
                w = (d[o] << 8) | d[o + 1]
                if w == 0x6100:       # bsr.w
                    disp = struct.unpack('>h', d[o + 2:o + 4])[0]
                    cands.add(a + 2 + disp)
                elif w == 0x4eb9:     # jsr abs.l
                    cands.add(struct.unpack('>I', d[o + 2:o + 6])[0])
                elif w == 0x4eba:     # jsr d16(pc)
                    disp = struct.unpack('>h', d[o + 2:o + 4])[0]
                    cands.add(a + 2 + disp)
                elif w == 0x6000:     # bra.w
                    disp = struct.unpack('>h', d[o + 2:o + 4])[0]
                    cands.add(a + 2 + disp)
                elif w == 0x41fa or w == 0x43fa or w == 0x45fa or w == 0x47fa or w == 0x49fa:
                    disp = struct.unpack('>h', d[o + 2:o + 4])[0]
                    t = a + 2 + disp
                    # lea label(pc),An followed (soon) by a task-create call
                    tail = d[o + 4:o + 40]
                    if re.search(rb'\x4e\xae\xfa[\xc6\xc2\xbe\xae\xaa]', tail):
                        cands.add(t)
                if w == 0x4e75 or w == 0x4e73:       # code right after rts / rte
                    cands.add(a + 2)
            added = 0
            for c in sorted(cands):
                if not self.inside(c) or (c & 1) or c in self.insn:
                    continue
                if c < 0xfa00 and self.base == 0xefc0:   # filename table region
                    continue
                new = self.try_flow(c)
                if new and len(new) >= 3:
                    self.insn.update(new)
                    for a2, (sz, m, ops) in new.items():
                        pass
                    added += len(new)
            added_total += added
            if not added:
                break
        # rebuild labels/refs over the full instruction set
        self.rebuild_refs()
        return added_total

    def rebuild_refs(self):
        self.labels, self.refs = {}, {}
        for a, (size, m, ops) in sorted(self.insn.items()):
            tg = None
            if m.startswith(('bra', 'bsr', 'bcc', 'bcs', 'beq', 'bne', 'bmi', 'bpl',
                             'bvc', 'bvs', 'bge', 'bgt', 'ble', 'blt', 'bhi', 'bls', 'db')) \
                    and not m.startswith(('bchg', 'bclr', 'bset', 'btst')):
                mm = HEX.findall(ops)
                if mm:
                    tg = int(mm[-1], 16)
            elif m in ('jsr', 'jmp') and '(a' not in ops and '(pc,' not in ops:
                mm = HEX.findall(ops)
                if mm:
                    tg = int(mm[0], 16)
            if tg is not None and self.inside(tg):
                self.ref(tg, a, 'sub' if m.startswith(('bsr', 'jsr')) else 'code')
            for mm in re.finditer(r'\$([0-9a-f]+)\(pc\)', ops):
                t = int(mm.group(1), 16)
                if self.inside(t) and t != tg:
                    self.ref(t, a, 'data')
            for mm in re.finditer(r'\$([0-9a-f]+)\.l', ops):
                t = int(mm.group(1), 16)
                if self.inside(t) and t != tg:
                    self.ref(t, a, 'data')

    # ---- output ---------------------------------------------------------
    def lab(self, a):
        k = self.labels.get(a)
        if k == 'sub':
            return 'sub_%x' % a
        if k == 'data':
            return 'd_%x' % a
        return 'l_%x' % a

    def fmt_ops(self, ops, comment):
        def rep(mm):
            t = int(mm.group(1), 16)
            if t in self.labels:
                return self.lab(t)
            if 0xdff000 <= t < 0xdff200:
                n = CUSTOM.get(t - 0xdff000)
                if n:
                    return n
            return mm.group(0)
        out = HEX.sub(rep, ops)
        mm = re.search(r'(-?)\$([0-9a-f]+)\(a6\)', ops)
        if mm:
            disp = -int(mm.group(2), 16) if mm.group(1) else int(mm.group(2), 16)
            if disp in KERNEL:
                comment.append(KERNEL[disp])
        for mm in re.finditer(r'\$([0-9a-f]+)\(a4\)', ops):
            n = CUSTOM.get(int(mm.group(1), 16))
            if n:
                comment.append('a4:' + n)
        return out

    def listing(self):
        out = []
        a = self.base
        end = self.end
        while a < end:
            if a in self.labels:
                out.append('')
                xr = sorted(self.refs.get(a, ()))
                note = ''
                if xr:
                    note = '   ; xref: ' + ' '.join('%x' % x for x in xr[:6]) + (' ...' if len(xr) > 6 else '')
                out.append('%s:%s' % (self.lab(a), note))
            if a in self.insn:
                size, m, ops = self.insn[a]
                cm = []
                o = self.fmt_ops(ops, cm)
                raw = self.d[a - self.base:a - self.base + size].hex()
                out.append('  %06x  %-16s %-8s %-30s%s' % (a, raw, m, o, ('; ' + ', '.join(cm)) if cm else ''))
                a += size
                continue
            # data run until next label / instruction
            b = a
            while b < end and b not in self.insn and (b == a or b not in self.labels):
                b += 1
            chunk = self.d[a - self.base:b - self.base]
            out.extend(self.fmt_data(a, chunk))
            a = b
        return '\n'.join(out) + '\n'

    def fmt_data(self, a, chunk):
        res = []
        i = 0
        n = len(chunk)
        while i < n:
            m = re.match(rb'[ -~]{4,}', chunk[i:])
            if m:
                s = m.group()
                end = i + len(s)
                term = ''
                if end < n and chunk[end] == 0:
                    term = ', 0'
                    end += 1
                res.append('  %06x  dc.b    "%s"%s' % (a + i, s.decode('ascii').replace('"', '""'), term))
                i = end
                continue
            run = chunk[i:i + 16]
            # stop the run before the next printable string
            m2 = re.search(rb'[ -~]{4,}', run)
            if m2 and m2.start() > 0:
                run = run[:m2.start()]
            elif m2:
                run = run[:1]
            res.append('  %06x  dc.b    %s' % (a + i, ','.join('$%02x' % x for x in run)))
            i += len(run)
        return res


def main():
    path, base = sys.argv[1], int(sys.argv[2], 0)
    out = sys.argv[3]
    entries = [int(x, 0) for x in sys.argv[4:]] or [base]
    p = Prog(open(path, 'rb').read(), base)
    p.trace(entries)
    n0 = len(p.insn)
    p.speculate()
    print('speculative: +%d instructions' % (len(p.insn) - n0))
    open(out, 'w').write(p.listing())
    code = sum(s for s, _, _ in p.insn.values())
    print('instructions: %d  code bytes: %d of %d (%.0f%%)  labels: %d' % (
        len(p.insn), code, len(p.d), 100 * code / len(p.d), len(p.labels)))


if __name__ == '__main__':
    main()
