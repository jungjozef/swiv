"""Decode SWIV .PAM level scripts.

A .PAM is a stream of big-endian 32-bit records, terminated by 0.  The game
walks it against the level scroll position (a 16-bit counter that starts at
$E9C0 and counts down); each record carries a distance (rows) since the
previous record.

  bit31 = 1   palette command (handler $12612 -> $139C4)
        bits 16..23  scroll delta
        bits  4..15  RGB444
        bits  0..3   colour index
  bit31 = 0   object
        bits  0..3   kind (0 = air/enemy path, nonzero = other)
        bits  4..11  type: index into the level's dictionary (word table)
        bits 12..19  scroll delta
        bits 20..31  position/flags (12 bits, see decode in $12612)

The per-level dictionary and the level->file table live in AMPROG.OBJ at
$1280C (6 bytes per level: u16 file-name index, u16 dictionary offset, u16 param).
"""
import re
import struct
import sys

BASE = 0xEFC0
LEVEL_TABLE = 0x1280C
START_SCROLL = 0xE9C0


def names_from_prog(prog):
    names, p = [], 0xEFC4 - BASE
    while True:
        e = prog.index(b'\0', p)
        chunk = prog[p:e]
        if not chunk or not re.fullmatch(rb'[ -~]{3,20}', chunk):
            break
        names.append(chunk.decode())
        p = e + 1
    return names


def level_table(prog, n=7):
    out = []
    for k in range(n):
        name_idx, off, param = struct.unpack_from('>HHH', prog, LEVEL_TABLE - BASE + 6 * k)
        out.append((name_idx, LEVEL_TABLE + off, param))
    return out


def dictionary(prog, start, end, names):
    """The level dictionary: u16 entries (byte A, byte bank-name index)."""
    raw = prog[start - BASE:end - BASE]
    ents = []
    for i in range(0, len(raw) - 1, 2):
        a, bank = raw[i], raw[i + 1]
        ents.append((a, names[bank] if bank < len(names) else '#%d' % bank))
    return ents


def decode(pam):
    recs = []
    scroll = START_SCROLL
    for (v,) in struct.iter_unpack('>I', pam[:len(pam) // 4 * 4]):
        if v == 0:
            recs.append(('end', scroll))
            break
        if v & 0x80000000:
            delta = (v >> 16) & 0xFF
            scroll -= delta
            recs.append(('colour', scroll, (v >> 4) & 0xFFF, v & 0xF, delta))
        else:
            kind, typ = v & 0xF, (v >> 4) & 0xFF
            delta, pos = (v >> 12) & 0xFF, (v >> 20) & 0x7FF
            if pos & 0x400:
                pos -= 0x800                 # 11-bit signed field
            if pos <= -92:
                pos += 0x800
            scroll -= delta
            recs.append(('object', scroll, kind, typ, pos, delta))
    return recs


if __name__ == '__main__':
    pam = open(sys.argv[1], 'rb').read()
    for r in decode(pam)[:int(sys.argv[2]) if len(sys.argv) > 2 else 60]:
        if r[0] == 'colour':
            print('%04x  colour %x = #%03x   (delta %d)' % (r[1], r[3], r[2], r[4]))
        elif r[0] == 'object':
            print('%04x  object kind=%x type=%02x pos=%03x (delta %d)' % (r[1], r[2], r[3], r[4], r[5]))
        else:
            print('%04x  end' % r[1])
