package actor

// Step is $15292, the call every task routine makes once per logic step.
// It integrates the position, runs the animations and the off-screen test,
// suspends until the next logic step and then handles what happened in the
// meantime. It returns false when the task has been told to terminate; the
// caller then ends the routine (the original: "bne" to $1930C).
func (t *Task) Step() bool {
	w := t.w
	if w.TraceStep != nil {
		w.TraceStep(t)
	}
	if t.killed {
		return false
	}

	// integrate: v += a, p += v, once per elapsed frame
	n := w.Dt
	if t.Flags&FlagFollow != 0 && t.Parent != nil {
		t.X, t.Y, t.Z = t.Parent.X, t.Parent.Y, t.Parent.Z
		n = 1
	}
	for i := 0; i < n; i++ {
		t.VX += t.AX
		t.VY += t.AY
		t.VZ += t.AZ
		t.X += t.VX
		t.Y += t.VY
		t.Z += t.VZ
	}
	if t.Z < 0 {
		t.Z, t.VZ, t.AZ = 0, 0, 0
	}

	// $15440: outside the playfield box (plus margin) the task is removed
	if !t.noDespawn {
		t.checkBounds()
	}

	t.tickAnim(&t.anim[0], &t.Attr, &t.Image)
	t.tickAnim(&t.anim[1], &t.Attr2, &t.Image2)

	t.prevScroll = w.Scroll
	t.ox, t.oy = int16(t.X>>16), int16(t.Y>>16) // $15D7A
	t.suspend()

	// after the other tasks have run
	if t.Flags&FlagScreenFixed != 0 {
		t.Y += int32(int16(w.Scroll-t.prevScroll)) << 16
	}
	t.Flags &^= FlagFlash
	if t.Parent == nil { // $21E callback: the parent has gone
		switch t.orphanCb {
		case orphanExit:
			t.exit()
		case orphanDie:
			t.Die()
		}
	}

	// $15428: while the smart bomb is on, every task with a $216 callback
	// takes it once per pass
	if t.cb216 != nil && w.auxGet(0xA9)&0xFF != 0 {
		t.cb216(t)
	}
	// $15476: events raised by the collision code
	t.dispatch()
	return !t.killed
}

// checkBounds is $15440 with the default callback (terminate the task).
func (t *Task) checkBounds() {
	sy := int16(t.Y>>16) - int16(t.w.Scroll)
	sx := int16(t.X >> 16)
	m := t.Margin
	out := false
	switch {
	case sy <= m, sx <= m:
		out = true
	case sy-0x100 >= -m:
		out = true
	case sx-0x140 >= -m:
		out = true
	}
	if out {
		t.exit()
	}
}

// exit is $15D74 (jsr -$586(a6)): ask for the task's own termination. The
// task carries on to the end of the current step; its next kill check
// (-$58A(a6)) then reports it and the routine ends through $1930C.
func (t *Task) exit() { t.killed = true }

// dispatch is $15476: run the handlers of the events that are set and
// listened to, in the original's order (0, 3, 4, 1, 2, 5), then clear them.
func (t *Task) dispatch() {
	for _, bit := range [...]int{0, 3, 4, 1, 2, 5} {
		if t.Event&t.Mask&(1<<bit) == 0 {
			continue
		}
		if h := t.handlers[bit]; h != nil {
			h(t)
		}
	}
	t.Event = 0
}

// Listen installs h as the handler of the given event bits ($154FE and
// friends: bit 0 $1FE, 3 $202, 4 $20A, 1 $206, 2 $20E, 5 $212).
func (t *Task) Listen(bits uint16, h func(*Task)) {
	for b := 0; b < 6; b++ {
		if bits&(1<<b) != 0 {
			t.handlers[b] = h
		}
	}
	t.Mask |= bits
}

// Unlisten removes handlers ($1554A and friends).
func (t *Task) Unlisten(bits uint16) { t.Mask &^= bits }

// defaultHit is $19322: one hit point per hit, the task ends at zero.
func (t *Task) defaultHit() {
	t.HP -= int16(1 + t.extra)
	t.extra = 0
	if t.HP <= 0 {
		t.Die()
		return
	}
	t.Sound(0x14030, int16(t.X>>16)) // $19312: the hit sound at x
	t.Flags |= FlagFlash
}

// KillHandler is $15D74 used as an event handler (the task just goes away).
func KillHandler(t *Task) { t.exit() }

// Die is $1932A: score, explosion, end of the task.
func (t *Task) Die() {
	if t.died { // two events in one pass (a bullet and a ram) kill it once
		return
	}
	t.died = true
	if t.w.Notify != nil {
		t.w.Notify(Event{Kind: Killed, Task: t})
	}
	// $178(a5): the routine the blast copy runs (default $1790A; the bigger
	// enemies set a ring of blasts, $17836, or the boss's sequence, $1A93C)
	blast := explosion
	if a := int(t.auxGet(0x178)); a != 0 {
		if f := funcAt[a]; f != nil {
			blast = func(c *Task) { f(c, &regs{}) }
		}
	}
	t.Clone(blast)
	if t.Debris != 0 {
		t.Clone(debris).P[0] = t.Debris // $1794C: the wreck left on the ground
	}
	// the object is gone from the picture and from the collision lists: a
	// routine that goes on after its death (the goose's checksum loop runs
	// for about 200 frames) no longer steps, and so no longer draws
	t.Active = false
	t.exit()
}

// WaitTicks is $1525C: step until d frames of the 50 Hz counter have passed.
// It returns false if the task was terminated.
func (t *Task) WaitTicks(d int) bool {
	target := t.w.Tick + uint16(d)
	for int16(t.w.Tick-target) < 0 {
		if !t.Step() {
			return false
		}
	}
	return !t.killed
}

