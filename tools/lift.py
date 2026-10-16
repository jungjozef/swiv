#!/usr/bin/env python3
"""Lift routines of AMPROG.OBJ to Go.

    python tools/lift.py assets/AMPROG.OBJ -o internal/actor/lifted.go \
        17008:foddera 1767c:yellow ...

Each entry is ADDRESS[:name]. The output is one Go function per routine
(and per local subroutine / continuation they reach), written against the
runtime in internal/actor/lift_rt.go: the 68000 registers are fields of a
`regs` value, the condition codes are a `flags` value, and every instruction
becomes the statements that do what it does. Calls into the game's helper
layer (constructor, step, waits, formations, ...) are replaced by the Go
methods of the port (table HELPERS below), with the inline data some of them
take (animation scripts) resolved to addresses.

The output is deliberately close to the disassembly: it is meant to be
generated, diffed and verified against traces, not hand edited.
"""
import argparse, re, sys
from capstone import Cs, CS_ARCH_M68K, CS_MODE_M68K_000

BASE = 0xEFC0

# ---- state block (a5) and globals (a6) -------------------------------------
# off: (Go lvalue with {b} for the task, kind, size in bytes)
A5 = {}
for i in range(8):
    A5[0x114 + 2 * i] = ('{b}.P[%d]' % i, 'i16', 2)
for off, name in ((0x140, 'X'), (0x144, 'Y'), (0x148, 'Z'), (0x14c, 'VX'), (0x150, 'VY'), (0x154, 'VZ'),
                  (0x158, 'AX'), (0x15c, 'AY'), (0x160, 'AZ')):
    A5[off] = ('{b}.' + name, 'i32', 4)
A5.update({
    0x164: ('{b}.Speed', 'u16', 2), 0x166: ('{b}.Angle', 'u16', 2), 0x168: ('{b}.HP', 'i16', 2),
    0x16a: ('{b}.Pts', 'i16', 2), 0x16c: ('{b}.Margin', 'i16', 2), 0x16f: ('{b}.Flags', 'u8', 1),
    0x170: ('{b}.Draw', 'u16', 2), 0x172: ('{b}.Wt', 'i16', 2), 0x176: ('{b}.Debris', 'i16', 2),
    0x18d: ('{b}.Attr', 'u8', 1), 0x18e: ('{b}.Image', 'u16', 2), 0x1b7: ('{b}.Attr2', 'u8', 1),
    0x1b8: ('{b}.Image2', 'u16', 2),
})
A6 = {  # signed offsets from a6
    0x92: ('w.Groups', 'int16', 2), 0xb6: ('w.Rank', 'int16', 2), 0x9c: ('w.Weight', 'int16', 2),
    0xdd6: ('w.Scroll', 'u16', 2), 0xdca: ('w.Logic', 'u16', 2), -0x42: ('w.Tick', 'u16', 2),
    -0x4c: ('w.Dt', 'int16', 2), 0x8a: ('w.rotoToggle', 'u16', 2), 0x2ba4: ('w.Seed', 'u32', 4),
}
# the player record ($2BA8 / $2C5C) fields the token code touches
PREC = {
    0x37: ('{b}.Active', 'bool', 1), 0x46: ('{b}.X', 'int16', 2), 0x48: ('{b}.Y', 'int16', 2),
    0x4c: ('{b}.Score', 'int32', 4), 0x62: ('{b}.Cool', 'int16', 2), 0x64: ('{b}.Level', 'int16', 2),
    0x66: ('{b}.Pickups', 'int16', 2), 0x68: ('{b}.Type', 'u8', 1), 0x6a: ('{b}.Shield', 'int16', 2),
    0x6c: ('{b}.Invuln', 'int16', 2),
}
KIND_CONV = {'i32': 'cvI32', 'i16': 'cvI16', 'u16': 'cvU16', 'u8': 'cvU8', 'u32': 'cvU32'}


def field_get(lv, kind, fsize, rel, asz):
    """Go expression (uint32) for asz bytes at byte offset rel of a field."""
    if kind == 'bool':
        return 'b2u(%s)' % lv
    if kind == 'int16':
        v = 'uint32(uint16(%s))' % lv
    elif kind == 'int32':
        v = 'uint32(int32(%s))' % lv
    elif kind in ('i32', 'u32'):
        v = 'uint32(%s)' % lv
    else:
        v = 'uint32(%s)' % ('uint16(%s)' % lv if kind == 'i16' else lv)
    shift = (fsize - rel - asz) * 8
    if shift:
        v = '(%s>>%d)' % (v, shift)
    if asz < fsize:
        v = '(%s&%#x)' % (v, (1 << (8 * asz)) - 1)
    return v


def field_set(lv, kind, fsize, rel, asz, expr):
    shift = (fsize - rel - asz) * 8
    cur = field_get(lv, kind, fsize, 0, fsize)
    m = ((1 << (8 * asz)) - 1) << shift
    new = '(%s&^%#x | (%s&%#x)<<%d)' % (cur, m, expr, (1 << (8 * asz)) - 1, shift) if asz < fsize else expr
    if kind == 'int16':
        return '%s = int(cvI16(%s))' % (lv, new)
    if kind == 'int32':
        return '%s = int(cvI32(%s))' % (lv, new)
    conv = KIND_CONV[kind]
    return '%s = %s(%s)' % (lv, conv, new)


# ---- helper layer: address -> (go lines, zflag expr or None) ------------------
def S(r):  # signed word view of a register
    return 'int(int16(r.%s))' % r


