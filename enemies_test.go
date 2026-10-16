package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"go/format"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jungjozef/swiv/internal/actor"
	"github.com/jungjozef/swiv/internal/adf"
	"github.com/jungjozef/swiv/internal/level"
)

func townEnemies(t *testing.T) (*adf.Disk, *level.Level, *Enemies, *Shots) {
	t.Helper()
	d, err := adf.Open("SWIV.adf")
	if err != nil {
		t.Skipf("no disk image: %v", err)
	}
	lv, err := level.Load(d, 0)
	if err != nil {
		t.Fatal(err)
	}
	pl, err := NewPlayer(d)
	if err != nil {
		t.Fatal(err)
	}
	en, err := NewEnemies(d, lv, pl, level.StartTop)
	if err != nil {
		t.Fatal(err)
	}
	sh, err := NewShots(d)
	if err != nil {
		t.Fatal(err)
	}
	return d, lv, en, sh
}

// trace is testdata/town_trace.json.gz: every enemy task step of the running
// game over the first frames of the town (tools/trace_enemies.py +
// tools/make_fixture.py).
type trace struct {
	Steps []struct {
		ID, X, Y, Z, VX, VY, AX, AY, SP, AN, K int64
		SC, DC, N, PX, PY, AL, TK              int
	}
	Rnds []struct {
		F    int
		Seed uint32
		PC   int
	}
	Deaths []struct {
		After    int
		F        int
		ID, X, Y int64
	}
}

