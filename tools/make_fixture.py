"""Compact a trace_enemies.py recording into the test fixture.

    python tools/make_fixture.py trace.json out.json.gz

Keeps, in the order the original ran them, the steps of the enemy types the
port covers (enemy fire, spark effects and the player's wreck are dropped),
each with the world values its task saw: scroll, frame counter, frame lag and
player position. Deaths refer to the step after which the dying task was shot
down.
"""
import gzip, json, sys

d = json.load(open(sys.argv[1]))

# (kept for the record: blasts and wrecks are tasks of the port now)
born = set()
_by_t = {}
for _i, _s in enumerate(d['steps']):
    _by_t.setdefault(_s['t'], []).append(_i)
for _dth in d['deaths']:
    for _i, _s in enumerate(d['steps']):
        if _s['x'] == _dth['x'] and _s['y'] == _dth['y'] and 0 <= _s['f'] - _dth['f'] <= 6 and _s['t'] != _dth['t']:
            run = [_i]
            for _j in _by_t[_s['t']]:
                if _j > run[-1] and d['steps'][_j]['f'] - d['steps'][run[-1]]['f'] <= 4:
                    run.append(_j)
            born.update(run)

def ported(s, i=None):
    """Every task the port runs: all but the scroller, the script loader and
    the helicopter's own task and control task."""
    if s['id'] == 0x31F and s['ra'] == 0x1528E and s['k'] != 768 and (s['z'] >> 16) != 33:
        return False          # the helicopter's task (its wreck pieces carry its type word)
    if 0x18050 <= s['ra'] < 0x18500 or 0x16000 <= s['ra'] < 0x16400:
        return False
    return True


# Steps in the order the original ran them, with the world values the task
# saw.  `after` of a death is the index (in this list) of the dying task's
# last step: the task is shot down in its next slice.
raw = d['steps']
steps, rawpos = [], {}
for i, s in enumerate(raw):
    if ported(s, i):
        rawpos[i] = len(steps)
        steps.append(dict(id=s['id'], x=s['x'], y=s['y'], z=s['z'], vx=s['vx'], vy=s['vy'],
                          ax=s['ax'], ay=s['ay'], sp=s['sp'], an=s['an'], k=s['k'],
                          sc=s['sc'], dc=s['dc'], n=s['n'], px=s['px'], py=s['py'], al=s['al'], tk=s['tk']))
deaths, last_step = [], {}
si = 0
for dth in d['deaths']:
    while si < len(raw) and raw[si]['f'] <= dth['f']:
        last_step[raw[si]['t']] = si
        si += 1
    i = last_step.get(dth['t'])
    if i is not None and i in rawpos:
        deaths.append(dict(after=rawpos[i], id=dth['id'], x=dth['x'], y=dth['y'], f=dth['f']))

out = dict(steps=steps, rnds=[dict(f=r['f'], seed=r['seed'], pc=r['pc']) for r in d['rnds']],
           deaths=deaths)
with gzip.open(sys.argv[2], 'wt') as f:
    json.dump(out, f, separators=(',', ':'))
print(len(steps), 'steps', len(deaths), 'deaths')
