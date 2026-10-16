package actor

// Runtime for the code produced by tools/lift.py: 68000 condition codes and
// register-width helpers. Lifted routines keep the original's registers as
// uint32 locals and its condition codes in a flags value, so that the Go is a
// faithful statement-by-statement image of the disassembly.

// flags is the 68000 CCR (X is not needed by the game's task routines).
type flags struct{ n, z, c, v bool }

func msb(size int, v uint32) bool { return v>>(size-1)&1 != 0 }
func mask(size int, v uint32) uint32 {
	if size == 32 {
		return v
	}
	return v & (1<<size - 1)
}

// fSub: flags of dst - src (cmp, sub, neg).
func fSub(size int, dst, src uint32) flags {
	dst, src = mask(size, dst), mask(size, src)
	r := mask(size, dst-src)
	return flags{
		n: msb(size, r), z: r == 0, c: src > dst,
		v: msb(size, dst) != msb(size, src) && msb(size, r) != msb(size, dst),
	}
}

// fAdd: flags of dst + src.
func fAdd(size int, dst, src uint32) flags {
	dst, src = mask(size, dst), mask(size, src)
	r := mask(size, dst+src)
	return flags{
		n: msb(size, r), z: r == 0, c: r < dst,
		v: msb(size, dst) == msb(size, src) && msb(size, r) != msb(size, dst),
	}
}

// fLogic: flags of a move or logical result (V and C cleared).
func fLogic(size int, r uint32) flags {
	r = mask(size, r)
	return flags{n: msb(size, r), z: r == 0}
}

func (f flags) eq() bool { return f.z }
func (f flags) ne() bool { return !f.z }
func (f flags) cs() bool { return f.c }
func (f flags) cc() bool { return !f.c }
func (f flags) hi() bool { return !f.c && !f.z }
func (f flags) ls() bool { return f.c || f.z }
func (f flags) mi() bool { return f.n }
func (f flags) pl() bool { return !f.n }
func (f flags) vs() bool { return f.v }
func (f flags) vc() bool { return !f.v }
func (f flags) ge() bool { return f.n == f.v }
func (f flags) lt() bool { return f.n != f.v }
func (f flags) gt() bool { return !f.z && f.n == f.v }
func (f flags) le() bool { return f.z || f.n != f.v }

// setB, setW replace the low byte / word of a data register.
func setB(r, v uint32) uint32 { return r&^0xFF | v&0xFF }
func setW(r, v uint32) uint32 { return r&^0xFFFF | v&0xFFFF }

func sx8(v uint32) uint32  { return uint32(int32(int8(v))) }
func sx16(v uint32) uint32 { return uint32(int32(int16(v))) }