HELPERS = {
    0x15292: dict(lines=['r.fl.z = t.Step()']),
    0x1528c: dict(lines=['t.Forever()', 'r.fl.z = false']),
    0x1525c: dict(lines=['r.fl.z = t.WaitTicks(%s)' % S('d0')]),
    0x1525a: dict(lines=['r.fl.z = t.WaitTicks(1)']),
    0x15278: dict(lines=['r.fl.z = t.WaitSteps(%s)' % S('d0')]),
    0x18aba: dict(lines=['r.fl.z = t.WaitScreenY(%s)' % S('d0')]),
    0x18a88: dict(lines=['r.fl.z = t.waitYield(%s)' % S('d0')]),
    0x19286: dict(lines=['t.Construct(uint16(r.d0), %s, %s, %s, %s, %s)' % (S('d1'), S('d2'), S('d3'), S('d4'), S('d5'))]),
    0x177fc: dict(lines=['r.d0 = t.Rand()']),
    0x177e2: dict(lines=['r.fl.z = !t.Capacity()']),
    0x15c48: dict(inline='anim', lines=['t.Play({data})']),
    0x15c42: dict(inline='anim', lines=['t.PlayOverlay({data})']),
    0x183a2: dict(lines=['t.PlayOverlay(0x183a6)']),
    0x15d36: dict(inline='word', lines=['t.Image = {data}', 't.Active = true']),
    0x15d88: dict(inline='word', lines=['t.Class = uint16({data})', 't.Active = true']),
    0x1437e: dict(lines=['// sound (pan from x)']),
    0x145d4: dict(lines=['// sound']),
    0x13bcc: dict(lines=['// sound set-up']), 0x13cb8: dict(lines=['// sound']), 0x13d2a: dict(lines=['// sound']),
    0x13dee: dict(lines=['// sound']), 0x13df8: dict(lines=['// sound']), 0x14298: dict(lines=['// sound']),
    0x14310: dict(lines=['// sound']), 0x143de: dict(lines=['// sound']), 0x143f6: dict(lines=['// sound']),
    0x1446c: dict(lines=['// sound']), 0x14ea0: dict(lines=['// pictures are loaded']),
    0x14ec2: dict(lines=['// release picture bank']),
    0x14eca: dict(lines=['t.suspend()']), 0x14ee0: dict(lines=['t.waitYieldTicks(1)']),
    0x14ef8: dict(lines=['for i := 0; i <= int(int16(r.d0)); i++ { t.suspend() }']),
    0x15440: dict(lines=['t.checkBounds()']),
    0x150a0: dict(lines=['t.orphanChildren()']),
    0x177f4: dict(lines=['// sound / flash']),
    0x177f8: dict(lines=['// sound / flash']),
    0x13fbe: dict(lines=['// sound']),
    0x13c72: dict(lines=['// sound / flash']),
    0x13bfc: dict(lines=['// sound']),
    0x14e9a: dict(inline='word', lines=['// bank preload']),
    0x1424a: dict(lines=['// sound']),
    0x15dca: dict(lines=['// remove the collision object']),
    0x13bdc: dict(lines=['// sound']),
    0x145c0: dict(lines=['// sound']),
    0x14ee2: dict(lines=['t.waitYieldTicks(int(int16(r.d0)))']),
    0x15d3c: dict(lines=['t.Image = uint16(r.d0)', 't.Active = true']),
    0x15d56: dict(lines=['t.clearMotion()']),
    0x19250: dict(lines=['r.d1 = uint32(t.Angle) + 8', 'r.d1 &= 0xf0', 'r.d1 <<= 5', 'r.d0 += r.d1', 't.Image = uint16(r.d0)', 't.Active = true']),
    0x14ebc: dict(inline='word', lines=['// bank release']),
    0x1931a: dict(lines=['t.Flags |= FlagFlash']),
    0x14090: dict(lines=['// sound']),
    0x14e9a: dict(inline='word', lines=['// bank preload']),
    0x155b2: dict(lines=['t.Velocity()']),
    0x1557e: dict(lines=['t.Turn(int16(r.d0), int16(r.d1), %s)' % S('d2')]),
    0x16266: dict(lines=['{ x, y := t.target(); r.d0, r.d1 = setW(r.d0, uint32(uint16(x))), setW(r.d1, uint32(uint16(y))) }',
                         'r.fl.z = true']),
    0x140f8: dict(lines=['// ground decoration at x']),
    0x13c18: dict(lines=['// sound']),
}
HELPERS[0x162ae] = HELPERS[0x16266]
# the sound entries ($13BDC ...): played through package sfx, d0 is the pan position
SOUNDS = [0x13bdc, 0x13bfc, 0x13c18, 0x13c72, 0x13cb8, 0x13d2a, 0x13d86, 0x13dee, 0x13e06, 0x13efe, 0x13fbe, 0x14030,
          0x14090, 0x140f8, 0x14194, 0x1424a, 0x14298, 0x14310, 0x1437e, 0x143de, 0x143f6, 0x1446c, 0x144fa, 0x14570,
          0x145c0, 0x145d4, 0x177f4, 0x177f8]
for a_ in SOUNDS:
    HELPERS[a_] = dict(lines=['t.Sound(%#x, int16(r.d0))' % a_])
# $162D2 (nearest player): with nobody alive it aims at the bottom centre of the screen
HELPERS[0x162d2] = dict(lines=['{ x, y := t.targetNearest(); r.d0, r.d1 = setW(r.d0, uint32(uint16(x))), setW(r.d1, uint32(uint16(y))) }',
                               'r.fl.z = true'])
# event handler registration: ($154FE etc.) bits, handler in a0
LISTEN = {0x154fe: 0x01, 0x1550a: 0x06, 0x1550c: 0x02, 0x15518: 0x04, 0x15524: 0x18, 0x15532: 0x10, 0x15526: 0x08, 0x1553e: 0x20}
UNLISTEN = {0x1554a: 0x01, 0x15552: 0x06, 0x15554: 0x02, 0x15566: 0x08, 0x1555c: 0x04, 0x15564: 0x18, 0x1556e: 0x10, 0x15576: 0x20}
for a_ in LISTEN:
    HELPERS[a_] = dict(listen=LISTEN[a_])
for a_ in UNLISTEN:
    HELPERS[a_] = dict(lines=['t.Unlisten(%#x)' % UNLISTEN[a_]])
HELPERS[0x1510a] = dict(spawn='Child')
HELPERS[0x15104] = dict(spawn='Child')
HELPERS[0x15138] = dict(spawn='Clone')
HELPERS[0x15120] = dict(spawn='Clone')
HELPERS[0x1513a] = dict(spawn='Clone')
END_TARGETS = {0x1930c: ['t.end()'], 0x15248: ['t.end()'], 0x1932a: ['t.Die()'], 0x15d74: ['t.exit()']}
SOFT_END = {0x15d74}   # `bsr`: ends the task at the next kill check, then returns
SPLIT = 0x19262   # formation: the code after the call is a separate function

LINK_CALLS = {-0x58e: ['// signal list'], -0x58a: ['r.fl.z = !t.Killed()'], -0x586: ['t.exit()'], -0x59e: ['t.suspend()']}

md = Cs(CS_ARCH_M68K, CS_MODE_M68K_000)


class Insn:
    def __init__(s, addr, size, text):
        s.addr, s.size, s.text = addr, size, text
        m = re.match(r'([a-z0-9]+)(?:\.([bwl]))?(?:\s+(.*))?$', text)
        s.mn, s.sz, rest = m.group(1), m.group(2), m.group(3) or ''
        s.ops = split_ops(rest)
        s.next = addr + size
        s.data = None      # inline data (address, words)


def split_ops(s):
    out, depth, cur = [], 0, ''
    for c in s:
        if c == '(':
            depth += 1
        if c == ')':
            depth -= 1
        if c == ',' and depth == 0:
            out.append(cur.strip())
            cur = ''
        else:
            cur += c
    if cur.strip():
        out.append(cur.strip())
    return out


