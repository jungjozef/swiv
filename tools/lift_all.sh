#!/bin/sh
# Regenerates internal/actor/lifted.go: every enemy routine of the type table
# at $16422 (all levels), the fire/bullet/token/shield/smart-bomb code and the
# boss, lifted from AMPROG.OBJ.
OUT=${1:-internal/actor/lifted.go}
cd "$(dirname "$0")/.."
ENTRIES=$(.venv/bin/python - <<'PY'
d = open('assets/AMPROG.OBJ', 'rb').read()
B = 0xefc0
named = {0x17008: 'foddera', 0x1767c: 'yellow', 0x175ee: 'bird', 0x1890e: 'rotobase', 0x18ad6: 'mine',
         0x18b3e: 'train', 0x18e8a: 'medtank', 0x19bd2: 'camogun', 0x16994: 'mill', 0x1966e: 'popup',
         0x199a0: 'proxmine', 0x19ad0: 'flame', 0x1b74a: 'goose'}
extra = {0x178bc: 'player_death', 0x18592: 'fire', 0x1858a: 'fire_ahead', 0x18582: 'fire_slow',
         0x174f0: 'fire_missiles', 0x18698: 'token', 0x18820: 'shield_capsule', 0x188b2: 'shield_child',
         0x1b960: 'token_drop', 0x17812: 'smart_bomb',
         0x17836: 'blast_ring', 0x178ac: 'blast_small', 0x1a93c: 'blast_boss'}
ents = set()
p = 0x16422 - B
while True:
    i = int.from_bytes(d[p:p+2], 'big')
    if i == 0:
        break
    ents.add(0x16564 + 2 * int.from_bytes(d[p+2:p+4], 'big'))
    p += 4
out = []
for e in sorted(ents):
    out.append('%x:%s' % (e, named[e]) if e in named else '%x' % e)
for e, n in extra.items():
    if e not in ents:
        out.append('%x:%s' % (e, n))
print(' '.join(out))
PY
)
.venv/bin/python tools/lift.py assets/AMPROG.OBJ -o "$OUT" \
  --local 16564-1bc18 --local 17812-17850 --local 18698-1890e --local 178bc-17912 \
  $ENTRIES
gofmt -w "$OUT"
