"""Record every enemy task step of the running game.

    python tools/trace_enemies.py SWIV.adf out.json FIRST LAST

Hooks the per-tick task step ($15292) and the random generator ($177fc) of
AMPROG.OBJ and logs, per call, the task struct (a5) fields and the global scroll.
Used to verify the Go port's enemy movement against the original.
"""
import json, os, sys
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from amiga68k import Amiga

adf, out, first, last = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
am = Amiga(adf, verbose=False)
am.boot_kernel()
frame = 0
steps, rnds, frames, deaths = [], [], [], []

def s16(v): return v - 0x10000 if v & 0x8000 else v

def on_step(pc):
    a5, a6 = am.r('a5'), am.r('a6')
    if a5 < 0x1000 or a5 > 0x200000: return
    steps.append(dict(f=frame, t=a5,
        id=am.rw(a5 + 0x170), k=am.rw(a5 + 0x114),
        x=am.rl(a5 + 0x140), y=am.rl(a5 + 0x144), z=am.rl(a5 + 0x148),
        vx=am.rl(a5 + 0x14c), vy=am.rl(a5 + 0x150), vz=am.rl(a5 + 0x154),
        ax=am.rl(a5 + 0x158), ay=am.rl(a5 + 0x15c), az=am.rl(a5 + 0x160),
        sp=am.rw(a5 + 0x164), an=am.rw(a5 + 0x166), hp=am.rw(a5 + 0x168),
        fl=am.rb(a5 + 0x16f, 1)[0], img=am.rw(a5 + 0x1da),
        sc=am.rw(a6 + 0xdd6), dc=am.rw(a6 + 0xdca), n=am.rw(a6 - 0x4c), tk=am.rw(a6 - 0x42),
        px=am.rw(a6 + 0x2ba8 + 0x46), py=am.rw(a6 + 0x2ba8 + 0x48),
        al=am.rb(a6 + 0x2ba8 + 0x36, 1)[0], rk=am.rw(a6 + 0xb6), gr=am.rw(a6 + 0x92), wt=am.rw(a6 + 0x9c),
        ro=am.rw(a6 + 0x8a), sd=am.rl(a6 + 0x2ba4),
        ra=am.rl(am.r('a7')) & 0xFFFFFF))

def on_rnd(pc):
    a6 = am.r('a6')
    # the seed before the call and the address the call returns to
    rnds.append(dict(f=frame, t=am.r('a5'), seed=am.rl(a6 + 0x2ba4),
                     pc=am.rl(am.r('a7')) & 0xFFFFFF))

def on_death(pc):
    a5 = am.r('a5')
    deaths.append(dict(f=frame, t=a5, id=am.rw(a5 + 0x170), x=am.rl(a5 + 0x140), y=am.rl(a5 + 0x144),
                       fl=am.rb(a5 + 0x16f, 1)[0], pv=am.rw(am.r('a6') + 0xdd6), ps=am.rw(a5 + 0x174),
                       tk=am.rw(am.r('a6') - 0x42)))

am.watch[0x1932a] = on_death
am.watch[0x15292] = on_step
am.watch[0x177fc] = on_rnd
for frame in range(last + 1):
    am.fire0 = am.fire1 = (frame % 50) in range(5, 10)
    if frame < first:
        saved = dict(am.watch); am.watch.clear()
        am.run(max_cycles=141648)
        am.watch.update(saved)
    else:
        am.run(max_cycles=141648)
        a6 = am.r('a6')
        frames.append(dict(f=frame, sc=am.rw(a6 + 0xdd6), tk=am.rw(a6 - 0x42),
                           px=am.rw(a6 + 0x2ba8 + 0x46), py=am.rw(a6 + 0x2ba8 + 0x48),
                           al=am.rb(a6 + 0x2ba8 + 0x36, 1)[0], al2=am.rb(a6 + 0x2c5c + 0x36, 1)[0]))
json.dump(dict(steps=steps, rnds=rnds, frames=frames, deaths=deaths), open(out, 'w'))
print(len(steps), 'steps', len(rnds), 'rnd calls', len(deaths), 'deaths')
