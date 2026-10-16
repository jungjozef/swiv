"""Survey every task instance of one level of the original: python _survey.py adf out.json level last_frame"""
import json, os, sys
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from amiga68k import Amiga
adf, out, LVL, last = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
am = Amiga(adf, verbose=False)
am.boot_kernel()
frame = 0
live = {}      # (t,id) -> instance dict
inst = []
def s16(v): return v - 0x10000 if v & 0x8000 else v
def on_step(pc):
    a5, a6 = am.r('a5'), am.r('a6')
    if a5 < 0x1000 or a5 > 0x200000: return
    i = am.rw(a5 + 0x170)
    k = (a5, i)
    d = live.get(k)
    if d is None or frame - d['last'] > 6:
        d = dict(id=i, t=a5, f=frame, last=frame, n=0,
                 x=s16(am.rl(a5 + 0x140) >> 16), y=s16(am.rl(a5 + 0x144) >> 16) - s16(am.rw(a6 + 0xdd6)),
                 vx=am.rl(a5 + 0x14c), vy=am.rl(a5 + 0x150), hp=s16(am.rw(a5 + 0x168)),
                 img=am.rw(a5 + 0x1da), cl=am.rw(a5 + 0x1f8), par=am.rl(a5 + 0x134), k=am.rw(a5 + 0x114),
                 sc=am.rw(a6 + 0xdd6), imgs=[])
        live[k] = d
        inst.append(d)
    d['last'] = frame
    d['n'] += 1
    im = am.rw(a5 + 0x1da)
    if im not in d['imgs'] and len(d['imgs']) < 40: d['imgs'].append(im)
am.watch[0x15292] = on_step
for frame in range(last + 1):
    am.fire0 = am.fire1 = (frame % 50) in range(5, 10)
    a6 = am.r('a6')
    if a6:
        am.ww(a6 + 0x2ba8 + 0x6c, 0x7000)
        am.ww(a6 + 0xac, LVL)
    am.run(max_cycles=141648)
json.dump(inst, open(out, 'w'))
print(len(inst), 'instances')