def num(s):
    s = s.strip()
    neg = s.startswith('-')
    s = s.lstrip('-')
    v = int(s[1:], 16) if s.startswith('$') else int(s, 0)
    return -v if neg else v


class Prog:
    def __init__(s, data):
        s.d = data

    def word(s, a):
        return int.from_bytes(s.d[a - BASE:a - BASE + 2], 'big')

    def insn(s, a):
        i = next(md.disasm(s.d[a - BASE:a - BASE + 12], a), None)
        if i is None:
            return None
        text = i.mnemonic + (' ' + i.op_str if i.op_str else '')
        ins = Insn(a, i.size, text)
        if ins.mn.startswith('bsr') or ins.mn == 'bsr':
            pass
        if ins.mn == 'bsr' and ins.ops and ins.ops[0].startswith('$'):
            t = num(ins.ops[0])
            h = HELPERS.get(t, {})
            if h.get('inline') == 'anim':
                ws, p = [], a + i.size
                while True:
                    w = s.word(p)
                    ws.append(w)
                    p += 2
                    if w == 0x8000:
                        break
                ins.data = (a + i.size, ws)
                ins.next = p
            elif h.get('inline') == 'word':
                ins.data = (a + i.size, [s.word(a + i.size)])
                ins.next = a + i.size + 2
        return ins


COND = {'eq', 'ne', 'cc', 'cs', 'hi', 'ls', 'pl', 'mi', 'ge', 'lt', 'gt', 'le', 'vc', 'vs', 'hs', 'lo'}
CONDMAP = {'hs': 'cc', 'lo': 'cs'}


def is_branch(ins):
    return ins.mn == 'bra' or (ins.mn.startswith('b') and ins.mn[1:] in COND)


class Lifter:
    def __init__(s, prog, names, local):
        s.p = prog
        s.names = names
        s.local = local
        s.queue = []       # function entries to lift
        s.done = {}
        s.unhandled = []
        s.unported = set()

    def fname(s, a):
        return 'lift_' + s.names[a] if a in s.names else 'l_%x' % a

    def table_targets(s, base):
        """Queue the code a table of word offsets (from the table's own start)
        points at: entries are taken while the target lies in the local ranges."""
        for k in range(32):
            off = s.p.word(base + 2 * k)
            off = off - 0x10000 if off & 0x8000 else off
            tgt = base + 2 * k + off
            if not any(lo <= tgt < hi for lo, hi in s.local) or tgt & 1:
                break
            s.want(tgt)

    def want(s, a):
        if a not in s.done and a not in s.queue:
            s.queue.append(a)

    def run(s, entries):
        for e in entries:
            s.want(e)
        out = []
        while s.queue:
            a = s.queue.pop(0)
            s.done[a] = True
            out.append(s.lift(a))
        return out

    # -- discover the body of one function ------------------------------------
    def body(s, entry):
        insns, todo = {}, [entry]
        while todo:
            a = todo.pop()
            while a not in insns:
                i = s.p.insn(a)
                if i is None:
                    break
                insns[a] = i
                if i.mn in ('rts', 'rte'):
                    break
                if i.mn == 'bsr' and i.ops and i.ops[0].startswith('$') and num(i.ops[0]) == SPLIT:
                    s.want(i.next)       # continuation becomes its own function
                    break
                if is_branch(i):
                    t = num(i.ops[0])
                    if t not in END_TARGETS and t not in HELPERS:
                        todo.append(t)
                    if i.mn == 'bra':
                        break
                if i.mn == 'jmp':
                    break
                a = i.next
        return insns

    # -- emit ------------------------------------------------------------------------
    def lift(s, entry):
        insns = s.body(entry)
        targets = set()
        for i in insns.values():
            if is_branch(i) or i.mn == 'dbra':
                t = num(i.ops[-1])
                if t in insns:
                    targets.add(t)
        em = Emit(s, insns, targets, entry)
        return em.function()