// WaitSteps is $15278: d logic steps.
func (t *Task) WaitSteps(d int) bool {
	for i := 0; i < d; i++ {
		if !t.Step() {
			return false
		}
	}
	return !t.killed
}

// Forever is $1528C: step until terminated.
func (t *Task) Forever() {
	for t.Step() {
	}
}

// WaitScreenY is $18ABA / $18A88: step until the task's screen y reaches y.
func (t *Task) WaitScreenY(y int) bool {
	for uint16(int16(t.Y>>16)-int16(y)) < t.w.Logic {
		if !t.Step() {
			return false
		}
	}
	return !t.killed
}

// Construct is $19286: the common start of every enemy routine.
//
//	word   image word (image*2<<8 | bank)
//	flags  d1: bit 1 / bit 2 make the task answer the air / ground weapons
//	waitY  d2: the task sleeps until its screen y reaches this row
//	hp     d3: hit points (0 = cannot be hit)
//	score  d4, weight d5
func (t *Task) Construct(word uint16, flags, waitY, hp, score, weight int) {
	t.Wt = int16(weight)
	t.Pts = int16(score)
	t.Draw = word
	// the wait inside: $18A88
	for uint16(int16(t.Y>>16)-int16(waitY)) < t.w.Logic {
		if t.killed {
			break
		}
		t.suspend() // $14ECA: no integration while waiting
	}
	if !t.killed {
		t.w.Created++ // $192A4
	}
	t.Image = word
	t.HP = int16(hp)
	t.Class = uint16(flags) // $15D8E: the object block takes d1 as its class
	if hp != 0 {
		t.Listen(1, (*Task).defaultHit) // $19322
		t.cb216 = (*Task).defaultHit    // $192E6
	}
	if flags&4 != 0 {
		t.Listen(1<<4, (*Task).defaultHit)
	}
	if flags&2 != 0 {
		t.Listen(1<<3, (*Task).defaultHit)
	}
	t.w.Weight += weight
	t.counted = true
	t.Active = true
	// the bosses the scroll waits for: the last one (200 hits) and the big
	// multi-part ones (2000 points and up); the goose is not one of them
	t.boss = hp >= 200 || score >= 2000
	t.last = hp >= 200
}

// Clone is $15138: a copy of the task that continues at body, sharing the
// parent's position, motion and parameter words. It has no parent link.
func (t *Task) Clone(body func(*Task)) *Task {
	c := &Task{w: t.w, Margin: -64}
	c.X, c.Y, c.Z = t.X, t.Y, t.Z
	c.VX, c.VY, c.VZ = t.VX, t.VY, t.VZ
	c.AX, c.AY, c.AZ = t.AX, t.AY, t.AZ
	c.Speed, c.Angle = t.Speed, t.Angle
	c.P = t.P
	c.Draw = t.w.freed // the block comes from the free list: $170 still holds its last owner's word
	c.Attr = t.Attr & 1
	c.begin(body)
	t.w.add(c)
	return c
}

// Child is $15104/$1510A: a clone that is registered as the task's child
// ($134 = parent, linked into the parent's child list).
func (t *Task) Child(body func(*Task)) *Task {
	c := t.Clone(body)
	c.Parent = t
	c.orphanCb = orphanExit // $15110: every child starts with $15D74
	t.children = append(t.children, c)
	return c
}

// Formation is $19262: spawn n-1 copies of the task, each offset by (dx,dy)
// from the previous one with dp added to $114. The copies, and finally the
// task itself, continue at rest.
func (t *Task) Formation(dx, dy, n, dp int, rest func(*Task)) {
	for i := 0; i < n-1; i++ {
		t.Clone(rest)
		t.X += int32(dx) << 16
		t.Y += int32(dy) << 16
		t.P[0] += int16(dp)
	}
	rest(t)
}

// Velocity is $155B2: set the velocity from heading and speed.
func (t *Task) Velocity() {
	t.Angle &= 0xFF
	s, c := t.w.tab.sincos(t.Angle)
	sp := int32(int16(t.Speed))
	t.VX = c * sp
	t.VY = s * sp
}

// Turn is $1557E: rotate the heading towards (tx,ty) by at most step per
// call (step 0 snaps to the target heading).
func (t *Task) Turn(tx, ty int16, step int) {
	a := t.w.tab.aim(tx, ty, int16(t.X>>16), int16(t.Y>>16))
	if step == 0 {
		t.Angle = uint16(a)
		return
	}
	d := int(int8(a - uint8(t.Angle)))
	if d > step {
		d = step
	} else if d < -step {
		d = -step
	}
	t.Angle = uint16(int(t.Angle)+d) & 0xFF
}

// Rand is $177FC.
func (t *Task) Rand() uint32 { return t.w.next() }

// TargetPos is $16266/$162AE: the player to aim at. When none is alive the
// original aims at a point of the screen; ok is false then.
func (t *Task) TargetPos() (x, y int16, ok bool) {
	if t.w.Target != nil {
		return t.w.Target()
	}
	return 0, 0, false
}

// SetFlag/ClearFlag etc.
func (t *Task) SetFlags(f uint8) { t.Flags |= f }

// OnHit replaces the event-0 handler ($154FE); h is told the damage.
func (t *Task) OnHit(h func(*Task, int)) {
	t.Listen(1, func(t *Task) {
		n := 1 + t.extra
		t.extra = 0
		h(t, n)
	})
}

// NoDespawn is "st.b $21a(a5)": do not remove the task outside the box.
func (t *Task) NoDespawn() { t.noDespawn = true }

// Capacity is $177E2: false when the enemy weight cap ($A0) is exceeded.
func (t *Task) Capacity() bool { return t.w.Weight <= 0xA0 }