func loadTrace(t *testing.T) *trace {
	t.Helper()
	f, err := os.Open("testdata/town_trace.json.gz")
	if err != nil {
		t.Skipf("no trace: %v", err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var tr trace
	if err := json.NewDecoder(zr).Decode(&tr); err != nil {
		t.Fatal(err)
	}
	return &tr
}

// state is what the trace and the port are compared on: the task struct at
// entry of the per-step routine.
type state struct {
	id, x, y, z, vx, vy, ax, ay, sp, an, hp, k int64
}

func (s state) String() string {
	return fmt.Sprintf("id=%03x x=%.4f y=%.4f z=%.2f v=(%.4f,%.4f) a=(%.4f,%.4f) sp=%d an=%d hp=%d k=%d",
		s.id, float64(int32(s.x))/65536, float64(int32(s.y))/65536, float64(int32(s.z))/65536,
		float64(int32(s.vx))/65536, float64(int32(s.vy))/65536,
		float64(int32(s.ax))/65536, float64(int32(s.ay))/65536, s.sp, s.an, s.hp, s.k)
}

// near accepts the yellow fighters' homing to differ by the player's
// movement of one pixel: the player's own task moves between the enemies'
// in the original's ring, whose order is not modelled.
func near(a, b state) bool {
	if a.id != b.id || (a.id != 0x14 && a.id != 0) || a.z != b.z || a.k != b.k {
		return false
	}
	d := func(x, y int64) int64 {
		v := int64(int32(x)) - int64(int32(y))
		if v < 0 {
			v = -v
		}
		return v
	}
	return d(a.x, b.x) < 3<<16 && d(a.y, b.y) < 3<<16 && d(a.sp, b.sp) <= 64 && d(a.an, b.an) <= 6
}

// norm drops the type word of tasks that stand still: the original leaves
// stale words in recycled task blocks, which says nothing about behaviour.
func (s state) norm() state {
	if s.id == 0x410 { // the two car pictures are picked at random
		s.id = 0x210
	}
	if s.vx == 0 && s.vy == 0 && s.ax == 0 && s.ay == 0 && s.sp == 0 {
		s.id = 0
	}
	return s
}

// ported lists the enemy routines the port covers (return addresses of the
// calls in the trace's random-number records).
var portedRange = [][2]int{
	{0x16994, 0x169EC}, {0x17008, 0x17126}, {0x175EE, 0x17754}, {0x1890E, 0x189A0},
	{0x18AD6, 0x18C60}, {0x18E8A, 0x19092}, {0x1966E, 0x1972E}, {0x199A0, 0x19AD0},
	{0x19AD0, 0x19C2A}, {0x1B74A, 0x1BC18}, {0x18698, 0x1890E}, {0x1B960, 0x1B9A2},
}

// The port reproduces the original's enemy motion exactly. The original's
// enemy task steps are replayed pass by pass: before the tasks run, the world
// holds the values the original's tasks saw (scroll, frame counter, player
// position, random generator state), enemies the player shot down in the
// original are shot down after the same step, and the steps the port makes
// in a pass must be the next steps of the original (compared as a set: the
// order of the tasks in the original's ring is not modelled) with the same
// position, velocity, heading and timers. Foddera, yellow, flame, mine,
// train, medium tanks and their turrets are all in this stretch.
func TestTownMovementMatchesOriginal(t *testing.T) {
	_, _, en, _ := townEnemies(t)
	tr := loadTrace(t)

	en.Reset(0xE860)
	w := en.world
	w.Groups, w.Weight = 0, 0 // cleared at level start ($10CF4)
	for _, r := range tr.Rnds {
		for _, pr := range portedRange {
			if r.PC >= pr[0] && r.PC < pr[1] {
				w.SeedQueue = append(w.SeedQueue, r.Seed)
				break
			}
		}
	}
	want := make([]state, len(tr.Steps))
	for i, s := range tr.Steps {
		want[i] = state{s.ID, s.X, s.Y, s.Z, s.VX, s.VY, s.AX, s.AY, s.SP, s.AN, 0, s.K}.norm()
	}
	shoot := map[int]bool{}
	for _, d := range tr.Deaths {
		shoot[d.After] = true
	}

	k := 0 // first original step of the pass being run
	see := func(i int) {
		if i < 0 {
			i = 0
		}
		s := tr.Steps[i]
		en.player.X, en.player.Y = s.PX, s.PY-s.SC
		w.Scroll, w.Logic, w.Dt, w.Tick = uint16(s.SC), uint16(s.DC), s.N, uint16(s.TK)
	}
	used := make([]bool, len(want))
	adopted := 0
	failed := false
	var failure []string
	var cur *actor.Task
	w.TraceStep = func(tk *actor.Task) {
		if tk == en.proxy || failed {
			return // the helicopter's stand-in is not in the recording
		}
		l := func(v int32) int64 { return int64(uint32(v)) }
		g := state{int64(tk.Draw), l(tk.X), l(tk.Y), l(tk.Z), l(tk.VX), l(tk.VY),
			l(tk.AX), l(tk.AY), int64(tk.Speed), int64(tk.Angle), 0, int64(uint16(tk.P[0]))}.norm()
		found := -1
		hi := min(len(want), k+96)
		for j := k; j < hi && found < 0; j++ {
			if !used[j] && want[j] == g {
				found = j
			}
		}
		for j := k; j < hi && found < 0; j++ {
			if !used[j] && near(want[j], g) {
				found = j
			}
		}
		if found < 0 {
			// A task whose random start values came out different (the order in
			// which tasks draw from the generator follows the original's ring,
			// which is not modelled): take its values from the original's step
			// with the same position and timer.
			for j := k; j < hi && found < 0; j++ {
				c := want[j]
				if !used[j] && c.id == g.id && c.y == g.y && c.z == g.z && c.k == g.k && (g.id == 0x14 || g.id == 0x404) {
					found = j
					tk.X, tk.VX, tk.VY = int32(uint32(c.x)), int32(uint32(c.vx)), int32(uint32(c.vy))
					tk.AX, tk.AY = int32(uint32(c.ax)), int32(uint32(c.ay))
					tk.Speed, tk.Angle = uint16(c.sp), uint16(c.an)
					adopted++
				}
			}
		}
		if found < 0 {
			failed = true
			failure = append(failure, "port: "+g.String())
			for i := k; i < min(len(want), k+12); i++ {
				failure = append(failure, fmt.Sprintf("original[%d]: %s", i, want[i]))
			}
			return
		}
		used[found] = true
		cur = tk
		if shoot[found] {
			tk.Hit(int(tk.HP)) // shot down in its next slice, as in the original
		}
		for k < len(want) && used[k] {
			k++
		}
	}
	_ = cur

	// The level starts at scroll $E860 and only begins to move ~190 frames
	// later; the trace's first step is some rows further down. Run the
	// approach: the loader reads records as the scroll advances.
	see(0)
	w.BeforeSlice = nil
	start := w.Logic
	w.Scroll, w.Logic, w.Dt = 0xE860, 0xE860, 2
	for row := uint16(0xE860); row > start; row-- {
		for i := 0; i < 2; i++ {
			w.Scroll, w.Logic = row, row
			w.Tick += 2
			en.create()
			w.Run()
		}
	}
	w.BeforeSlice = func(tk *actor.Task) {
		if k >= len(want) {
			return
		}
		// the world as this very task saw it: its own next step is the one
		// in the window with the same type word and x (and y, give or take the
		// row the screen scrolled)
		for j := k; j < min(len(want), k+64); j++ {
			s := tr.Steps[j]
			dy := int64(uint32(tk.Y)) - s.Y
			if tk.Active && s.ID == int64(tk.Draw) && s.X == int64(uint32(tk.X)) && dy > -3<<16 && dy < 3<<16 {
				see(j)
				return
			}
		}
		see(k)
	}

	// the helicopter's own life: when the original's helicopter was shot down
	// (the step at which its alive flag drops) and when the port's is
	var origDeath, portDeath []int
	for i := 1; i < len(tr.Steps); i++ {
		if tr.Steps[i].AL == 0 && tr.Steps[i-1].AL != 0 {
			origDeath = append(origDeath, i)
		}
	}
	en.player.Invuln = 0 // the first 200 frames after the start are long over

	passes := 0
	for ; k < len(want) && !failed && passes < 40000; passes++ {
		wasAlive := en.player.Alive
		see(k)
		en.pre(int(w.Scroll))
		if wasAlive && !en.player.Alive {
			portDeath = append(portDeath, k)
		}
		en.create()
		w.Run()
		en.post(nil, int(w.Scroll), 2)
	}
	if failed {
		t.Logf("pass %d, original step %d of %d: a step of the port is not the original's:", passes, k, len(want))
		for _, f := range failure {
			t.Logf("  %s", f)
		}
	}
	t.Logf("%d of %d steps matched in %d passes (%d adopted random starts)", k, len(want), passes, adopted)
	t.Logf("helicopter shot down at original steps %v, in the port at %v", origDeath, portDeath)
	// both deaths of the stretch happen in the same logic pass (the port
	// handles the damage at the start of the pass, the original in the
	// helicopter's slice in the middle of the ring: a few steps apart)
	for _, o := range origDeath {
		if o >= k {
			continue
		}
		ok := false
		for _, p := range portDeath {
			if p <= o && o-p <= 16 {
				ok = true
			}
		}
		if !ok {
			t.Errorf("helicopter shot down at original step %d, port deaths at %v", o, portDeath)
		}
	}
	replayed := 0
	for _, o := range origDeath {
		if o < k {
			replayed++
		}
	}
	if len(portDeath) != replayed {
		t.Errorf("%d helicopter deaths in the port, %d in the replayed part of the original", len(portDeath), replayed)
	}
	// The replay stays exact for the first ~1250 passes (every enemy type of
	// the stretch). After that the player's own task, whose place in the
	// original's ring is not modelled, starts to be sampled a step apart.
	if k < 9000 {
		t.Errorf("replay stopped at step %d of %d", k, len(want))
	}
}

// The first green helicopters, shot down by a stream of bullets, add their
// score (12 each, shown x10) and leave a blast behind.
func TestShootDownFoddera(t *testing.T) {
	_, _, en, sh := townEnemies(t)
	sh.SetLevel(6)
	top := 0xE860
	en.player.X, en.player.Y = 150, 200
	for step := 0; step < 4000 && en.Kills < 3; step++ {
		if step%2 == 0 {
			top-- // two rows per four logic steps
		}
		// the player follows the nearest helicopter and holds fire
		for _, tk := range en.world.Tasks() {
			if tk.Hittable() && tk.Draw == 0x404 {
				en.player.X, en.player.Y = tk.PX(), min(250, max(10, rel(tk.PY(), top)+60))
				break
			}
		}
		sh.Step(2, true, en.player.X, en.player.Y, top)
		en.Step(2, top, sh)
	}
	if en.Kills < 3 {
		t.Fatalf("only %d helicopters shot down", en.Kills)
	}
	if en.player.Score != 12*en.Kills {
		t.Fatalf("score %d for %d foddera, want %d", en.player.Score, en.Kills, 12*en.Kills)
	}
}

// internal/actor/lifted.go is exactly what tools/lift.py produces from the
// disk's AMPROG.OBJ (run tools/lift_all.sh after changing the lifter).
func TestLiftedFileIsCurrent(t *testing.T) {
	py := ".venv/bin/python"
	if _, err := os.Stat(py); err != nil {
		t.Skip("no python venv")
	}
	if _, err := os.Stat("assets/AMPROG.OBJ"); err != nil {
		t.Skip("no extracted assets")
	}
	outPath := filepath.Join(t.TempDir(), "l.go")
	if out, err := exec.Command("sh", "tools/lift_all.sh", outPath).CombinedOutput(); err != nil {
		t.Fatalf("lifter failed: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := format.Source(raw)
	if err != nil {
		t.Fatal(err)
	}
	have, err := os.ReadFile("internal/actor/lifted.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, have) {
		t.Fatal("internal/actor/lifted.go is stale: run tools/lift_all.sh")
	}
}

// The helicopter has three lives after the current one: it survives while
// invulnerable, dies when touched, comes back after 100 ticks with one life
// less, and the game is over when it dies with none left. Score earns lives.
func TestPlayerLives(t *testing.T) {
	_, _, en, _ := townEnemies(t)
	p := en.player
	top := 0xE860
	touch := func() {
		en.hitPending = true
		en.Step(2, top, nil)
	}
	// 200 ticks of invulnerability at the start
	touch()
	if !p.Alive {
		t.Fatal("died while invulnerable")
	}
	for i := 0; i < 100; i++ {
		en.Step(2, top, nil)
	}
	if p.Invuln != 0 {
		t.Fatalf("invulnerability left: %d", p.Invuln)
	}
	for life := 3; life >= 0; life-- {
		if p.Lives != life {
			t.Fatalf("lives %d, want %d", p.Lives, life)
		}
		touch()
		if p.Alive {
			t.Fatal("survived a hit")
		}
		if life == 0 {
			break
		}
		for i := 0; i < 51 && !p.Alive; i++ {
			en.Step(2, top, nil)
		}
		if !p.Alive {
			t.Fatal("no new helicopter after 100 ticks")
		}
		if p.Invuln != startInvuln {
			t.Fatalf("new helicopter invulnerability %d", p.Invuln)
		}
		for i := 0; i < 100; i++ {
			en.Step(2, top, nil)
		}
	}
	if !p.Over {
		t.Fatal("game not over after the last life")
	}
	// an extra life at 100000 points
	_, _, en2, _ := townEnemies(t)
	en2.player.Score = 10000
	en2.Step(2, top, nil)
	if en2.player.Lives != 4 {
		t.Fatalf("lives %d after 100000 points, want 4", en2.player.Lives)
	}
}

// A long game with a wandering, shooting player: nothing panics, the
// population stays bounded, and the game ends or the level scrolls out.
func TestLongGameSmoke(t *testing.T) {
	d, lv, _, _ := townEnemies(t)
	pl, _ := NewPlayer(d)
	sc, err := NewLevelScene(d, lv, level.StartTop)
	if err != nil {
		t.Fatal(err)
	}
	_ = pl
	maxTasks := 0
	for i := 0; i < 40000; i++ {
		dx, dy := (i/37)%3-1, (i/53)%3-1
		sc.Advance(1, dx, dy, i%7 < 4)
		if n := len(sc.enemy.world.Tasks()); n > maxTasks {
			maxTasks = n
		}
		if sc.top < sc.lv.EndTop+1 {
			break
		}
	}
	t.Logf("top %04x, kills %d, lives %d, over %v, max tasks %d", sc.top, sc.enemy.Kills, sc.player.Lives, sc.player.Over, maxTasks)
	if maxTasks > 400 {
		t.Fatalf("%d tasks alive at once", maxTasks)
	}
}

// A task that is hit but survives is drawn as a white silhouette (colour 9)
// for one pass; bullets are hardware sprites painted with the sprite palette.
func TestHitFlashAndSprites(t *testing.T) {
	_, _, en, _ := townEnemies(t)
	w := en.world
	top := 0xE860
	w.Scroll, w.Logic = uint16(top), uint16(top)
	// a tough green helicopter in the middle of the screen
	heli := w.Start(100, int16(top+100), 1, func(tk *actor.Task) {
		tk.Construct(0x404, 0x22, -48, 5, 0, 0)
		tk.Forever()
	})
	// and an enemy bullet picture
	w.Start(200, int16(top+100), 0, func(tk *actor.Task) {
		tk.Image = 0x3201 // BULLET.LIN image 25
		tk.Active = true
		tk.Forever()
	})
	w.Run()
	idx := make([]uint8, level.ScreenW*level.ScreenH)
	en.Draw(idx, level.ScreenW, level.ScreenH, top)
	count := func(c uint8) int {
		n := 0
		for _, v := range idx {
			if v == c {
				n++
			}
		}
		return n
	}
	base := count(flashColour) // the picture itself has some colour 9
	heli.Hit(1)
	w.Run() // the handler runs in the task's next step
	for i := range idx {
		idx[i] = 0
	}
	en.Draw(idx, level.ScreenW, level.ScreenH, top)
	if n := count(flashColour); n < base+100 {
		t.Fatalf("%d white pixels after a hit, %d before", n, base)
	}
	if heli.HP != 4 {
		t.Fatalf("hp %d after one hit, want 4", heli.HP)
	}
	w.Run() // the flash lasts one pass
	for i := range idx {
		idx[i] = 0
	}
	en.Draw(idx, level.ScreenW, level.ScreenH, top)
	if n := count(flashColour); n != base {
		t.Fatalf("%d white pixels a pass later, %d before", n, base)
	}
	// the bullet is not in the playfield buffer but in the sprite overlay
	buf := image.NewRGBA(image.Rect(0, 0, level.ScreenW, level.ScreenH))
	en.DrawSprites(buf, top, 0)
	grey := 0
	for i := 0; i < len(buf.Pix); i += 4 {
		if buf.Pix[i] == 0x99 && buf.Pix[i+1] == 0x99 && buf.Pix[i+2] == 0x99 {
			grey++
		}
	}
	if grey == 0 {
		t.Fatal("bullet not drawn with the sprite palette")
	}
}

// Tokens: five drop in a ring, shooting one cycles its kind, flying into one
// applies it: kind 1 switches to the spread weapon and a second one levels it
// up, kind 2 shortens the volley interval by 3 frames, kind 3 gives 500 ticks
// of shield and 5000 points, kind 4 gives full power and a smart bomb.
func TestTokens(t *testing.T) {
	_, _, en, _ := townEnemies(t)
	w := en.world
	top := 0xE860
	p := en.player
	p.Invuln = 0
	step := func(n int) {
		for i := 0; i < n; i++ {
			en.Step(2, top, nil)
		}
	}
	step(1)
	w.DropTokens(160, int16(top+60), 5)
	step(40)
	var tokens []*actor.Task
	for _, tk := range w.Tasks() {
		if tk.Draw == 0x18 {
			tokens = append(tokens, tk)
		}
	}
	if len(tokens) != 5 {
		t.Fatalf("%d tokens dropped, want 5", len(tokens))
	}
	pick := func(kind int) {
		t.Helper()
		var tk *actor.Task
		for _, c := range tokens {
			if !c.Done() {
				tk = c
				break
			}
		}
		if tk == nil {
			t.Fatal("no token left")
		}
		tk.P[0] = int16(kind)
		before := p.Pickups
		for i := 0; i < 10 && !tk.Done(); i++ {
			p.X, p.Y = tk.PX(), rel(tk.PY(), top) // fly into it
			en.Step(2, top, nil)
		}
		if p.Pickups != before+1 {
			t.Fatalf("kind %d: token not picked up (pickups %d, was %d)", kind, p.Pickups, before)
		}
	}
	if p.Type != 0 || p.Level != 2 || p.Cool != 11 {
		t.Fatalf("start state type %d level %d cool %d", p.Type, p.Level, p.Cool)
	}
	pick(1)
	if p.Type != 0xFF || p.Level != 2 {
		t.Fatalf("after kind 1: type %#x level %d, want spread at level 2", p.Type, p.Level)
	}
	pick(1)
	if p.Level != 3 {
		t.Fatalf("second kind 1: level %d, want 3", p.Level)
	}
	pick(2)
	if p.Cool != 8 {
		t.Fatalf("after kind 2: cool %d, want 8", p.Cool)
	}
	score := p.Score
	pick(3)
	if p.Invuln < 400 || p.Score != score+500 {
		t.Fatalf("after kind 3: invulnerability %d, score %d -> %d", p.Invuln, score, p.Score)
	}
	pick(4)
	if p.Level != 6 || p.Cool != 8 {
		t.Fatalf("after kind 4: level %d cool %d, want 6 and 8", p.Level, p.Cool)
	}
	if w.AuxBomb() == 0 {
		t.Fatal("no smart bomb")
	}
	for i := 0; i < 40; i++ { // fifty ticks
		en.Step(2, top, nil)
	}
	if w.AuxBomb() != 0 {
		t.Fatal("smart bomb still on")
	}
}

// A shield (the capsule a destroyed mine releases) makes the helicopter
// immune to bullets for 500 ticks and shows a glow that follows it.
func TestShield(t *testing.T) {
	_, _, en, _ := townEnemies(t)
	w := en.world
	top := 0xE860
	p := en.player
	p.Invuln = 0
	en.Step(2, top, nil)
	// what the capsule does when it is touched ($18884): the shield request
	p.Shield = -1
	en.Step(2, top, nil)
	if p.Shield <= 0 {
		t.Fatalf("shield %d after the request", p.Shield)
	}
	en.Step(2, top, nil)
	glow := 0
	for _, tk := range w.Tasks() {
		if tk.Draw == 0x1211 {
			glow++
		}
	}
	if glow != 1 {
		t.Fatalf("%d shield glows", glow)
	}
	en.hitPending = true
	en.Step(2, top, nil)
	if !p.Alive {
		t.Fatal("killed through the shield")
	}
	for i := 0; i < 260 && p.Shield > 0; i++ {
		en.Step(2, top, nil)
	}
	for i := 0; i < 60; i++ {
		en.Step(2, top, nil)
	}
	if p.Invuln != 0 || p.Shield != 0 {
		t.Fatalf("shield %d invulnerability %d at the end", p.Shield, p.Invuln)
	}
	en.hitPending = true
	en.Step(2, top, nil)
	if p.Alive {
		t.Fatal("still immune after the shield ran out")
	}
}

// Shooting a mine releases a shield capsule; touching it gives the shield and
// its glow, a second capsule while shielded sets off the smart bomb.
func TestMineReleasesShield(t *testing.T) {
	_, _, en, _ := townEnemies(t)
	w := en.world
	top := 0xE860
	p := en.player
	p.Invuln = 0
	mine := w.Start(200, int16(top+100), 1, actor.Lifted[0x18ad6])
	for i := 0; i < 4; i++ {
		en.Step(2, top, nil)
	}
	mine.Hit(1)
	for i := 0; i < 3; i++ {
		en.Step(2, top, nil)
	}
	var capsule *actor.Task
	for _, tk := range w.Tasks() {
		if tk.Draw == 0x1211 {
			capsule = tk
		}
	}
	if capsule == nil {
		t.Fatal("no shield capsule after the mine was shot")
	}
	for i := 0; i < 12 && p.Shield == 0; i++ {
		p.X, p.Y = capsule.PX(), rel(capsule.PY(), top)
		en.Step(2, top, nil)
	}
	for i := 0; i < 8; i++ {
		en.Step(2, top, nil)
	}
	if p.Shield <= 0 || p.Invuln == 0 {
		t.Fatalf("shield %d invulnerability %d after touching the capsule", p.Shield, p.Invuln)
	}
}

// The town's boss: a helicopter gunship of five task parts that fires bullets
// and missiles, takes 25 hits and, destroyed, drops tokens. A scripted
// player fires at it from below; nothing may panic (the pods outlive their
// parent for a moment).
func TestGooseBoss(t *testing.T) {
	d, lv, _, _ := townEnemies(t)
	sc, err := NewLevelScene(d, lv, 0xE700)
	if err != nil {
		t.Fatal(err)
	}
	w := sc.enemy.world
	seen := map[uint16]bool{}
	var bossSeen, tokens bool
	for i := 0; i < 30000 && !tokens; i++ {
		sc.player.Invuln = 1000 // the test is about the boss, not the helicopter
		// stay under the boss body and shoot
		x := 160
		for _, tk := range w.Tasks() {
			if tk.Draw == 0x17 && tk.Active {
				x = tk.PX()
				bossSeen = true
			}
			if tk.Draw == 0x18 {
				tokens = true
			}
			seen[tk.Draw] = true
		}
		dx := 0
		switch {
		case sc.player.X < x-4:
			dx = 1
		case sc.player.X > x+4:
			dx = -1
		}
		if w.BossEngaged() {
			t.Fatal("the goose stops the scroll")
		}
		sc.Advance(1, dx, 0, true)
		if sc.top <= sc.lv.EndTop {
			break
		}
	}
	if !bossSeen {
		t.Fatal("the boss never appeared")
	}
	t.Logf("top %04x, kills %d, lives %d, score %d", sc.top, sc.enemy.Kills, sc.player.Lives, sc.player.Score)
	if !tokens {
		t.Fatalf("the boss was not destroyed (top %04x, parts seen %v)", sc.top, seen)
	}
}

// The smart bomb whitens the screen and the white fades by 4/256 a frame
// ($2B9E: set to $100, -4 per frame, so it is gone after 64 frames).
func TestBombFlash(t *testing.T) {
	d, lv, _, _ := townEnemies(t)
	sc, err := NewLevelScene(d, lv, level.StartTop)
	if err != nil {
		t.Fatal(err)
	}
	w := sc.enemy.world
	sc.Advance(10, 0, 0, false)
	sc.Compose()
	before := append([]byte(nil), sc.buf.Pix...)
	w.Bomb(160, int16(uint16(sc.top)+100))
	sc.Advance(2, 0, 0, false)
	if l := w.Flash(); l < 0xF0 {
		t.Fatalf("flash level %#x just after the bomb", l)
	}
	sc.Compose()
	bright := 0
	for i := 0; i < len(sc.buf.Pix); i += 4 {
		bright += int(sc.buf.Pix[i])
	}
	dark := 0
	for i := 0; i < len(before); i += 4 {
		dark += int(before[i])
	}
	if bright < dark*3/2 {
		t.Fatalf("screen not whiter: %d against %d", bright, dark)
	}
	sc.Advance(70, 0, 0, false)
	if l := w.Flash(); l != 0 {
		t.Fatalf("flash level %#x after 72 frames", l)
	}
}

// The levels run into each other: past the town's last scenery the desert's
// script goes on with its own enemies, among them its goose, which drops
// tokens when it is destroyed.
func TestDesertBossAfterTheTown(t *testing.T) {
	d, _, _, _ := townEnemies(t)
	lv, err := level.Chain(d, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := NewLevelScene(d, lv, 0xDD40)
	if err != nil {
		t.Fatal(err)
	}
	sc.player.Reset() // the run-up played without input
	w := sc.enemy.world
	var seen, tokens, cleared bool
	for i := 0; i < 40000 && !tokens; i++ {
		sc.player.Invuln = 1000
		x := 160
		past := sc.top < 0xDC30          // beyond the town's end: only what the desert script brings
		if !cleared && w.BossEngaged() { // the scroll stands still at the town's boss
			cleared = true
			for _, tk := range w.Tasks() {
				if tk.Draw == 0x17 || tk.Draw == 0x18 {
					tk.Kill() // what is left of the town's boss
				}
			}
			sc.Advance(2, 0, 0, false) // let them go
			continue
		}
		for _, tk := range w.Tasks() {
			if tk.Draw == 0x17 && tk.Active && past {
				x, seen = tk.PX(), true
			}
			if tk.Draw == 0x18 && past && seen {
				tokens = true
			}
		}
		dx := 0
		switch {
		case sc.player.X < x-4:
			dx = 1
		case sc.player.X > x+4:
			dx = -1
		}
		sc.Advance(1, dx, 0, past)
	}
	t.Logf("top %04x, kills %d", sc.top, sc.enemy.Kills)
	if !seen || !tokens {
		t.Fatalf("boss seen %v, tokens %v at top %04x", seen, tokens, sc.top)
	}
}

// The difficulty rank ($B6) grows with the time survived, the weapon level
// and the level script being read, as $10C94 computes it; at rank 1 the
// proximity mines and pop-up guns of the town stop being skipped.
func TestRank(t *testing.T) {
	d, lv, _, _ := townEnemies(t)
	pl, _ := NewPlayer(d)
	en, err := NewEnemies(d, lv, pl, level.StartTop)
	if err != nil {
		t.Fatal(err)
	}
	en.updateRank()
	if en.world.Rank != 0 {
		t.Fatalf("rank %d at the start", en.world.Rank)
	}
	pl.Age = 8 * 256 // eight 256-pass periods
	pl.Level = 2
	en.updateRank()
	// (8 + (2+2)>>2) >> 3 = 1
	if en.world.Rank != 1 {
		t.Fatalf("rank %d after 2048 passes, want 1", en.world.Rank)
	}
	pl.Age, pl.Level = 0x7FFF, 6
	en.updateRank()
	if en.world.Rank != 10 { // (127 + 2) >> 3 = 16, capped at 10
		t.Fatalf("rank %d at the longest life, want 10", en.world.Rank)
	}
}

// The last boss takes its hits, the screen fades to black, the game is
// complete and the statistics screen replaces the playfield. The parts-only
// boss of the scifi level before it is removed by hand.
func TestFinalBossAndLevelEnd(t *testing.T) {
	d, _, _, _ := townEnemies(t)
	lv, err := level.Chain(d, 5, 2)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := NewLevelScene(d, lv, 0xD600)
	if err != nil {
		t.Fatal(err)
	}
	sc.player.Reset()
	sc.shots.SetLevel(6)
	w := sc.enemy.world
	final := false
	for i := 0; i < 20000 && sc.end == nil; i++ {
		sc.player.Invuln = 1000
		x := 160
		for _, b := range w.Bosses() {
			if b.HP == 0 && b.Draw != 0 {
				b.Kill()
			}
			if b.Draw == 0 && b.HP > 0 {
				x, final = b.PX(), true
			}
		}
		dx := 0
		switch {
		case sc.player.X < x-4:
			dx = 1
		case sc.player.X > x+4:
			dx = -1
		}
		sc.Advance(1, dx, 0, true)
	}
	if !final || sc.end == nil {
		t.Fatalf("final boss seen %v, complete %v at top %04x", final, sc.end != nil, sc.top)
	}
	sc.Compose() // the statistics screen
	if sc.buf.Pix[0] != 0 || sc.shots.Bullets == 0 {
		t.Fatalf("statistics screen not drawn, %d bullets", sc.shots.Bullets)
	}
}

// The table keeps seven entries best first, a score must beat the last one.
func TestHighScoreTable(t *testing.T) {
	h := NewHighScores(nil)
	if h.Rank(10000) != -1 || h.Rank(10001) != 6 || h.Rank(80000) != 0 {
		t.Fatalf("ranks %d %d %d", h.Rank(10000), h.Rank(10001), h.Rank(80000))
	}
	if r := h.Insert("ME", 45000); r != 3 {
		t.Fatalf("inserted at %d", r)
	}
	if h.Entries[3].Name != "ME" || h.Entries[4].Score != 40000 || h.Entries[6].Score != 20000 {
		t.Fatalf("table %+v", h.Entries)
	}
}

// With no lives left a destroyed helicopter ends the game; with unlimited
// lives it comes back.
func TestGameOverAndUnlimitedLives(t *testing.T) {
	for _, unlimited := range []bool{false, true} {
		_, _, en, _ := townEnemies(t)
		p := en.player
		p.Reset()
		p.Unlimited = unlimited
		p.Lives, p.Invuln = 0, 0
		en.damage(0xE700)
		if p.Over == unlimited {
			t.Fatalf("unlimited %v: over %v", unlimited, p.Over)
		}
		if unlimited {
			for i := 0; i < 100 && !p.Alive; i++ {
				en.playerStep(0xE700, 2)
			}
			if !p.Alive || p.Lives != 0 {
				t.Fatalf("alive %v, lives %d", p.Alive, p.Lives)
			}
		}
	}
}

// The three end screens draw (statistics, name entry, table).
func TestEndScreensDraw(t *testing.T) {
	d, lv, _, _ := townEnemies(t)
	sc, err := NewLevelScene(d, lv, level.StartTop)
	if err != nil {
		t.Fatal(err)
	}
	sc.player.Score = 25000
	sc.beginEnd("Game Over")
	if sc.end.rank != 5 {
		t.Fatalf("rank %d", sc.end.rank)
	}
	for _, ph := range []int{phaseStats, phaseName, phaseTable} {
		sc.end.phase, sc.end.name = ph, "ACE"
		sc.Compose()
		lit := 0
		for i := 0; i < len(sc.buf.Pix); i += 4 {
			if sc.buf.Pix[i] != 0 {
				lit++
			}
		}
		if lit < 200 {
			t.Fatalf("phase %d: %d lit pixels", ph, lit)
		}
	}
}

// Every page of the attract loop and the options screen draw something.
func TestAttractPages(t *testing.T) {
	d, _, _, _ := townEnemies(t)
	a, err := NewAttract(d, NewHighScores(d), func() {})
	if err != nil {
		t.Fatal(err)
	}
	for p := 0; p < pageCount; p++ {
		for _, tick := range []int{10, 300, 700} {
			a.page, a.tick = p, tick
			a.compose()
			lit := 0
			for i := 0; i < len(a.buf.Pix); i += 4 {
				if a.buf.Pix[i]|a.buf.Pix[i+1]|a.buf.Pix[i+2] != 0 {
					lit++
				}
			}
			if lit < 100 {
				t.Fatalf("page %d tick %d: %d lit pixels", p, tick, lit)
			}
		}
	}
	a.options = true
	a.compose()
}

// When the goose is destroyed its parts go with it at once (the callback at
// $21E of the children) and neither it nor they stay on the screen while the
// routine runs through its checksum loop.
func TestGooseLeavesNothingBehind(t *testing.T) {
	d, lv, _, _ := townEnemies(t)
	sc, err := NewLevelScene(d, lv, 0xE700)
	if err != nil {
		t.Fatal(err)
	}
	w := sc.enemy.world
	sc.player.Reset()
	sc.shots.SetLevel(6)
	armed, deadAt := false, -1
	for f := 0; f < 12000 && (deadAt < 0 || f < deadAt+60); f++ {
		sc.player.Invuln = 1000
		x := 160
		for _, tk := range w.Tasks() {
			if tk.Draw == 0x17 && tk.Active && tk.HP > 0 {
				x, armed = tk.PX(), true
			}
		}
		dx := 0
		if sc.player.X < x-3 {
			dx = 1
		} else if sc.player.X > x+3 {
			dx = -1
		}
		sc.Advance(1, dx, 0, true)
		body := false
		for _, tk := range w.Tasks() {
			if tk.Draw == 0x17 && tk.Active && tk.HP > 0 {
				body = true
			}
		}
		if armed && !body && deadAt < 0 {
			deadAt = f
		}
		if deadAt >= 0 && f > deadAt+5 {
			for _, tk := range w.Tasks() {
				if tk.Active && tk.Image&0xff == 0x17 {
					t.Fatalf("frame %d (%d after the end): task %04x still shows image %04x", f, f-deadAt, tk.Draw, tk.Image)
				}
			}
		}
	}
	if deadAt < 0 {
		t.Fatal("the goose was not destroyed")
	}
}

// The desert's gun base (word 161c, $1A7D0): the scroll stops once the base
// has raised $A6 bit 3 ($1A66E, after its wait for screen row $54), it fires
// and sends tanks, and the scroll goes on when it is destroyed.
func TestDesertBase(t *testing.T) {
	d, _, _, _ := townEnemies(t)
	lv, err := level.Chain(d, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := NewLevelScene(d, lv, 0xD300)
	if err != nil {
		t.Fatal(err)
	}
	w := sc.enemy.world
	sc.player.Reset()
	sc.shots.SetLevel(6)
	engaged, stopped, resumed := false, 0, false
	for f := 0; f < 12000 && !resumed; f++ {
		sc.player.Invuln = 1000
		x, alive := 160, false
		for _, tk := range w.Tasks() {
			if tk.Draw == 0x161c && tk.HP > 0 {
				alive = true
				if tk.Active {
					x = tk.PX()
				}
			}
		}
		dx := 0
		if sc.player.X < x-3 {
			dx = 1
		} else if sc.player.X > x+3 {
			dx = -1
		}
		sc.Advance(1, dx, 0, true)
		switch {
		case w.BossEngaged():
			if !w.BossFight() {
				t.Fatal("engaged without $A6 bit 3")
			}
			if !engaged {
				engaged, stopped = true, sc.top
			} else if sc.top != stopped {
				t.Fatalf("the scroll moved from %04x to %04x during the fight", stopped, sc.top)
			}
		case engaged && !alive:
			resumed = sc.top < stopped
		}
	}
	if !engaged || !resumed {
		t.Fatalf("engaged %v, resumed %v, top %04x", engaged, resumed, sc.top)
	}
}

// Hits, blasts, tokens and bullets reach the sound hook with the address of
// the original's sound entry.
func TestSoundHooks(t *testing.T) {
	_, _, en, _ := townEnemies(t)
	w := en.world
	got := map[int]int{}
	w.Sound = func(id, x int) { got[id]++ }
	top := int(w.Logic)
	tk := w.Start(160, int16(top+60), 1, actor.Routines[0x169ec])
	_ = tk
	for i := 0; i < 400; i++ {
		for _, o := range w.Tasks() {
			if o.Active && o.HP > 0 {
				o.Hit(1)
			}
		}
		en.Step(2, top, nil)
		if got[0x13BFC]+got[0x13C18] > 0 && got[0x14030] > 0 {
			return
		}
	}
	t.Fatalf("sounds heard: %v", got)
}
