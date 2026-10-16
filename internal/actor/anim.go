package actor

// animState is one animation channel of a task ($17C and $1A6 in the state
// block): a script of image words and commands, played by $15C7C.
//
// A script word is either an image word (bit 15 clear) or a command
// (bit 15 set: command = bits 11-14, operand = bits 0-10):
//
//	0 stop        1 end the task     2 loop back (count in $4; 0 = forever)
//	3 set loop start, count = operand
//	4 set $E  5 add to $E
//	6 set frame delay    7 set attribute bits (or)    8 clear attribute bits
type animState struct {
	script []uint16
	loop   int    // (a0): where "loop back" jumps to
	cur    int    // $6: cursor
	count  uint16 // $4
	delay  uint16 // $A: ticks until the next frame
	rate   uint16 // $C: frame delay
	e      int16  // $E
	on     bool   // $10
}

// play starts script a (the data that follows a "bsr $15C48").
func (w *World) newAnim(addr int) animState {
	return animState{script: w.Prog.AnimScript(addr), rate: 3, delay: 1, on: true}
}

// Play starts the animation script stored at original address addr on the
// task's main image channel ($15C48).
func (t *Task) Play(addr int) {
	t.anim[0] = t.w.newAnim(addr)
	t.Active = true
}

// PlayOverlay starts a script on the overlay channel ($15C42).
func (t *Task) PlayOverlay(addr int) { t.anim[1] = t.w.newAnim(addr) }

// AnimRunning reports whether the main channel is still playing.
func (t *Task) AnimRunning() bool { return t.anim[0].on }

// tick is $15C7C: advance the channel by one logic step.
func (t *Task) tickAnim(a *animState, attr *uint8, img *uint16) {
	if !a.on {
		return
	}
	dt := uint16(t.w.Dt)
	if a.delay > dt {
		a.delay -= dt
		return
	}
	a.delay = a.rate
	pos := a.cur
	for {
		if pos >= len(a.script) {
			a.on = false
			return
		}
		w := a.script[pos]
		pos++
		if w&0x8000 == 0 {
			*img = w
			a.cur = pos
			return
		}
		op := w & 0x7FF
		switch (w >> 11) & 0xF {
		case 0: // stop
			a.on = false
		case 1: // end the task
			a.on = false
			t.exit()
		case 2: // loop back
			if a.count == 0 {
				pos = a.loop
			} else {
				a.count--
				if a.count != 0 {
					pos = a.loop
				}
			}
		case 3: // loop start
			a.count = op
			a.loop = pos
		case 4:
			a.e = int16(op)
		case 5:
			a.e += int16(op)
		case 6:
			a.delay, a.rate = op, op
		case 7:
			*attr |= uint8(op)
		case 8:
			*attr &^= uint8(op)
		}
		if !a.on {
			return
		}
	}
}
