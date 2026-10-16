"""Boot the original Sales Curve kernel ($2C00 bytes at $D9400) under emulation."""
import sys
sys.path.insert(0, __file__.rsplit('/', 1)[0])
from amiemu import *

ADF = sys.argv[1] if len(sys.argv) > 1 else 'SWIV.adf'
am = Amiga(ADF)
am.budget = int(sys.argv[2]) if len(sys.argv) > 2 else 2_000_000
BASE = 0x70000
am.wb(BASE, am.adf[0xD9400:0xD9400 + 0x2C00])
am.wb(0x90, am.adf[0x42:0xB2])   # boot block (sector 0) copies this stub to $90
am.w('a0', BASE); am.w('a3', BASE); am.w('a4', am.ioreq); am.w('a1', am.ioreq)
am.w('a6', 0x100)
am.w('sr', 0x2000); am.w('a7', 0x7000)       # supervisor SP bank
am.w('sr', 0x0000); am.w('a7', BASE + 0x70)  # user SP bank
pc = BASE + 0x70
try:
    end = am.run(pc, max_insns=3_000_000)
    print('stopped at pc=%x after %d insns (budget_hit=%s)' % (end, am.total, am.budget_hit))
    am.dump_trace(25)
except UcError as e:
    print('error', e, 'pc=%x' % am.r('pc'))
    am.dump_trace(int(sys.argv[3]) if len(sys.argv)>3 else 40)
    print({k: hex(am.r(k)) for k in ['d0','d1','d2','d3','a0','a1','a2','a3','a4','a5','a6','a7','sr']})
print('exec calls:', sorted(set(am.calls)))