func b2u(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// aux holds the state-block words the port does not model by name.
func (t *Task) auxGet(off int) uint32 { return t.aux[off] }
func (t *Task) auxSet(off int, v uint32) {
	if t.aux == nil {
		t.aux = map[int]uint32{}
	}
	t.aux[off] = v
}

// unported marks a call into original code the lifter has no mapping for.
func (t *Task) unported(addr int) { _ = addr }

// Killed reports the kill status of -$58A(a6): true after the task was asked
// to end.
func (t *Task) Killed() bool { return t.killed }

// Conversions used for stores into state-block fields.
func cvI32(v uint32) int32  { return int32(v) }
func cvI16(v uint32) int16  { return int16(v) }
func cvU16(v uint32) uint16 { return uint16(v) }
func cvU8(v uint32) uint8   { return uint8(v) }
func cvU32(v uint32) uint32 { return v }

// end is "bra $1930C": the task is over, whatever subroutine it is in.
func (t *Task) end() { panic(exitTask{}) }

// waitYieldTicks is $14EE2: sleep d frames without stepping.
func (t *Task) waitYieldTicks(d int) {
	target := t.w.Tick + uint16(d)
	for {
		if t.killed {
			return
		}
		t.suspend() // a do-while: it always yields once
		if int16(t.w.Tick-target) >= 0 {
			return
		}
	}
}

// noBomb is "st.b $216(a5)": the smart bomb does not touch the task.
func (t *Task) noBomb() { t.cb216 = nil }

// rec picks the player record by the event word ($1FA bit 6 = player 1).
func (t *Task) rec() *PlayerRec {
	if t.Event&0x40 != 0 {
		return t.w.P1
	}
	return t.w.P2
}

// DropTokens is $1B960: the boss's release of n tokens (per player) in a ring
// round (x, y).
func (w *World) DropTokens(x, y int16, n int) {
	w.Start(x, y, 0, func(t *Task) { lift_token_drop(t, &regs{d0: uint32(n)}) })
}

// Bomb starts the smart bomb ($17812).
func (w *World) Bomb(x, y int16) {
	w.Start(x, y, 0, func(t *Task) { lift_smart_bomb(t, &regs{}) })
}

// AuxBomb reports the smart bomb flag ($A9).
func (w *World) AuxBomb() uint32 { return w.auxGet(0xA9) & 0xFF }

// orphanChildren is $150A0: the children lose their parent link.
func (t *Task) orphanChildren() {
	for _, c := range t.children {
		c.Parent = nil
	}
	t.children = nil
}

// parentTask is "movea.l $134(a5),a0" followed by field accesses: a task that
// has lost its parent reads zeroes through the null pointer in the original,
// not a fault.
func (t *Task) parentTask() *Task {
	if t.Parent != nil {
		return t.Parent
	}
	return &Task{w: t.w}
}

// Flash is the white screen flash level ($2B9E, 0..$100) the smart bomb sets
// to $100; the display fades it by $2BA0 (-4) every frame.
func (w *World) Flash() int { return int(w.auxGet(0x2B9E) & 0xFFFF) }

// TickFlash is the per-frame fade ($118A2).
func (w *World) TickFlash() {
	w.tickFade()
	v := w.auxGet(0x2B9E) & 0xFFFF
	if v == 0 {
		return
	}
	v = (v + 0xFFFC) & 0xFFFF
	if v > 0x100 {
		v = 0x100
	}
	w.auxSet(0x2B9E, v)
}

// runAt runs the lifted function at an original address (a spawn through a
// register).
func runAt(addr int, t *Task) {
	if f := funcAt[addr]; f != nil {
		f(t, &regs{})
	}
}

// targetCoord is $46/$48(a0) of the player record the enemies aim at.
func (t *Task) targetCoord(axis int) int16 {
	x, y := t.target()
	if axis == 0 {
		return x
	}
	return y
}

// bossMaxPasses bounds how long the scroll stands still for a boss that
// cannot be destroyed (a safety net: 2 minutes at 25 passes per second).
const bossMaxPasses = 3000

// bossStopY is the screen row a boss must have reached for the scroll to stop:
// the bosses are constructed above the top edge, and stopping there would
// leave them out of the picture.
const bossStopY = 64

// bossAlive reports whether a boss task is on stage.
func (w *World) bossAlive() bool {
	for _, t := range w.tasks {
		if t.boss && t.Active && !t.done && !t.killed {
			if t.last {
				// the last boss raises no flag: it has to come into the picture
				if !t.held && int16(uint16(t.Y>>16)-w.Logic) >= bossStopY {
					t.held = true
				}
				if t.held {
					return true
				}
			} else if w.BossFight() {
				// the others count themselves in $8C and set $A6 bit 3
				// ($1A66E) once they are on stage, and out again at $1A67A
				return true
			}
		}
	}
	return false
}

// BossEngaged reports whether a level boss has come into play: the scroll
// stands still from the moment its constructor wait is over (it is on screen)
// until it is destroyed.
func (w *World) BossEngaged() bool { return w.boss > 0 && w.boss <= bossMaxPasses && w.bossAlive() }

// GameComplete is bit 3 of $3041: the last boss was destroyed ($1B16E).
func (w *World) GameComplete() bool { return w.auxGet(0x3041)&8 != 0 }

// BossFight is bit 3 of $A6: a big boss is on stage ($1A66E sets it, the last
// boss waits for it).
func (w *World) BossFight() bool { return w.auxGet(0xA6)&8 != 0 }

// SetAux sets a global the port does not model by name (tests, scene code).
func (w *World) SetAux(off int, v uint32) { w.auxSet(off, v) }

// Bosses lists the boss tasks on stage.
func (w *World) Bosses() []*Task {
	var out []*Task
	for _, t := range w.tasks {
		if t.boss && t.Active && !t.done && !t.killed {
			out = append(out, t)
		}
	}
	return out
}

// tickFade is $11870: $8E is a fade request, negative towards black (the black
// level $2BA2 rises by $10 to $100), positive back to the picture; it clears
// when the level is reached.
func (w *World) tickFade() {
	f := int16(w.auxGet(0x8E))
	l := w.auxGet(0x2BA2) & 0xFFFF
	switch {
	case f == 0:
	case f < 0:
		l += 0x10
		if l >= 0x100 {
			l = 0x100
			w.auxSet(0x8E, 0)
		}
		w.auxSet(0x2BA2, l)
	default:
		if l < 0x10 {
			l = 0
			w.auxSet(0x8E, 0)
		} else {
			l -= 0x10
		}
		w.auxSet(0x2BA2, l)
	}
}

// Black is the level $2BA2 the picture is faded to black by (0..$100).
func (w *World) Black() int { return int(w.auxGet(0x2BA2) & 0xFFFF) }

// Tokens is $30CA: the tokens picked up.
func (w *World) Tokens() int { return int(w.auxGet(0x30CA)) }

// GetAux reads a global the port does not model by name (tests).
func (w *World) GetAux(off int) uint32 { return w.auxGet(off) }

// Sound is a call into the sound code of the original (see package sfx).
func (t *Task) Sound(id int, x int16) {
	if t.w.Sound != nil {
		t.w.Sound(id, int(x))
	}
}
