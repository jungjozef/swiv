"""Spawn graph of one level of the original: parent task id -> clone/child entry address.
python _spawns.py adf out.json level last_frame"""
import json, os, sys, collections
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from amiga68k import Amiga
adf, out, LVL, last = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
am = Amiga(adf, verbose=False)
am.boot_kernel()
frame = 0
rec = collections.Counter()
first = {}
def mk(kind):
    def f(pc):
        a5 = am.r('a5')
        if a5 < 0x1000 or a5 > 0x200000: return
        k = (am.rw(a5 + 0x170), kind, am.r('a0') & 0xFFFFFF)
        rec[k] += 1
        first.setdefault(k, frame)
    return f
for a, n in ((0x15104, 'child'), (0x1510a, 'child2'), (0x15120, 'cloneW'), (0x15138, 'clone'), (0x1513a, 'clone2')):
    am.watch[a] = mk(n)
for frame in range(last + 1):
    am.fire0 = am.fire1 = (frame % 50) in range(5, 10)
    a6 = am.r('a6')
    if a6:
        am.ww(a6 + 0x2ba8 + 0x6c, 0x7000)
        am.ww(a6 + 0xac, LVL)
    am.run(max_cycles=141648)
json.dump([[k[0], k[1], k[2], v, first[k]] for k, v in rec.items()], open(out, 'w'))
print('done', frame)
