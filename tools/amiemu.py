"""Minimal Amiga-ish 68000 harness on top of Unicorn.

Provides: RAM + custom/CIA register pages, a fake ExecBase whose library
vectors trap into Python, and a trackdisk-style DoIO backed by an ADF image.
Enough to run trackloader kernels under emulation and dump what they load.
"""
import struct
from unicorn import *
from unicorn.m68k_const import *

R = {n: getattr(__import__('unicorn.m68k_const', fromlist=['x']), 'UC_M68K_REG_' + n.upper())
     for n in ['d0', 'd1', 'd2', 'd3', 'd4', 'd5', 'd6', 'd7',
               'a0', 'a1', 'a2', 'a3', 'a4', 'a5', 'a6', 'a7', 'pc', 'sr']}

def _mfm_longs(vals, prev):
    """MFM-encode 0x55555555-masked data longs; returns (longs, last_data_bit)."""
    out = []
    for d in vals:
        clk = ~(((d >> 1) | (prev << 31)) | (d << 1)) & 0xAAAAAAAA
        out.append((d | clk) & 0xFFFFFFFF)
        prev = d & 1
    return out, prev


def encode_track(trk_data, track_no):
    """Standard AmigaDOS MFM track (11 sectors) as a list of 32-bit longs."""
    out = []
    prev = 0

    def odd_even(longs):
        return [(x >> 1) & 0x55555555 for x in longs] + [x & 0x55555555 for x in longs]

    def emit(vals):
        nonlocal prev
        enc, prev = _mfm_longs(vals, prev)
        out.extend(enc)

    for sec in range(11):
        out.extend([0xAAAAAAAA])                 # gap
        out.append(0x44894489)                   # sync
        prev = 1
        info = (0xFF << 24) | (track_no << 16) | (sec << 8) | (11 - sec)
        hdr = odd_even([info])
        label = odd_even([0, 0, 0, 0])
        emit(hdr)
        emit(label)
        hsum = 0
        for x in hdr + label:
            hsum ^= x
        hsum &= 0x55555555
        emit([0, hsum & 0x55555555])             # header checksum (odd=0 even=x) approx
        data = trk_data[sec * 512:(sec + 1) * 512]
        words = [int.from_bytes(data[i:i + 4], 'big') for i in range(0, 512, 4)]
        dd = odd_even(words)
        dsum = 0
        for x in dd:
            dsum ^= x
        dsum &= 0x55555555
        emit([0, dsum])                           # data checksum
        emit(dd)
    out.extend([0xAAAAAAAA] * 40)
    return out


EXEC = 0xF00800         # fake ExecBase, outside RAM so the kernel can't clobber it
STUB = 0xF80000         # library vectors jump here: STUB + 8*k  (k = LVO/6)
RAM_SIZE = 0x100000     # 1MB chip

# exec.library vector names we know about (LVO -> name)
EXEC_LVO = {
    -0x84: 'Forbid', -0x8a: 'Permit', -0xc6: 'AllocMem', -0xcc: 'AllocAbs',
    -0xd2: 'FreeMem', -0xd8: 'AvailMem', -0x1bc: 'OpenDevice',
    -0x1c2: 'CloseDevice', -0x1c8: 'DoIO', -0x1ce: 'SendIO', -0x1d4: 'CheckIO',
    -0x1da: 'WaitIO', -0x216: 'TypeOfMem', -0x30: 'Supervisor',
    -0x24: 'Disable?', -0x78: 'Disable', -0x7e: 'Enable',
}


