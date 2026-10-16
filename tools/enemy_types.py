"""List enemy types of the level script: counts per dictionary word with the
task routine entry.   enemy_types.py assets LEVEL"""
import os, sys, collections
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import pam as pamlib
root, level = sys.argv[1], int(sys.argv[2])
prog = open(os.path.join(root, 'AMPROG.OBJ'), 'rb').read()
B = 0xEFC0
names = pamlib.names_from_prog(prog)
ni, doff, _ = pamlib.level_table(prog)[level]
dic = pamlib.dictionary(prog, doff, doff + 0x300, names)
recs = pamlib.decode(open(os.path.join(root, names[ni].upper()), 'rb').read())
tab = {}
p = 0x16422 - B
while True:
    i = int.from_bytes(prog[p:p+2], 'big')
    if i == 0: break
    tab[i] = 0x16564 + 2 * int.from_bytes(prog[p+2:p+4], 'big')
    p += 4
cnt = collections.Counter()
for r in recs:
    if r[0] == 'object' and r[2] != 0:
        a, bank = dic[r[3]]
        cnt[(a << 8 | names.index(bank), r[2])] += 1
for (w, k), c in sorted(cnt.items(), key=lambda x: -x[1]):
    print('word %04x kind %d count %3d entry %s' % (w, k, c, hex(tab[w]) if w in tab else '?'))
