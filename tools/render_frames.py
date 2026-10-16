"""Boot the game under emulation and save frames as PNG.

    python tools/render_frames.py SWIV.adf out_dir 450 720 900 ...
"""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from amiga68k import Amiga
from render import render_frame, save_png

adf, out = sys.argv[1], sys.argv[2]
frames = sorted(int(x) for x in sys.argv[3:]) or [450, 720]
os.makedirs(out, exist_ok=True)

am = Amiga(adf, verbose=False)
am.boot_kernel()
last = frames[-1]
want = set(frames)
for f in range(last + 1):
    am.fire0 = am.fire1 = (f % 50) in range(5, 10)
    am.run(max_cycles=141648)
    if f in want:
        notes = []
        rgb, info = render_frame(am, notes)
        path = os.path.join(out, 'frame_%05d.png' % f)
        save_png(path, rgb)
        print('frame %5d -> %s  %s %s' % (f, path, info, sorted(set(notes))))