class Amiga:
    def __init__(self, adf_path, verbose=True):
        self.adf = open(adf_path, 'rb').read()
        self.verbose = verbose
        self.reads = []         # (offset, length, dest) of every disk read
        self.calls = []
        uc = self.uc = Uc(UC_ARCH_M68K, UC_MODE_BIG_ENDIAN)
        uc.ctl_set_cpu_model(UC_CPU_M68K_M68000)
        uc.mem_map(0, RAM_SIZE)
        uc.mem_map(0xDFF000, 0x1000)     # custom chips
        uc.mem_map(0xBFD000, 0x2000)     # CIAs
        uc.mem_map(STUB, 0x1000)
        uc.mem_map(0xF00000, 0x1000)      # fake ExecBase + vector table
        self.hooks = {}
        self._init_exec()
        uc.hook_add(UC_HOOK_CODE, self._code_hook, begin=STUB, end=STUB + 0xFFF)
        uc.hook_add(UC_HOOK_INTR, self._intr)
        self.pending = None
        self.ring = []
        self.next_irq_check = 0
        self.irq_poll = None
        self.req = None
        self._bypass = False
        self.intena = 0
        self.intreq = 0
        self.dmacon = 0
        self.vbl_period = 20000      # instructions per 50Hz frame (rough)
        self.vbl_wait = False
        self.vpos = 0
        uc.hook_add(UC_HOOK_MEM_WRITE, self._cust_write, begin=0xDFF000, end=0xDFFFFF)
        uc.hook_add(UC_HOOK_MEM_READ, self._cust_read, begin=0xDFF000, end=0xDFFFFF)
        self.adkcon = 0
        self.dsksync = 0x4489
        self.dskpt = 0
        self.dsklen_prev = 0
        self.cyl = 10
        self.prb = 0xFF
        self.disk_reads = []
        self.irq_wait = 0            # bits of INTREQ raised by us, delivered by hook
        uc.hook_add(UC_HOOK_MEM_WRITE, self._cia_write, begin=0xBFD000, end=0xBFEFFF)
        uc.hook_add(UC_HOOK_MEM_READ, self._cia_read, begin=0xBFD000, end=0xBFEFFF)
        self.trace_on = True
        self.total = 0
        self.budget = 10**12
        self.budget_hit = False

    # -- helpers ------------------------------------------------------------
    def r(self, name): return self.uc.reg_read(R[name])
    def w(self, name, v): self.uc.reg_write(R[name], v & 0xFFFFFFFF)
    def rb(self, a, n): return bytes(self.uc.mem_read(a, n))
    def wb(self, a, b): self.uc.mem_write(a, bytes(b))
    def rl(self, a): return struct.unpack('>I', self.rb(a, 4))[0]
    def wl(self, a, v): self.wb(a, struct.pack('>I', v & 0xFFFFFFFF))
    def rw(self, a): return struct.unpack('>H', self.rb(a, 2))[0]
    def ww(self, a, v): self.wb(a, struct.pack('>H', v & 0xFFFF))
    def log(self, *a):
        if self.verbose: print(*a)

    # -- exec ---------------------------------------------------------------
    def _init_exec(self):
        self.wl(4, EXEC)
        # fill library vectors: jmp STUB+8*k ; stub itself = rts
        for k in range(1, 0x120):
            lvo = -6 * k
            self.wb(EXEC + lvo, b'\x4e\xf9' + struct.pack('>I', STUB + 8 * k))
            self.wb(STUB + 8 * k, b'\x4e\x75')
        self.wl(EXEC + 0x26, 0)  # AttnFlags etc left zero
        self.next_io = 0x2100
        # a fake IORequest/device for boot code that passes a1
        self.ioreq = 0x2000

    def _code_hook(self, uc, addr, size, ud):
        k = (addr - STUB) // 8
        lvo = -6 * k
        name = EXEC_LVO.get(lvo, 'LVO%d' % lvo)
        fn = getattr(self, 'ex_' + name.rstrip('?'), None)
        self.calls.append(name)
        if fn:
            fn()
        else:
            self.log('  [exec] unhandled', name, 'a0=%x a1=%x d0=%x d1=%x' % (self.r('a0'), self.r('a1'), self.r('d0'), self.r('d1')))
            self.w('d0', 0)

    def ex_AllocAbs(self):
        # d0 = size, a1 = location -> d0 = location
        self.log('  [exec] AllocAbs size=%x at=%x' % (self.r('d0'), self.r('a1')))
        self.w('d0', self.r('a1'))

    def ex_AllocMem(self):
        size = (self.r('d0') + 7) & ~7
        self.next_io = (self.next_io + 7) & ~7
        addr = getattr(self, 'heap', 0xA0000)
        self.heap = addr + size
        self.log('  [exec] AllocMem size=%x -> %x' % (self.r('d0'), addr))
        self.w('d0', addr)

    def ex_FreeMem(self): pass
    def ex_Forbid(self): pass
    def ex_Permit(self): pass
    def ex_Disable(self): pass
    def ex_Enable(self): pass

    def ex_TypeOfMem(self):
        a = self.r('a1')
        self.w('d0', 3 if a < RAM_SIZE else 0)

    def ex_AvailMem(self): self.w('d0', RAM_SIZE - 0x10000)
    def ex_OpenDevice(self): self.w('d0', 0)
    def ex_CloseDevice(self): pass

    def ex_DoIO(self):
        io = self.r('a1')
        cmd = self.rw(io + 0x1C)
        length = self.rl(io + 0x24)
        data = self.rl(io + 0x28)
        off = self.rl(io + 0x2C)
        if cmd == 2:  # CMD_READ
            blob = self.adf[off:off + length]
            self.wb(data, blob.ljust(length, b'\0'))
            self.wl(io + 0x20, length)
            self.reads.append((off, length, data))
            self.log('  [disk] READ off=%06x len=%06x -> %06x (pc-ret=%x)' % (off, length, data, self.rl(self.r('a7'))))
        else:
            self.log('  [disk] cmd=%d' % cmd)
        self.wb(io + 0x1F, b'\0')
        self.w('d0', 0)

    ex_SendIO = ex_DoIO
    def ex_WaitIO(self): self.w('d0', 0)
    def ex_CheckIO(self): self.w('d0', 1)

    def _intr(self, uc, intno, ud):
        # Unicorn doesn't vector 68k exceptions itself: record the request and
        # let run() perform it once emulation has stopped (SR must be read
        # exactly, see exact_sr).
        ret = (self.ring[-1] + 2) if self.ring else self.r('pc')   # trap #n is 2 bytes
        self.nexc = getattr(self, 'nexc', 0) + 1
        self.req = (None, intno, ret)
        uc.emu_stop()

    def exact_sr(self):
        """Unicorn's SR read returns stale condition codes after arithmetic.
        Execute a real `move.w sr,<abs>` to get the exact value (only legal
        while emulation is stopped)."""
        t = 0xF00F00
        self.wb(t, b'\x40\xf9' + struct.pack('>I', t + 0x20))
        pc = self.r('pc')
        self._bypass = True
        try:
            self.uc.emu_start(t, t + 6, count=1)
        finally:
            self._bypass = False
        v = self.rw(t + 0x20)
        self.w('pc', pc)
        return v

    def _enter_exception(self, level, vecno, ret):
        sr = self.exact_sr()
        new = (sr | 0x2000) & ~0x8000
        if level is not None:
            new = (new & ~0x0700) | (level << 8)
        self.w('sr', new)                 # supervisor: swaps to the SSP bank
        sp = (self.r('a7') - 6) & 0xFFFFFFFF
        self.ww(sp, sr)
        self.wl(sp + 2, ret)
        self.w('a7', sp)
        return self.rl(vecno * 4)

    # -- custom chips -------------------------------------------------------
    @staticmethod
    def _setclr(cur, val):
        return (cur | (val & 0x7FFF)) if val & 0x8000 else (cur & ~(val & 0x7FFF))

    # floppy drive 0 via CIA ports
    def _cia_write(self, uc, access, addr, size, value, ud):
        if addr == 0xBFD100:
            old, new = self.prb, value & 0xFF
            self.prb = new
            if (new & 0x08) == 0 and (old & 1) == 0 and (new & 1) == 1:   # STEP rising edge
                if new & 2:
                    self.cyl = max(0, self.cyl - 1)
                else:
                    self.cyl = min(81, self.cyl + 1)

    def _cia_read(self, uc, access, addr, size, value, ud):
        if addr == 0xBFE001:
            v = 0xFF & ~0x20                       # /RDY asserted
            if self.cyl == 0:
                v &= ~0x10                         # /TK0
            self.wb(addr, bytes([v]))
        elif addr in (0xBFDD00, 0xBFED01, 0xBFEE01, 0xBFDE00):
            pass

    def _track_no(self):
        side = 0 if (self.prb & 4) else 1           # /SIDE low = upper head
        return min(self.cyl, 79) * 2 + side

    def _disk_dma(self, length_words):
        t = self._track_no()
        raw = encode_track(self.adf[t * 5632:(t + 1) * 5632], t)
        # whole track repeated so any read length is satisfied; start at the sync
        buf = bytearray()
        i = 0
        words = []
        for l in raw:
            words.append(l >> 16); words.append(l & 0xFFFF)
        # start at first sync word if WORDSYNC is on
        start = 0
        if self.adkcon & 0x0400:
            start = next((j for j, w in enumerate(words) if w == self.dsksync), 0)
        data = b''.join(struct.pack('>H', words[(start + j) % len(words)]) for j in range(length_words))
        self.wb(self.dskpt, data)
        self.disk_reads.append((t, self.dskpt, length_words))
        self.log('  [disk] DMA track %d (cyl %d) -> %06x, %d words, wordsync=%s' % (t, self.cyl, self.dskpt, length_words, bool(self.adkcon & 0x400)))

    def _cust_write(self, uc, access, addr, size, value, ud):
        reg = addr & 0x1FE
        if reg == 0x20 and size == 4:
            self.dskpt = value & 0xFFFFFFFF
            return
        if size == 4:
            return
        value16 = value & 0xFFFF
        if reg == 0x9A: self.intena = self._setclr(self.intena, value16)
        elif reg == 0x9C: self.intreq = self._setclr(self.intreq, value16)
        elif reg == 0x96: self.dmacon = self._setclr(self.dmacon, value16)
        elif reg == 0x9E: self.adkcon = self._setclr(self.adkcon, value16)
        elif reg == 0x7E: self.dsksync = value16
        elif reg == 0x20: self.dskpt = (self.dskpt & 0xFFFF) | (value16 << 16)
        elif reg == 0x22: self.dskpt = (self.dskpt & 0xFFFF0000) | value16
        elif reg == 0x24:
            if value16 & 0x8000 and self.dsklen_prev & 0x8000 and not (value16 & 0x4000):
                self._disk_dma(value16 & 0x3FFF)
                self.intreq |= 0x0002              # DSKBLK
            self.dsklen_prev = value16

    def _cust_read(self, uc, access, addr, size, value, ud):
        reg = addr & 0x1FE
        if reg == 0x1C:   v = self.intena
        elif reg == 0x1E: v = self.intreq
        elif reg == 0x02: v = self.dmacon
        elif reg == 0x04: self.vpos = (self.vpos + 1) & 0x1FF; v = (self.vpos >> 8) & 1
        elif reg == 0x06: self.vpos = (self.vpos + 7) & 0x1FFFF; v = (self.vpos & 0xFF) << 8 | (self.vpos & 0xFF)
        else: return
        self.wb(0xDFF000 + reg, struct.pack('>H', v & 0xFFFF))

    def _deliver_irq(self, uc, ret, level, vecno):
        self.req = (level, vecno, ret)
        uc.emu_stop()

    def _ring_hook(self, uc, addr, size, ud):
        if self._bypass:
            return
        r = self.ring
        self.total += 1
        if self.total >= self.budget:
            self.budget_hit = True
            uc.emu_stop()
        r.append(addr)
        if len(r) > 6000:
            del r[:3000]
        if self.total % self.vbl_period == 0:
            self.vbl_wait = True
        if self.vbl_wait:
            self.vbl_wait = False
            self.intreq |= 0x20
            self.vbl_count = getattr(self, 'vbl_count', 0) + 1
        # Never read SR here (stale flags).  Stop periodically while an
        # interrupt is pending and let run() decide with the exact SR.
        if (self.intreq & self.intena & 0x3FFF) and (self.intena & 0x4000) \
                and self.total >= self.next_irq_check and not self.pending \
                and not (size == 2 and self.rw(addr) == 0x4E73):
            self.next_irq_check = self.total + 150
            self.irq_poll = addr
            uc.emu_stop()
            return
        # QEMU's rte pops a 68020-style frame; do the 68000 one by hand
        if size == 2 and self.rw(addr) == 0x4E73:
            sp = self.r('a7')
            sr = self.rw(sp)
            pc = self.rl(sp + 2)
            self.w('a7', sp + 6)
            self.w('sr', sr)
            self.pending = pc
            uc.emu_stop()

    def dump_trace(self, n=30):
        try:
            from capstone import Cs, CS_ARCH_M68K, CS_MODE_M68K_000
        except ImportError:
            return
        md = Cs(CS_ARCH_M68K, CS_MODE_M68K_000)
        for a in self.ring[-n:]:
            try:
                ins = next(md.disasm(self.rb(a, 12), a))
                print('   %06x  %-18s %s %s' % (a, self.rb(a, ins.size).hex(), ins.mnemonic, ins.op_str))
            except Exception:
                print('   %06x  ?' % a)

    # -- running ------------------------------------------------------------
    def run(self, pc, max_insns=200_000_000, stop=None):
        """Run from pc. stop: optional set of addresses at which to stop."""
        stop = stop or set()
        def h(uc, addr, size, ud):
            if addr in stop:
                self.stopped_at = addr
                uc.emu_stop()
        hid = self.uc.hook_add(UC_HOOK_CODE, h)
        if self.trace_on:
            self.uc.hook_add(UC_HOOK_CODE, self._ring_hook)
        try:
            while True:
                self.pending = None
                self.req = None
                self.irq_poll = None
                self.stopped_at = None
                self.uc.emu_start(pc, 0xFFFFFFFF, count=max_insns)
                if self.budget_hit:
                    break
                if self.irq_poll is not None and self.req is None and self.pending is None:
                    at = self.irq_poll
                    self.w('pc', at)
                    sr = self.exact_sr()
                    mask = (sr >> 8) & 7
                    act = self.intreq & self.intena & 0x3FFF
                    lvl_bits = ((6, 0x2000), (5, 0x1800), (4, 0x0780), (3, 0x0070), (2, 0x0008), (1, 0x0007))
                    for lvl, bits in lvl_bits:
                        if act & bits and lvl > mask:
                            pc = self._enter_exception(lvl, 24 + lvl, at)
                            break
                    else:
                        pc = at
                    continue
                if self.req is not None:
                    level, vecno, ret = self.req
                    self.w('pc', ret)
                    pc = self._enter_exception(level, vecno, ret)
                    continue
                if self.pending is None:
                    break
                pc = self.pending
        finally:
            self.uc.hook_del(hid)
        return self.r('pc')
