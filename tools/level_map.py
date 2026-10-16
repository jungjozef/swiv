"""Compose a whole SWIV level image offline from its .PAM and the .LIN banks.

    python tools/level_map.py assets level_index out.png [all] [top height]   (top may be a 0x scroll value)

Objects of kind 0 are drawn with their bank/frame at (x - hotX, y - hotY).
By default only scenery banks (names starting with '_') and the .LIN
sprites are drawn; pass `all` to include everything.  Scroll counts down from
$E9C0, so the level start is at the bottom of the image.
"""
import os
import struct
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import pam as pamlib
from render import save_png


def parse_lin(data):
    n = struct.unpack_from('>H', data, 0)[0]
    p, frames = 2, []
    while p < len(data):
        size = struct.unpack_from('>H', data, p)[0]
        w, h, key, flags = data[p + 2], data[p + 3], data[p + 6], data[p + 7]
        hx, hy = struct.unpack_from('>bb', data, p + 4)      # signed hotspot
        px = data[p + 10:p + 10 + size]
        wpr = (w + 15) // 16
        idx = np.zeros((h, wpr * 16), dtype=np.uint8)
        raw = np.frombuffer(px, dtype=np.uint8).reshape(h, 4, wpr * 2)
        for plane in range(4):
            bits = np.unpackbits(raw[:, plane, :], axis=1)
            idx |= (bits << plane).astype(np.uint8)
        frames.append(dict(w=w, h=h, hx=hx, hy=hy, key=key, flags=flags, idx=idx[:, :w]))
        p += 10 + size
    return frames


def compose(root, level, draw_all=True, mirror=None, with_idx=False):
    """Return (image HxWx3 with 64px side margins, lo, palette)."""
    prog = open(os.path.join(root, 'AMPROG.OBJ'), 'rb').read()
    names = pamlib.names_from_prog(prog)
    lv = pamlib.level_table(prog)
    ni, doff, _ = lv[level]
    dend = lv[level + 1][1] if level + 1 < len(lv) else doff + 0x200
    dic = pamlib.dictionary(prog, doff, min(dend, doff + 0x300), names)
    recs = pamlib.decode(open(os.path.join(root, names[ni].upper()), 'rb').read())

    # palette = the first 16 colour commands (delta 0 at the start)
    pal = np.zeros((16, 3), dtype=np.uint8)
    for r in recs:
        if r[0] == 'colour' and r[1] > 0xE900:
            c, i = r[2], r[3]
            pal[i] = [((c >> 8) & 15) * 17, ((c >> 4) & 15) * 17, (c & 15) * 17]

    objs = [r for r in recs if r[0] == 'object' and r[2] == 0]
    skipped = sum(1 for r in recs if r[0] == 'object' and r[2] != 0)
    lo = min(r[1] for r in objs) - 128
    hi = pamlib.START_SCROLL + 256
    H, W = hi - lo, 320 + 2 * 64
    img = np.zeros((H, W, 3), dtype=np.uint8)
    iimg = np.zeros((H, W), dtype=np.uint8)
    img[:] = pal[0]
    cache, banks = {}, {}
    # Draw back to front: z = 4 - (number of $200 subtractions needed for x),
    # highest z first; within a layer the newest object (later in the script)
    # first, which is the order of the game's object list.
    def zof(r):
        v, k = r[4], 0
        while v >= 0x1A0 and k < 4:
            v -= 0x200
            k += 1
        return 4 - k
    order = sorted(range(len(objs)), key=lambda i: (-zof(objs[i]), -i))
    drawn = 0
    for i in order:
        _, scroll, kind, typ, pos, delta = objs[i]
        if typ >= len(dic):
            continue
        a, bank = dic[typ]
        if not draw_all and not (bank.startswith('_') or True):
            continue
        if bank not in banks:
            path = os.path.join(root, bank.upper())
            banks[bank] = images_of(parse_lin(open(path, 'rb').read())) if os.path.exists(path) else []
        images = banks[bank]
        fi = a // 2
        if fi >= len(images):
            continue
        if any(f['flags'] & 0x10 for f in images[fi]):
            continue                      # marker frame, never drawn by the game
        v = pos
        k = 0
        while v >= 0x1A0 and k < 4:
            v -= 0x200
            k += 1
        for f in images[fi]:                  # strips of one image
            vv = v if mirror is None else mirror - v
            x = vv - f['hx'] + 64
            y = (scroll - lo) - f['hy']
            x0, y0 = max(x, 0), max(y, 0)
            x1, y1 = min(x + f['w'], W), min(y + f['h'], H)
            if x0 < x1 and y0 < y1:
                sub = f['idx'][y0 - y:y1 - y, x0 - x:x1 - x]
                mask = sub != f['key']
                img[y0:y1, x0:x1][mask] = pal[sub[mask]]
                iimg[y0:y1, x0:x1][mask] = sub[mask]
            drawn += 1
    print('level %d %s: %d objects drawn, %d non-zero-kind skipped, image %dx%d' % (
        level, names[ni], drawn, skipped, W, H))
    if with_idx:
        return img, lo, pal, iimg
    return img, lo, pal


def images_of(frames):
    """Group file frames into images: a frame with flag bit 0 continues into
    the next one.  The game's frame index (dictionary byte A / 2) counts images."""
    imgs, cur = [], None
    for f in frames:
        if cur is None:
            cur = []
            imgs.append(cur)
        cur.append(f)
        if not (f['flags'] & 1):
            cur = None
    return imgs


def main():
    root, level = sys.argv[1], int(sys.argv[2])
    out = sys.argv[3]
    draw_all = len(sys.argv) > 4 and sys.argv[4] == 'all'
    img, lo, pal = compose(root, level, draw_all)
    # crop the 64px side margins used for objects partly off-screen
    view = img[:, 64:64 + 320]
    if len(sys.argv) > 6:                       # optional crop: top row, height
        y0, h = int(sys.argv[5], 0), int(sys.argv[6], 0)
        if sys.argv[5].lower().startswith('0x'):   # a scroll value, not a row
            y0 -= lo
        view = view[y0:y0 + h]
    save_png(out, view)


if __name__ == '__main__':
    main()
