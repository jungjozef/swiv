"""Boot the kernel + game under Musashi and log every file the game opens."""
import sys
sys.path.insert(0, __file__.rsplit('/', 1)[0])
from amiga68k import Amiga

FRAMES = int(sys.argv[2]) if len(sys.argv) > 2 else 600
am = Amiga(sys.argv[1] if len(sys.argv) > 1 else 'SWIV.adf', verbose=False)
am.boot_kernel()


def on_find(pc):
    name = am.cstr(am.r('a0'))
    print('frame %4d  FindFile %r' % (am.vbl_count, name))


def on_read(pc):
    print('frame %4d    Read  dest=%06x count=%d' % (am.vbl_count, am.r('a0'), am.r('d0')))


am.watch[0x796] = on_find
am.watch[0xB14] = on_read
# tap fire on port 1 and 0 now and then so title/attract screens advance
import itertools
for f in range(0, FRAMES):
    am.fire0 = am.fire1 = (f % 50) in range(5, 10)
    am.run(max_cycles=141648)
print('done: frame', am.vbl_count, 'pc=%x' % am.cpu.r_pc(), 'tracks', [t for t, _, _ in am.disk_reads][-6:])
