package actor

// Routine is the code of one enemy type.
type Routine func(*Task)

// Routines maps the original entry address of a type's task routine (the
// target of the table at $16422) to its Go port: the routines lifted from the
// disassembly (lifted.go).
var Routines = Lifted

const (
	typeTable = 0x16422
	codeBase  = 0x16564
)

// Entry resolves a dictionary word to the entry address of its routine
// ($1654A). ok is false for words the table does not list.
func (p Prog) Entry(word uint16) (addr int, ok bool) {
	for a := typeTable; ; a += 4 {
		id := p.Word(a)
		if id == 0 {
			return 0, false
		}
		if id == word {
			return codeBase + 2*int(p.Word(a+2)), true
		}
	}
}

func (t *Task) setXInt(v int) { t.X = int32(uint32(t.X)&0xFFFF | uint32(uint16(v))<<16) }
func (t *Task) setYInt(v int) { t.Y = int32(uint32(t.Y)&0xFFFF | uint32(uint16(v))<<16) }
func (t *Task) setZInt(v int) { t.Z = int32(uint32(t.Z)&0xFFFF | uint32(uint16(v))<<16) }
func (t *Task) addYInt(v int) { t.setYInt(t.PY() + v) }
func (t *Task) addXInt(v int) { t.setXInt(t.PX() + v) }
func abs16(v int16) int16 {
	if v < 0 {
		return -v
	}
	return v
}

// clearMotion is $15D56.
func (t *Task) clearMotion() {
	t.AX, t.AY, t.AZ = 0, 0, 0
	t.VX, t.VY, t.VZ = 0, 0, 0
	t.Speed = 0
}

// target is $16266 / $162AE: the player's position to aim at; with no player
// the original sweeps a point across the screen with the frame counter.
func (t *Task) target() (x, y int16) {
	if t.w.Target != nil {
		if x, y, ok := t.w.Target(); ok {
			return x, y
		}
	}
	d := int16(int8(t.w.Tick))
	if d < 0 {
		d = ^d
	}
	return 0x60 + d, d + int16(t.w.Scroll) + 0x80
}

// targetNearest is $162D2: the nearest living player; with none alive the
// bottom centre of the screen (x $A0, y $DCA + $C0).
func (t *Task) targetNearest() (x, y int16) {
	if t.w.Target != nil {
		if x, y, ok := t.w.Target(); ok {
			return x, y
		}
	}
	return 0xA0, int16(t.w.Logic) + 0xC0
}

// --- explosion -----------------------------------------------------------

// explosion is $1790A: the blast left behind by a destroyed enemy (a clone of
// the dying task with its motion cleared).
func explosion(t *Task) {
	t.Sound(0x13C18, int16(t.X>>16)) // $1790A: the blast's sound at x
	t.clearMotion()
	t.Active = true
	t.Play(0x17920)
	t.Flags |= FlagNoShadow
	t.setZInt(t.PZ() + 1)
	t.Forever()
}

// debris is $1794C: a picture (the word is in $114) stamped into the ground
// for good, then the task ends.
func debris(t *Task) {
	t.Attr |= AttrGround
	t.clearMotion()
	t.Z = 0
	t.NoDespawn()
	t.Image = uint16(t.P[0])
	t.Active = true
	if t.w.Decal != nil {
		t.w.Decal(t.Image, t.PX(), t.PY())
	}
	if t.WaitTicks(1) {
		t.addYInt(-0x140) // out of sight
		t.WaitTicks(1)
	}
}

// waitYield is $18A88: wait (without integrating) until the screen y reaches y.
func (t *Task) waitYield(y int) bool {
	for uint16(int16(t.Y>>16)-int16(y)) < t.w.Logic {
		if t.killed {
			return false
		}
		t.suspend()
	}
	return !t.killed
}
