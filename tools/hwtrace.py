"""Log distinct custom/CIA register accesses made by the kernel."""
import sys, collections
sys.path.insert(0, __file__.rsplit('/', 1)[0])
from amiemu import *
am = Amiga('SWIV.adf', verbose=False)
BASE = 0x70000
am.wb(BASE, am.adf[0xD9400:0xD9400 + 0x2C00])
am.wb(0x90, am.adf[0x42:0xB2])   # boot block (sector 0) copies this stub to $90
am.w('a0', BASE); am.w('a3', BASE); am.w('a4', am.ioreq); am.w('a1', am.ioreq); am.w('a6', 0x100)
am.w('sr', 0x2000); am.w('a7', 0x7000); am.w('sr', 0); am.w('a7', BASE + 0x70)
am.budget = int(sys.argv[1]) if len(sys.argv) > 1 else 3_000_000
W = collections.OrderedDict(); Rd = collections.Counter()
def wr(uc, acc, addr, size, val, ud):
    if 0xBFD000 <= addr < 0xBFF000 or 0xDFF000 <= addr < 0xDFF200:
        k = (addr, size)
        W.setdefault(k, set())
        if len(W[k]) < 6: W[k].add(val & 0xFFFFFFFF)
def rd(uc, acc, addr, size, val, ud):
    Rd[addr] += 1
am.uc.hook_add(UC_HOOK_MEM_WRITE, wr, begin=0xBFD000, end=0xDFFFFF)
am.uc.hook_add(UC_HOOK_MEM_READ, rd, begin=0xBFD000, end=0xDFFFFF)
am.run(BASE + 0x70)
print('insns', am.total, 'vbl', getattr(am, 'vbl_count', 0))
for (a, s), v in W.items():
    print('W %06x/%d %s' % (a, s, sorted(hex(x) for x in v)))
print('R', {hex(a): n for a, n in Rd.most_common(25)})
