"""Disassemble a range of AMPROG.OBJ:  disrange.py START END  (hex addresses)

Knows the game's inline-data calling conventions:
  bsr $15c48 / $15c42   animation script words follow, ended by $8000
  bsr $15d36 / $15d88   one data word follows
"""
import sys
from capstone import Cs, CS_ARCH_M68K, CS_MODE_M68K_000
d = open('assets/AMPROG.OBJ', 'rb').read()
B = 0xEFC0
lo, hi = int(sys.argv[1], 16), int(sys.argv[2], 16)
md = Cs(CS_ARCH_M68K, CS_MODE_M68K_000)
w = lambda a: int.from_bytes(d[a - B:a - B + 2], 'big')
a = lo
while a < hi:
    try:
        i = next(md.disasm(d[a - B:a - B + 12], a))
    except StopIteration:
        print('%06x  dc.w $%04x' % (a, w(a))); a += 2; continue
    print('%06x  %-14s %s %s' % (a, d[a - B:a - B + i.size].hex(), i.mnemonic, i.op_str))
    a += i.size
    if i.mnemonic.startswith('bsr') and i.size == 4:
        t = int(i.op_str.split('$')[1], 16)
        if t in (0x15c48, 0x15c42):
            ws = []
            while True:
                v = w(a); ws.append(v); a += 2
                if v == 0x8000: break
            print('        anim: ' + ' '.join('%04x' % v for v in ws))
        elif t in (0x15d36, 0x15d88):
            print('        data: %04x' % w(a)); a += 2
