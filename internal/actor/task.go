package actor

// Flag bits of $16F.
const (
	FlagNoShadow    = 0x01 // the object has no shadow
	FlagFlash       = 0x02 // hit this tick
	FlagFollow      = 0x08 // position follows the parent ($134)
	FlagScreenFixed = 0x10 // y follows the scroll (object is fixed to the screen)
)

// Attr bits of $18D (the object's blit attributes).
const (
	AttrGround = 0x01 // drawn with the ground objects, below flyers
	AttrHidden = 0x80 // not drawn this frame
)

// Task is one enemy: the $222-byte state block of the original, reduced to
// the fields the ported routines use.
type Task struct {
	boss bool // a level boss: the scroll stops for it
	held bool // ... once it has come down to bossStopY
	last bool // the last boss (200 hit points)
	w    *World

	// Position, velocity and acceleration: 16.16 fixed point, in pixels and
	// pixels per integration step. Y is a scroll coordinate (screen y plus
	// the scroll value of the top row).
	X, Y, Z    int32 // $140 $144 $148
	VX, VY, VZ int32 // $14C $150 $154
	AX, AY, AZ int32 // $158 $15C $160

	Speed  uint16   // $164
	Angle  uint16   // $166: heading, 0..255
	HP     int16    // $168
	Pts    int16    // $16A: score value (shown x10)
	Wt     int16    // $172
	P      [8]int16 // $114..$122: general purpose words ($114 = kind, timers)
	Margin int16    // $16C
	Flags  uint8    // $16F
	Attr   uint8    // $18D
	Image  uint16   // $18E: current image (image*2<<8 | bank)
	Image2 uint16   // $1B8: overlay image
	Attr2  uint8    // $1B7
	Draw   uint16   // $170: the word the constructor was given
	Debris int16    // $176: pieces thrown when the task is destroyed

	Parent *Task
	// Active is true once the constructor has run: the task is drawn and can
	// be hit.
	Active bool

	anim [2]animState
	// Collision: the object block at $1E8. Class is $1F8 (what the object
	// is: bits 0-5 are the event bits it raises in the objects it touches,
	// bit 15 = passive), Event is $1FA (the classes of what touched it since
	// the task last ran), Mask is $1FC (the events with a handler).
	Class, Event, Mask uint16
	handlers           [6]func(*Task)
	extra              int         // test harness: damage beyond one hit point
	cb216              func(*Task) // $216: run every pass while the smart bomb ($A9) is on
	ox, oy             int16       // position as the collision list has it ($1F0/$1F2)
	noDespawn          bool        // st.b $21a(a5)
	orphanCb           orphanMode  // $21E: what a task without a parent does (children: end)
	killed             bool
	died               bool
	counted            bool
	done               bool
	prevScroll         uint16
	children           []*Task

	in       chan bool // scheduler -> task: true = run, closed = unwind
	out      chan bool // task -> scheduler: false = suspended, true = finished
	finished bool
	aux      map[int]uint32 // state-block words not modelled by name (lifted code)
}

// Weight is $172.
func (t *Task) Weight() int { return int(t.Wt) }

// PX and PY return the integer position.
func (t *Task) PX() int { return int(int16(t.X >> 16)) }
func (t *Task) PY() int { return int(int16(t.Y >> 16)) }
func (t *Task) PZ() int { return int(int16(t.Z >> 16)) }

// World returns the task's world.
func (t *Task) World() *World { return t.w }

// Done reports whether the task has ended.
func (t *Task) Done() bool { return t.done }

// Kill makes the task's next Step report termination ("kill" status of
// -$58A(a6)).
func (t *Task) Kill() { t.killed = true }

// Hit raises the player-bullet events (bits 0 and 6) with n hit points of
// damage; the handler runs inside the task's next Step ($15476). The game
// itself gets its events from Collide; this is for tests.
func (t *Task) Hit(n int) {
	t.Event |= 0x41
	t.extra += n - 1
}

// Hittable reports whether player bullets can damage the task.
func (t *Task) Hittable() bool { return t.Active && t.HP > 0 && !t.done }

// PlayerRec is the part of a player record ($2BA8 / $2C5C) the enemy and
// token code reads and writes.
type PlayerRec struct {
	Active  bool  // $37: the player is in the game
	Score   int   // $4C: score / 10
	Cool    int   // $62: frames between volleys
	Level   int   // $64: weapon level 1..6
	Pickups int   // $66: tokens collected (max 19)
	Type    uint8 // $68: weapon type, 0 parallel, $FF spread
	Shield  int   // $6A: shield (negative: just granted)
	Invuln  int   // $6C: ticks of invulnerability
}

// orphanMode is the callback at $21E, run after each step while $134 (the
// parent) is zero.
type orphanMode uint8

const (
	orphanNone orphanMode = iota // negative: none (the default of a free-standing task)
	orphanExit                   // $15D74: ask for the end
	orphanDie                    // $1932A: die with the blast
)