class Emit:
    def __init__(s, L, insns, targets, entry):
        s.L, s.insns, s.targets, s.entry = L, insns, targets, entry
        s.lines = []
        s.ptr = {}          # address register -> ('const', addr) | ('task', var)
        s.postinc = []      # (address register, size) to add after the instruction
        s.stack = []        # sizes of pushed values
        s.used_stack = False
        s.rt114 = entry in (0x1b302,)   # tasks spawned from a table entry kept in $114

    def w(s, line):
        s.lines.append('\t' + line)

    # operand access -----------------------------------------------------------
    def reg_read(s, r, sz):
        r = 'r.' + r
        return {'b': '(%s&0xff)' % r, 'w': '(%s&0xffff)' % r, 'l': r}[sz]

    def reg_write(s, r, sz, e):
        r = 'r.' + r
        if sz == 'l':
            return '%s = %s' % (r, e)
        return '%s = set%s(%s, %s)' % (r, sz.upper(), r, e)

    def mem(s, op):
        """('a5'|'a6'|ptr var, offset) for disp(An), or None."""
        m = re.match(r'^(-?\$?[0-9a-fA-F]+)?\((a\d)\)$', op)
        if not m:
            return None
        off = num(m.group(1)) if m.group(1) else 0
        return off, m.group(2)

    def read(s, op, sz, signed=False):
        sz = sz or 'w'
        n = {'b': 8, 'w': 16, 'l': 32}[sz]
        if re.match(r'^d\d$', op):
            return s.reg_read(op, sz)
        if op.startswith('#'):
            v = num(op[1:])
            return '%#x' % (v & ((1 << n) - 1))
        m = re.match(r'^\((a\d)\)$', op)
        if m and s.ptr.get(m.group(1), ('',))[0] == 'prec_at':
            _, base, off = s.ptr[m.group(1)]
            return s.field(PREC, m.group(1), off, sz, None, base)
        e = s.progread(op, sz)
        if e:
            return e
        m = re.match(r'^(\$?[0-9a-f]+)\(a7\)$', op)
        if m:                       # a byte or word inside a pushed word
            off = num(m.group(1))
            if sz == 'b' and off in (0, 1):
                return '((r.st[len(r.st)-1] >> %d) & 0xff)' % (8 if off == 0 else 0)
            if sz == 'w' and off % 2 == 0:
                return 'r.st[len(r.st)-1-%d]' % (off // 2)
            raise Unhandled('read %s' % op)
        if op == '(a7)':
            return 'r.st[len(r.st)-1]'
        if op == '(a7)+':
            s.pop_after = True
            return 'r.st[len(r.st)-1]'
        m = s.mem(op)
        if m:
            off, an = m
            if an == 'a5':
                return s.field(A5, 'a5', off, sz, None, 't')
            if an == 'a6':
                return s.field(A6, 'a6', off, sz, None, None)
            p = s.ptr.get(an)
            if p and p[0] == 'task':
                return s.field(A5, an, off, sz, None, p[1])
            if p and p[0] == 'prec':
                return s.field(PREC, an, off, sz, None, p[1])
            if p and p[0] == 'player' and off in (0x46, 0x48) and sz == 'w':
                return 'uint32(uint16(t.targetCoord(%d)))' % (0 if off == 0x46 else 1)
            if p is None and an != 'a7' and any(fo <= off < fo + fs for fo, (_, _, fs) in PREC.items()):
                return s.field(PREC, an, off, sz, None, 'r.prec')
        m = re.match(r'^(\$[0-9a-f]+)\(pc, d(\d)\.w\)$', op)
        if m and sz == 'w':        # a table in the program, indexed by a data register
            return 'uint32(w.Prog.Word(%#x + int(int16(r.d%s))))' % (num(m.group(1)), m.group(2))
        if re.match(r'^a\d$', op):
            p = s.ptr.get(op)
            if p and p[0] in ('const', 'rt'):
                return 'r.%s' % op
        raise Unhandled('read %s' % op)

    def progread(s, op, sz):
        """Read through an address register that points into the program
        (tables): (aN), (aN)+, disp(aN), (aN, dM.w)."""
        m = re.match(r'^(-?\$?[0-9a-fA-F]+)?\((a\d)(?:, (d\d)\.([wl]))?\)(\+)?$', op)
        kind = s.ptr.get(m.group(2), (None,))[0] if m else None
        if not m or kind not in ('const', 'rt', None) or m.group(2) == 'a7':
            return None
        if kind is None and not (m.group(3) or m.group(5)):
            return None            # a bare (aN) of an unknown register is not assumed to be a table
        disp = num(m.group(1)) if m.group(1) else 0
        addr = 'int(r.%s)' % m.group(2)
        if disp:
            addr += '%+d' % disp
        if m.group(3):
            addr += ' + int(%s(r.%s))' % ('int16' if m.group(4) == 'w' else 'int32', m.group(3))
        n = {'b': 1, 'w': 2, 'l': 4}[sz]
        if m.group(5):
            s.postinc.append((m.group(2), n))
        if sz == 'b':
            return 'uint32(w.Prog.Byte(%s))' % addr
        if sz == 'w':
            return 'uint32(w.Prog.Word(%s))' % addr
        return '(uint32(w.Prog.Word(%s))<<16 | uint32(w.Prog.Word(%s+2)))' % (addr, addr)

    def field(s, table, an, off, sz, write_expr, base):
        asz = {'b': 1, 'w': 2, 'l': 4}[sz]
        if asz == 4 and 0x114 <= off < 0x122 and off % 2 == 0 and table is A5:
            b = base or 't'
            i = (off - 0x114) // 2
            if write_expr is None:
                return '(uint32(uint16(%s.P[%d]))<<16 | uint32(uint16(%s.P[%d])))' % (b, i, b, i + 1)
            return '{ v := %s; %s.P[%d], %s.P[%d] = int16(v>>16), int16(v) }' % (write_expr, b, i, b, i + 1)
        for fo, (lv, kind, fs) in table.items():
            if fo <= off and off + asz <= fo + fs:
                lvl = lv.replace('{b}', base or 't')
                if write_expr is None:
                    return field_get(lvl, kind, fs, off - fo, asz)
                return field_set(lvl, kind, fs, off - fo, asz, write_expr)
        if table is PREC:
            raise Unhandled('player record field %#x' % off)
        if an == 'a6' and off in (0x2bdf, 0x2c93) and write_expr is None:
            return 'b2u(w.%s.Active)' % ('P1' if off == 0x2bdf else 'P2')
        if an == 'a5' or (base and base != 'w'):
            key = off
            if write_expr is None:
                if off == 0x134 and asz == 4:
                    return 'b2u(%s.Parent != nil)' % (base or 't')
                return '%s.auxGet(%#x)' % (base or 't', key)
            return '%s.auxSet(%#x, %s)' % (base or 't', key, write_expr)
        if write_expr is None:
            return 'w.auxGet(%#x)' % (off & 0xffff)
        return 'w.auxSet(%#x, %s)' % (off & 0xffff, write_expr)

    def write(s, op, sz, e):
        sz = sz or 'w'
        if re.match(r'^d\d$', op):
            return s.reg_write(op, sz, e)
        if op == '-(a7)':
            s.stack.append({'b': 1, 'w': 2, 'l': 4}[sz])
            s.used_stack = True
            return 'r.st = append(r.st, %s)' % e
        if op == '(a7)':
            return 'r.st[len(r.st)-1] = %s' % e
        m = re.match(r'^\((a\d)\)$', op)
        if m and s.ptr.get(m.group(1), ('',))[0] == 'prec_at':
            _, base, off = s.ptr[m.group(1)]
            return s.field(PREC, m.group(1), off, sz, e, base)
        m = s.mem(op)
        if m:
            off, an = m
            if an == 'a5':
                return s.field(A5, 'a5', off, sz, e, 't')
            if an == 'a6':
                return s.field(A6, 'a6', off, sz, e, None)
            p = s.ptr.get(an)
            if p and p[0] == 'task':
                return s.field(A5, an, off, sz, e, p[1])
            if p and p[0] == 'prec':
                return s.field(PREC, an, off, sz, e, p[1])
            if p is None and an != 'a7' and any(fo <= off < fo + fs for fo, (_, _, fs) in PREC.items()):
                return s.field(PREC, an, off, sz, e, 'r.prec')
        raise Unhandled('write %s' % op)

    def post(s):
        for an, n in s.postinc:
            s.w('r.%s += %d' % (an, n))
        s.postinc = []
        if getattr(s, 'pop_after', False):
            s.w('r.st = r.st[:len(r.st)-1]')
            if s.stack:
                s.stack.pop()
            s.pop_after = False

    # control ---------------------------------------------------------------
    def goto_or_exit(s, t, cond=None):
        pre = 'if %s ' % cond if cond else ''
        if t in s.insns:
            s.w('%s{ goto L_%x }' % (pre, t) if cond else 'goto L_%x' % t)
            return
        if t in END_TARGETS:
            body = END_TARGETS[t] + ['return']
        elif t in HELPERS and not cond:
            # a tail jump into the helper layer: do the call, then return
            fake = Insn(0, 4, 'bsr.w $%x' % t)
            s.call(fake)
            s.w('return')
            return
        elif t in HELPERS:
            body = ['return']
        else:
            s.L.want(t)
            body = ['%s(t, r)' % s.L.fname(t), 'return']
        if cond:
            s.w('if %s { %s }' % (cond, '; '.join(body)))
        else:
            for b in body:
                s.w(b)

    def function(s):
        e = s.entry
        name = s.L.fname(e)
        order = sorted(s.insns)
        if order and order[0] != e:       # code reached from a branch sits below the entry
            s.targets.add(e)
            s.lines.append('\tgoto L_%x' % e)
        dead = False
        for a in order:
            i = s.insns[a]
            if a in s.targets:
                s.lines.append('L_%x:' % a)
                dead = False
            elif dead:
                continue
            if s.lines and s.lines[-1].strip().startswith(('return', 'goto')):
                pass
            s.lines.append('\t// %06x  %s' % (a, i.text))
            try:
                s.insn(i)
                s.post()
                dead = bool(s.lines) and s.lines[-1].strip().startswith(('return', 'goto')) and not s.lines[-1].strip().startswith('if')
            except Unhandled as ex:
                s.w('// TODO unhandled: %s' % ex)
                s.L.unhandled.append((a, i.text, str(ex)))
        head = ['// %s lifted from $%06x.' % (name, e), 'func %s(t *Task, r *regs) {' % name, '\tw := t.w',
                '\t_ = w', '\tvar a0t, a1t, a2t, a3t *Task', '\t_, _, _, _ = a0t, a1t, a2t, a3t']
        # drop what follows an unconditional return/goto up to the next label,
        # until no label is left that nothing jumps to
        for _ in range(4):
            out, skip = [], False
            for l in s.lines:
                if re.match(r'^L_[0-9a-f]+:$', l):
                    skip = False
                if not skip:
                    out.append(l)
                t_ = l.strip()
                if re.match(r'^(return|goto L_[0-9a-f]+)$', t_):
                    skip = True
            s.lines = out
            used = set(re.findall(r'goto (L_[0-9a-f]+)', '\n'.join(s.lines)))
            s.lines = [l for l in s.lines if not (re.match(r'^L_[0-9a-f]+:$', l) and l[:-1] not in used)]
        used = set(re.findall(r'goto (L_[0-9a-f]+)', '\n'.join(s.lines)))
        s.lines = [l for l in s.lines if not (re.match(r'^L_[0-9a-f]+:$', l) and l[:-1] not in used)]
        last = s.lines[-1].strip() if s.lines else ''
        tail = ([] if last.startswith(('return', 'goto')) else ['\treturn']) + ['}', '']
        wrap = ['// %s_entry runs the routine as a task body.' % name,
                'func %s_entry(t *Task) { %s(t, &regs{}) }' % (name, name), '']
        return '\n'.join(head + s.lines + tail + wrap)

    def insn(s, i):
        mn, sz, ops = i.mn, i.sz, i.ops
        if mn in ('nop',):
            return
        if mn == 'rts':
            s.w('return')
            return
        if mn == 'bra':
            s.goto_or_exit(num(ops[0]))
            return
        if is_branch(i):
            c = mn[1:]
            c = CONDMAP.get(c, c)
            s.goto_or_exit(num(ops[0]), 'r.fl.%s()' % c)
            return
        if mn == 'bsr':
            return s.call(i)
        if mn == 'jmp':
            m = re.match(r'^(-?\$[0-9a-f]+)\(a6\)$', ops[0])
            if m and num(m.group(1)) in LINK_CALLS:
                for l in LINK_CALLS[num(m.group(1))]:
                    s.w(l)
                s.w('return')
                return
            if ops[0] == '(a3)':        # the sound routine's return through the caller's address
                s.w('return')
                return
            raise Unhandled('jmp %s' % ops[0])
        if mn == 'jsr':
            m = re.match(r'^(-?\$[0-9a-f]+)\(a6\)$', ops[0])
            if m and num(m.group(1)) in LINK_CALLS:
                for l in LINK_CALLS[num(m.group(1))]:
                    s.w(l)
                return
            if ops[0] == '(a0)':       # the aiming routine, entered at $16266 or $1626a
                s.ptr['a0'] = ('player',)
                for l in HELPERS[0x16266]['lines']:
                    s.w(l)
                return
            raise Unhandled('jsr %s' % ops[0])
        if mn == 'lea':
            m = re.match(r'^(\$[0-9a-f]+)\(a6\)$', ops[0])
            if m and num(m.group(1)) in (0x2ba8, 0x2c5c):
                if num(m.group(1)) == 0x2c5c and s.ptr.get(ops[1]) == ('prec', 'w.P1'):
                    # "lea P1; btst #6,event; bne skip; lea P2": the record
                    # follows the event bit at run time
                    s.ptr[ops[1]] = ('prec', 't.rec()')
                else:
                    s.ptr[ops[1]] = ('prec', 'w.P1' if num(m.group(1)) == 0x2ba8 else 'w.P2')
                return
            m = re.match(r'^(\$[0-9a-f]+)\(pc\)$', ops[0])
            if m:
                s.ptr[ops[1]] = ('const', num(m.group(1)))
                s.w('r.%s = %#x' % (ops[1], num(m.group(1))))
                return
            m = re.match(r'^(-?\$[0-9a-f]+)\(pc, d(\d)\.([wl])\)$', ops[0])
            if m:                   # a table entry's address
                s.ptr[ops[1]] = ('rt',)
                s.w('r.%s = uint32(%#x + int(%s(r.d%s)))' % (ops[1], num(m.group(1)), 'int16' if m.group(3) == 'w' else 'int32', m.group(2)))
                return
            m = re.match(r'^(\$[0-9a-f]+)\(a6\)$', ops[0])
            if m:                   # an a6-relative variable: only used as a signal cookie
                s.ptr[ops[1]] = ('a6addr', num(m.group(1)))
                return
            m = s.mem(ops[0])       # lea $68(a0),a1 on a player record
            if m and s.ptr.get(m[1], ('',))[0] == 'prec':
                s.ptr[ops[1]] = ('prec_at', s.ptr[m[1]][1], m[0])
                return
            if m and s.ptr.get(m[1], ('',))[0] in ('const', 'rt'):
                s.ptr[ops[1]] = ('rt',)
                s.w('r.%s = r.%s + uint32(%d)' % (ops[1], m[1], m[0]))
                return
            raise Unhandled('lea')
        if mn in ('adda', 'suba'):
            if s.ptr.get(ops[1], ('',))[0] not in ('const', 'rt'):
                raise Unhandled(mn)
            tab = s.ptr.get(ops[1])
            m = re.match(r'^\((a\d), d\d\.w\)$', ops[0])
            if m and tab[0] == 'const' and mn == 'adda':
                # a table of word offsets from itself: the routines it names
                # are spawned or jumped to through an address register
                s.L.table_targets(tab[1])
            v = s.read(ops[0], sz or 'w')
            if sz == 'w':
                v = 'uint32(int32(int16(%s)))' % v
            s.w('r.%s %s= %s' % (ops[1], '+' if mn == 'adda' else '-', v))
            s.ptr[ops[1]] = ('rt',)
            return
        if mn == 'dbra':
            reg = ops[0]
            s.w('r.%s = setW(r.%s, r.%s-1)' % (reg, reg, reg))
            s.goto_or_exit(num(ops[1]), 'int16(r.%s) != -1' % reg)
            return
        if mn == 'movea':
            m = s.mem(ops[0])
            if m and m[1] == 'a5' and m[0] == 0x114 and getattr(s, 'rt114', False):
                s.ptr[ops[1]] = ('rt',)
                s.w('r.%s = uint32(uint16(t.P[0]))<<16 | uint32(uint16(t.P[1]))' % ops[1])
                return
            if m and m[1] == 'a5' and m[0] == 0x114:
                if getattr(s, 'stored_player', False):
                    s.ptr[ops[1]] = ('player',)
                else:                       # the shield child's player record
                    s.ptr[ops[1]] = ('prec', 'w.P1')
                return
            if m and m[1] == 'a5' and m[0] == 0x134:
                s.ptr[ops[1]] = ('task', ops[1] + 't')
                s.w('%st = t.parentTask()' % ops[1])
                return
            if ops[0] in ('(a7)+', '(a7)'):
                s.ptr[ops[1]] = ('rt',)
                s.w('r.%s = r.st[len(r.st)-1]' % ops[1])
                if ops[0] == '(a7)+':
                    s.w('r.st = r.st[:len(r.st)-1]')
                    if s.stack:
                        s.stack.pop()
                return
            if ops[0].startswith('a'):
                s.ptr[ops[1]] = s.ptr.get(ops[0])
                s.w('r.%s = r.%s' % (ops[1], ops[0]))
                return
            if m and m[1] == 'a5' and m[0] == 0x114 and getattr(s, 'rt114', False):
                pass
            if re.match(r'^d\d$', ops[0]) and ops[1] != 'a7':
                # the only use: the parent pointer read from $134(a5) into d0
                s.ptr[ops[1]] = ('task', ops[1] + 't')
                s.w('%st = t.parentTask()' % ops[1])
                return
            raise Unhandled('movea')
        S = {'b': 8, 'w': 16, 'l': 32}
        if mn == 'movem':
            return s.movem(i)
        if mn == 'move' and sz == 'l' and re.match(r'^a\d$', ops[0]) and ops[1] == '$114(a5)':
            kind = s.ptr.get(ops[0], ('',))[0]
            if kind == 'player':
                s.stored_player = True
                return
            if kind in ('const', 'rt'):          # a table pointer kept in the state block
                s.rt114 = True
                s.w('t.P[0], t.P[1] = int16(r.%s>>16), int16(r.%s)' % (ops[0], ops[0]))
                return
            raise Unhandled('store of a pointer')
        if mn == 'move' and sz == 'l' and re.match(r'^a\d$', ops[0]) and ops[1] == '-(a7)':
            if s.ptr.get(ops[0], ('',))[0] not in ('const', 'rt'):
                raise Unhandled('push of a pointer')
            s.w('r.st = append(r.st, r.%s)' % ops[0])
            s.stack.append(4)
            s.used_stack = True
            return
        if mn == 'move' and sz == 'w' and ops[0] == 'sr' and ops[1] == '-(a7)':
            s.w('r.st = append(r.st, 0) // the status register')
            s.stack.append(2)
            return
        if mn == 'move' and sz == 'w' and ops[0] == '(a7)+' and ops[1] == 'sr':
            s.w('r.st = r.st[:len(r.st)-1]')
            if s.stack:
                s.stack.pop()
            return
        if mn == 'move' and sz == 'w' and ops[1] == 'sr':
            return
        if mn in ('move', 'moveq'):
            if mn == 'moveq':
                v = num(ops[0][1:]) & 0xff
                src = '%#x' % (v | 0xffffff00 if v & 0x80 else v)
                s.w(s.write(ops[1], 'l', src))
                s.w('r.fl = fLogic(32, %s)' % s.read(ops[1], 'l'))
                return
            if ops[1].startswith('a'):
                raise Unhandled('move to An')
            if ops[1] == '$216(a5)' and sz == 'w':
                s.w('t.noBomb()')
                return
            if ops[1] == '$21e(a5)':
                # the callback run once the task has no parent: $15D74 ends
                # it, $1932A kills it with the blast; a negative word clears it
                p = s.ptr.get(ops[0]) if ops[0].startswith('a') else None
                if p and p[0] == 'const' and p[1] == 0x15d74:
                    s.w('t.orphanCb = orphanExit')
                elif p and p[0] == 'const' and p[1] == 0x1932a:
                    s.w('t.orphanCb = orphanDie')
                elif ops[0].startswith('#$ffff') or ops[0] == '#$ffff':
                    s.w('t.orphanCb = orphanNone')
                else:
                    raise Unhandled('$21e callback %s' % ops[0])
                return
            v = s.read(ops[0], sz)
            s.w(s.write(ops[1], sz, v))
            if ops[1] not in ('-(a7)',):
                s.w('r.fl = fLogic(%d, %s)' % (S[sz], v if re.match(r'^d\d$', ops[1]) is None else s.read(ops[1], sz)))
            s.post()
            return
        if mn in ('clr',):
            s.w(s.write(ops[0], sz, '0'))
            s.w('r.fl = fLogic(%d, 0)' % S[sz])
            return
        if mn in ('st', 'sf'):
            m = s.mem(ops[0])
            if m and m[0] == 0x216 and mn == 'st':
                s.w('t.noBomb()')
                return
            if m and m[0] == 0x21a:
                s.w('t.NoDespawn()' if mn == 'st' else '// sf $21a')
                return
            s.w(s.write(ops[0], 'b', '0xff' if mn == 'st' else '0'))
            return
        if mn == 'tst':
            s.w('r.fl = fLogic(%d, %s)' % (S[sz], s.read(ops[0], sz)))
            return
        if mn in ('cmp', 'cmpi', 'cmpa'):
            if ops[1].startswith('a') or ops[0].startswith('a'):
                raise Unhandled('cmpa')
            s.w('r.fl = fSub(%d, %s, %s)' % (S[sz], s.read(ops[1], sz), s.read(ops[0], sz)))
            return
        if mn in ('add', 'addi', 'addq', 'sub', 'subi', 'subq'):
            if ops[1] == 'a7' and mn in ('addq', 'addi'):
                n = num(ops[0][1:])
                while n > 0 and s.stack:
                    n -= s.stack.pop()
                    s.w('r.st = r.st[:len(r.st)-1]')
                return
            if ops[1].startswith('a'):
                raise Unhandled('add to An')
            d, sv = s.read(ops[1], sz), s.read(ops[0], sz)
            op = 'add' if mn.startswith('add') else 'sub'
            n = S[sz]
            res = '(%s %s %s)' % (d, '+' if op == 'add' else '-', sv)
            s.w('r.fl = f%s(%d, %s, %s)' % ('Add' if op == 'add' else 'Sub', n, d, sv))
            s.w(s.write(ops[1], sz, res))
            return
        if mn in ('and', 'andi', 'or', 'ori', 'eor', 'eori'):
            sym = {'and': '&', 'or': '|', 'eor': '^'}[mn.rstrip('i') if mn not in ('andi', 'ori', 'eori') else mn[:-1]]
            d, sv = s.read(ops[1], sz), s.read(ops[0], sz)
            s.w(s.write(ops[1], sz, '(%s %s %s)' % (d, sym, sv)))
            s.w('r.fl = fLogic(%d, %s)' % (S[sz], s.read(ops[1], sz)))
            return
        if mn == 'not':
            s.w(s.write(ops[0], sz, '^%s' % s.read(ops[0], sz)))
            s.w('r.fl = fLogic(%d, %s)' % (S[sz], s.read(ops[0], sz)))
            return
        if mn == 'neg':
            v = s.read(ops[0], sz)
            s.w('r.fl = fSub(%d, 0, %s)' % (S[sz], v))
            s.w(s.write(ops[0], sz, '(-%s)' % v))
            return
        if mn == 'ext':
            r = ops[0]
            if sz == 'w':
                s.w(s.write(r, 'w', 'sx8(r.%s)' % r))
            else:
                s.w(s.write(r, 'l', 'sx16(r.%s)' % r))
            s.w('r.fl = fLogic(%d, r.%s)' % (S[sz], r))
            return
        if mn == 'swap':
            s.w('r.%s = r.%s<<16 | r.%s>>16' % (ops[0], ops[0], ops[0]))
            s.w('r.fl = fLogic(32, r.%s)' % ops[0])
            return
        if mn in ('asl', 'lsl', 'asr', 'lsr', 'ror', 'rol'):
            if len(ops) == 1:
                cnt, dst = '1', ops[0]
            else:
                cnt, dst = (str(num(ops[0][1:])) if ops[0].startswith('#') else s.read(ops[0], 'l') + '&63'), ops[1]
            v = s.read(dst, sz)
            n = S[sz]
            if mn in ('asl', 'lsl'):
                e = '(%s << (%s))' % (v, cnt)
            elif mn == 'lsr':
                e = '(%s >> (%s))' % (v, cnt)
            elif mn == 'asr':
                sx = {8: 'sx8', 16: 'sx16', 32: 'uint32'}[n]
                e = 'uint32(int32(%s(%s)) >> (%s))' % (sx, v, cnt)
            elif mn == 'rol':
                e = '((%s << (%s)) | (%s >> (%d-(%s))))' % (v, cnt, v, n, cnt)
            else:
                e = '((%s >> (%s)) | (%s << (%d-(%s))))' % (v, cnt, v, n, cnt)
            s.w(s.write(dst, sz, e))
            s.w('r.fl = fLogic(%d, %s)' % (n, s.read(dst, sz)))
            return
        if mn == 'divu':
            d = s.read(ops[0], 'w')
            s.w('if v := %s; v != 0 { q, m := r.%s/v, r.%s%%v; r.%s = m<<16 | q&0xffff }' % (d, ops[1], ops[1], ops[1]))
            s.w('r.fl = fLogic(16, r.%s)' % ops[1])
            return
        if mn in ('muls', 'mulu'):
            a = s.read(ops[0], 'w')
            if mn == 'muls':
                s.w('r.%s = uint32(int32(int16(%s)) * int32(int16(r.%s)))' % (ops[1], a, ops[1]))
            else:
                s.w('r.%s = (%s) * (r.%s & 0xffff)' % (ops[1], a, ops[1]))
            s.w('r.fl = fLogic(32, r.%s)' % ops[1])
            return
        if mn in ('btst', 'bset', 'bclr', 'bchg'):
            bit = s.read(ops[0], 'l') + ('&31' if re.match(r'^d\d$', ops[1]) else '&7')
            nsz = 'l' if re.match(r'^d\d$', ops[1]) else 'b'
            v = s.read(ops[1], nsz)
            s.w('r.fl.z = (%s>>(%s))&1 == 0' % (v, bit))
            if mn != 'btst':
                f = {'bset': '|', 'bclr': '&^', 'bchg': '^'}[mn]
                s.w(s.write(ops[1], nsz, '(%s %s (1 << (%s)))' % (v, f, bit)))
            return
        raise Unhandled(i.text)

    def movem(s, i):
        ops = i.ops

        def regs(spec):
            out = []
            for part in spec.split('/'):
                m = re.match(r'^([da])(\d)(?:-\1(\d))?$', part)
                if not m:
                    raise Unhandled('movem list %s' % spec)
                lo, hi = int(m.group(2)), int(m.group(3) or m.group(2))
                out += ['%s%d' % (m.group(1), k) for k in range(lo, hi + 1)]
            return out
        size = 2 if i.sz == 'w' else 4
        mask = '&0xffff' if size == 2 else ''
        if ops[1] == '-(a7)':            # push: lowest register ends on top
            for r in reversed(regs(ops[0])):
                s.w('r.st = append(r.st, r.%s%s)' % (r, mask))
                s.stack.append(size)
            s.used_stack = True
            return
        if ops[0] in ('(a7)', '(a7)+') and re.match(r'^[da0-9/-]+$', ops[1]):
            rs = regs(ops[1])
            for k, r in enumerate(rs):
                if r[0] == 'd' and size == 2:
                    s.w('r.%s = setW(r.%s, r.st[len(r.st)-1-%d])' % (r, r, k))
                else:
                    s.w('r.%s = r.st[len(r.st)-1-%d]' % (r, k))
                    if r[0] == 'a':
                        s.ptr[r] = ('rt',)
            if ops[0] == '(a7)+':
                s.w('r.st = r.st[:len(r.st)-%d]' % len(rs))
                for _ in rs:
                    if s.stack:
                        s.stack.pop()
            return
        m = re.match(r'^\$46\(a0\)$', ops[0])
        if m and s.ptr.get('a0') == ('player',) and ops[1] == 'd0-d1':
            s.w('{ x, y := t.target(); r.d0, r.d1 = setW(r.d0, uint32(uint16(x))), setW(r.d1, uint32(uint16(y))) }')
            return
        # registers to / from the state block
        mm = s.mem(ops[1]) if re.match(r'^[da0-9/-]+$', ops[0]) else s.mem(ops[0])
        if mm and mm[1] == 'a5':
            to_mem = re.match(r'^[da0-9/-]+$', ops[0]) is not None
            rs = regs(ops[0] if to_mem else ops[1])
            off = mm[0]
            for k, r in enumerate(rs):
                o = off + k * size
                sz = 'w' if size == 2 else 'l'
                if to_mem:
                    s.w(s.field(A5, 'a5', o, sz, ('r.%s%s' % (r, mask)) if r[0] == 'd' else 'r.%s' % r, 't'))
                else:
                    src = s.field(A5, 'a5', o, sz, None, 't')
                    if r[0] == 'd' and size == 2:
                        s.w('r.%s = setW(r.%s, %s)' % (r, r, src))
                    else:
                        s.w('r.%s = %s' % (r, src))
            return
        raise Unhandled(i.text)

    def call(s, i):
        t = num(i.ops[0])
        if t in SOFT_END:
            for l in END_TARGETS[t]:
                s.w(l)
            return
        if t in END_TARGETS:
            for l in END_TARGETS[t]:
                s.w(l)
            s.w('return')
            return
        if t == SPLIT:
            s.L.want(i.next)
            s.w('t.Formation(%s, %s, %s, %s, func(t *Task) { %s(t, &regs{}) })' % (
                'int(int16(r.d0))', 'int(int16(r.d1))', 'int(int16(r.d2))', 'int(int16(r.d3))',
                s.L.fname(i.next)))
            s.w('return')
            return
        h = HELPERS.get(t)
        if h and 'listen' in h:
            p = s.ptr.get('a0')
            if not p or p[0] != 'const':
                raise Unhandled('listen without handler in a0')
            if p[1] == 0x15d74:
                fn = 'KillHandler'
            elif p[1] == 0x19322:
                fn = '(*Task).defaultHit'
            elif any(lo <= p[1] < hi for lo, hi in s.L.local):
                s.L.want(p[1])
                fn = 'func(t *Task) { %s(t, &regs{}) }' % s.L.fname(p[1])
            else:
                s.L.unported.add(p[1])
                s.w('// handler $%x (not lifted)' % p[1])
                return
            s.w('t.Listen(%#x, %s)' % (h['listen'], fn))
            return
        if h and 'spawn' in h:
            p = s.ptr.get('a0')
            if p and p[0] == 'rt' and h['spawn'] in ('Clone', 'Child'):
                s.w('a0t = t.%s(func(t *Task) { runAt(int(r.a0), t) })' % h['spawn'])
                s.ptr['a0'] = ('task', 'a0t')
                s.w('r.fl.z = false')
                return
            if not p or p[0] != 'const':
                raise Unhandled('spawn without entry in a0')
            if not any(lo <= p[1] < hi for lo, hi in s.L.local):
                s.L.unported.add(p[1])
                s.w('// spawn of $%x (not lifted)' % p[1])
                s.w('a0t = nil')
                s.ptr['a0'] = ('task', 'a0t')
                s.w('r.fl.z = true // no task: the caller skips its set-up')
                return
            s.L.want(p[1])
            fn = s.L.fname(p[1])
            if True:
                s.w('a0t = t.%s(func(t *Task) { %s(t, &regs{}) })' % (h['spawn'], fn))
                s.ptr['a0'] = ('task', 'a0t')
            s.w('r.fl.z = false // allocated: "ne"')
            return
        if h:
            if t in (0x16266, 0x162ae, 0x162d2):
                s.ptr['a0'] = ('player',)
            for l in h['lines']:
                if '{data}' in l:
                    d = i.data
                    if h['inline'] == 'anim':
                        l = l.replace('{data}', '%#x' % d[0])
                    else:
                        l = l.replace('{data}', '%#x' % d[1][0])
                s.w(l)
            return
        if any(lo <= t < hi for lo, hi in s.L.local):
            s.L.want(t)         # a subroutine of the routine family
            for an, p in s.ptr.items():
                if p and p[0] == 'prec':
                    s.w('r.prec = %s' % p[1])
            s.w('%s(t, r)' % s.L.fname(t))
        else:
            s.L.unported.add(t)
            s.w('t.unported(%#x) // bsr $%x' % (t, t))
            s.w('r.fl.z = true')


class Unhandled(Exception):
    pass


HEADER = '''// Code generated by tools/lift.py from AMPROG.OBJ; DO NOT EDIT.
// The routines are lifted statement by statement from the disassembly: every
// register is a field of regs, the condition codes live in regs.fl.

package actor

// regs is the register file of lifted code.
type regs struct {
	d0, d1, d2, d3, d4, d5, d6, d7 uint32
	a0, a1, a2, a3                 uint32 // address registers that point into the program
	prec                           *PlayerRec
	fl                             flags
	st                             []uint32
}

'''


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('prog')
    ap.add_argument('entries', nargs='+', help='ADDRESS[:name]')
    ap.add_argument('-o', '--out', default='-')
    ap.add_argument('--local', action='append', default=[],
                    help='LO-HI: code ranges whose subroutines are lifted too (others are reported)')
    a = ap.parse_args()
    prog = Prog(open(a.prog, 'rb').read())
    names, entries = {}, []
    for e in a.entries:
        ad, _, nm = e.partition(':')
        ad = int(ad, 16)
        entries.append(ad)
        if nm:
            names[ad] = nm
    local = [tuple(int(x, 16) for x in r.split('-')) for r in a.local]
    L = Lifter(prog, names, local)
    funcs = L.run(entries)
    table = ['// Lifted maps the routine entry addresses to their lifted bodies.',
             'var Lifted = map[int]Routine{']
    for ad in entries:
        table.append('\t%#x: %s_entry,' % (ad, L.fname(ad)))
    table.append('}\n')
    table.append('// funcAt maps the address of every lifted function to it, for the jumps and')
    table.append('// spawns the original makes through a register.')
    table.append('var funcAt map[int]func(*Task, *regs)\n\nfunc init() {\nfuncAt = map[int]func(*Task, *regs){')
    for ad in sorted(L.done):
        table.append('\t%#x: %s,' % (ad, L.fname(ad)))
    table.append('}\n}\n')
    text = HEADER + '\n'.join(funcs) + '\n' + '\n'.join(table)
    # a leading used-vars guard for the unused pointer temporaries
    if a.out == '-':
        sys.stdout.write(text)
    else:
        open(a.out, 'w').write(text)
    sys.stderr.write('%d functions, %d unhandled instructions\n' % (len(funcs), len(L.unhandled)))
    if L.unported:
        sys.stderr.write('calls with no mapping: %s\n' % ' '.join('$%x' % x for x in sorted(L.unported)))
    for ad, t, why in L.unhandled:
        sys.stderr.write('  %06x  %-34s %s\n' % (ad, t, why))


if __name__ == '__main__':
    main()
