package actor

import "slices"

// EventKind says what happened to a task.
type EventKind int

const (
	// Killed: the task's hit points ran out (score is Task.Pts, in the
	// original's units: shown x10).
	Killed EventKind = iota
)

// Event is reported to the embedding game.
type Event struct {
	Kind EventKind
	Task *Task
}

// World is the global state the task routines read and write: the part of the
// a6-relative variable block that enemies care about, plus the task list.
type World struct {
	Prog Prog
	tab  *tables

	Seed   uint32 // $2BA4: random generator state
	Scroll uint16 // $DD6: scroll value of the screen's top row (display)
	Logic  uint16 // $DCA: the same value as the game logic sees it (leads $DD6 by a frame)
	Tick   uint16 // -$42: 50 Hz frame counter
	Dt     int    // -$4C: frames elapsed per logic step (2 at 25 Hz)
	Rank   int    // $B6: difficulty rank (0 at the start of a game)
	Groups int    // $92: number of foddera formations started
	Weight int    // $9C: summed weight of live enemies (capped at $A0)

	// P1 and P2 are the players' records. P2 only exists to give the code
	// that picks a record by event bit something to write to.
	P1, P2 *PlayerRec

	rotoToggle uint16 // $8A: alternates the rotation sense of rotobases

	// Target returns the player to aim at: its world x and y ($46/$48 of the
	// player record). ok is false when no player is alive.
	Target func() (x, y int16, ok bool)

	// Decal is called when a destroyed object leaves its wreck on the
	// ground: image word and world position.
	Decal func(image uint16, x, y int)

	// Extents returns the half width and height of the collision box of an
	// image word ($1F4/$1F6: the picture's cx, cy).
	Extents func(image uint16) (c, e int)

	// Notify receives game events (kills).
	Notify func(Event)

	// SeedQueue, when not empty, supplies the generator state before each
	// call (the test harness replays the original's states).
	SeedQueue []uint32
	// TraceStep, when set, sees every task as it enters Step ($15292).
	TraceStep func(*Task)
	// BeforeSlice, when set, is called before each task runs (the test
	// harness sets the world as that task saw it).
	BeforeSlice func(*Task)

	aux     map[int]uint32
	Created int // enemies constructed ($30CE), for the statistics
	// Sound plays a sound effect: id is the address of its entry in the
	// original ($14030 ...), x the d0 the caller had (the pan position).
	Sound func(id, x int)
	cur   *Task  // the task whose slice is running
	boss  int    // passes the scroll has stood still for a boss
	freed uint16 // $170 of the most recently freed task block
	tasks []*Task
}

// New returns a world reading its tables from prog (AMPROG.OBJ).
func New(prog []byte) *World {
	p := Prog(prog)
	w := &World{Prog: p, tab: loadTables(p), Dt: 2, Seed: 1, P1: &PlayerRec{Active: true}, P2: &PlayerRec{}}
	w.auxSet(0xCE, 4) // the enemy bullet budget ($10D10)
	return w
}

// Tasks returns the live tasks in creation order.
func (w *World) Tasks() []*Task { return w.tasks }

// auxGet and auxSet hold the globals the port does not model by name.
func (w *World) auxGet(off int) uint32 { return w.aux[off] }
func (w *World) auxSet(off int, v uint32) {
	if w.aux == nil {
		w.aux = map[int]uint32{}
	}
	w.aux[off] = v
}

// Rand exposes the game's random generator.
func (w *World) Rand() uint32 { return w.next() }

// Run advances every task by one logic step (25 Hz).
func (w *World) Run() {
	// tasks created during the pass run in the same pass, after the others
	for i := 0; i < len(w.tasks); i++ {
		t := w.tasks[i]
		if t.done {
			continue
		}
		if w.BeforeSlice != nil {
			w.BeforeSlice(t)
		}
		w.cur = t
		t.resume()
	}
	w.cur = nil
	if w.bossAlive() {
		w.boss++
	} else {
		w.boss = 0
	}
	w.tasks = slices.DeleteFunc(w.tasks, func(t *Task) bool { return t.done })
}

// add registers a started task.
func (w *World) add(t *Task) { w.tasks = append(w.tasks, t) }

// exitTask is panicked inside a coroutine to unwind it to its entry.
type exitTask struct{}

// Start creates a task at (x,y) running body, with kind in $114 (what the
// script loader at $126BE does).
func (w *World) Start(x, y int16, kind int, body func(*Task)) *Task {
	t := &Task{w: w, Margin: -64}
	t.X = int32(x) << 16
	t.Y = int32(y) << 16
	t.P[0] = int16(kind)
	t.begin(body)
	w.add(t)
	return t
}

// Tasks are goroutines handing control back and forth over channels (never
// two at once, so the simulation stays deterministic). iter.Pull coroutines
// cannot be used: they must stay on the OS thread that created them, which
// the game loop of Ebitengine does not allow.
func (t *Task) begin(body func(*Task)) {
	t.in = make(chan bool)
	t.out = make(chan bool, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(exitTask); !ok {
					panic(r)
				}
			}
			t.out <- true // finished
		}()
		if !<-t.in {
			return
		}
		body(t)
	}()
}

// resume runs the task until its next suspension.
func (t *Task) resume() {
	t.in <- true
	if <-t.out {
		t.finished = true
		t.finish()
	}
}

// suspend gives control back to the scheduler until the next logic step.
func (t *Task) suspend() {
	t.out <- false
	if !<-t.in {
		panic(exitTask{})
	}
}

// finish is $192F6 followed by $15248: release the weight, orphan the
// children and leave the parent's list.
func (t *Task) finish() {
	if t.done {
		return
	}
	t.done = true
	t.Active = false
	t.w.freed = t.Draw
	if t.counted {
		t.w.Weight -= int(t.Wt)
	}
	for _, c := range t.children {
		c.Parent = nil
	}
	t.children = nil
	if p := t.Parent; p != nil {
		p.children = slices.DeleteFunc(p.children, func(c *Task) bool { return c == t })
		t.Parent = nil
	}
	if t.in != nil && !t.finished {
		close(t.in) // unwinds a parked task
	}
}

// Close stops every task (level restart).
func (w *World) Close() {
	for _, t := range w.tasks {
		t.finish()
	}
	w.tasks = nil
}
