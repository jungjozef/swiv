package main

import (
	"image"

	"github.com/jungjozef/swiv/internal/actor"
	"github.com/jungjozef/swiv/internal/adf"
	"github.com/jungjozef/swiv/internal/enemy"
	"github.com/jungjozef/swiv/internal/hud"
	"github.com/jungjozef/swiv/internal/level"
	"github.com/jungjozef/swiv/internal/lin"
)

// Enemies runs the level's enemies. Each enemy is a task of the original's
// own task routine, ported in internal/actor from the disassembly: this file
// only creates the tasks as the scroll reaches them (the script loader at
// $126BE), hands them the player's position, applies the player's bullets and
// draws what the tasks say.
type Enemies struct {
	world  *actor.World
	prog   []byte
	names  []string
	banks  map[string]*lin.Bank
	disk   *adf.Disk
	defs   enemy.Defs
	spawns []level.Spawn
	ends   []int // scroll values at which the chained scripts end
	next   int   // next script record to create
	player *Player
	decals []decal

	// Silent keeps the sounds of a run-up (the time played up to a start position) away.
	Silent     bool
	hitPending bool // the helicopter was touched; its damage handler runs next

	Kills int

	// proxy stands for the helicopter's task: the shield child follows it.
	proxy *actor.Task
}

// NewEnemies builds the enemy list for lv. startTop is the scroll value the
// level begins at (the script is read up to 256 rows ahead of it).
func NewEnemies(d *adf.Disk, lv *level.Level, pl *Player, startTop int) (*Enemies, error) {
	prog, err := d.ReadName("AMPROG.OBJ")
	if err != nil {
		return nil, err
	}
	defs, err := enemy.Load(prog)
	if err != nil {
		return nil, err
	}
	actor.UseLifted() // the routines lifted from the disassembly (tools/lift.py)
	e := &Enemies{
		world:  actor.New(prog),
		prog:   prog,
		names:  bankNames(prog),
		banks:  map[string]*lin.Bank{},
		disk:   d,
		defs:   defs,
		spawns: lv.Spawns,
		ends:   lv.Ends,
		player: pl,
	}
	// the original seeds its generator from the raster position; any seed
	// gives a valid game
	e.world.Seed = 0x2F6E2B1
	e.world.Scroll, e.world.Logic = uint16(startTop), uint16(startTop)
	e.world.Target = e.target
	e.hook()
	e.create()
	return e, nil
}

// rel is a world y relative to the scroll value of the top row, with the
// 16-bit wrap of the original's scroll coordinates.
func rel(y, top int) int { return int(int16(uint16(y) - uint16(top))) }

// hook connects the world's callbacks.
func (e *Enemies) hook() {
	e.world.Extents = e.extents
	e.world.Sound = func(id, x int) {
		if !e.Silent {
			playSound(id, x)
		}
	}
	e.world.P1 = e.player.PlayerRec
	e.world.Notify = func(ev actor.Event) {
		if ev.Kind == actor.Killed {
			e.player.Score += int(ev.Task.Pts) // the score is kept /10, shown x10
			e.Kills++
		}
	}
	e.world.Decal = func(word uint16, x, y int) {
		e.decals = append(e.decals, decal{word, x, y})
	}
}

// extents are the half sizes of the collision box of a picture ($1F4/$1F6):
// the cx, cy of its first strip.
func (e *Enemies) extents(word uint16) (int, int) {
	b := e.bank(word)
	if b == nil {
		return 8, 8
	}
	im := int(word>>8) / 2
	if im >= len(b.Images) || len(b.Images[im]) == 0 {
		return 8, 8
	}
	f := b.Frames[b.Images[im][0]]
	return int(f.CX), int(f.CY)
}

// playerClass is what the helicopter's collision object raises in what it
// touches: bits 3 (hurts air enemies) and 6 (player 1), $0048 given to $15D8E
// at $183F0. It listens to event bit 1 (air enemies and their bullets) with
// the damage handler $182C6 ($1550C at $18408).
const playerClass = 0x0048

// collide runs the collision pass after the tasks have run: the player's
// bullets are passive objects (class $8041), the helicopter an active one.
// Bullets that touched something are gone; a touch with class bit 1 (enemy
// bullets, air enemies) is the helicopter's damage event, handled in the
// player's next slice.
func (e *Enemies) collide(shots *Shots, top int) {
	var objs []*actor.Obj
	var owner []int
	if shots != nil {
		for i := range shots.pool {
			b := &shots.pool[i]
			if !b.active {
				continue
			}
			objs = append(objs, &actor.Obj{X: int16(b.x), Y: int16(b.y), C: 8, E: 8, Class: 0x8041})
			owner = append(owner, i)
		}
	}
	nb := len(objs)
	var po *actor.Obj
	if e.player.Alive {
		po = &actor.Obj{X: int16(e.player.X), Y: int16(uint16(e.player.Y) + uint16(top)), C: 10, E: 19, Class: playerClass}
		objs = append(objs, po)
	}
	e.world.Collide(objs...)
	for i := 0; i < nb; i++ {
		if objs[i].Event&^0x8000 != 0 {
			shots.pool[owner[i]].active = false
		}
	}
	if po != nil && po.Event&2 != 0 {
		e.hitPending = true
	}
}

// damage is the helicopter's event handler ($182C6): unless it is invulnerable
// it is destroyed, leaving the sixteen pieces of $178BC.
func (e *Enemies) damage(top int) {
	p := e.player
	if !p.Alive || p.Invuln > 0 || p.Shield != 0 {
		return
	}
	p.Alive = false
	p.Age = 0 // $16104
	if e.proxy != nil {
		e.proxy.Kill() // the shield child goes with it
		e.proxy = nil
	}
	t := e.world.Start(int16(p.X), int16(uint16(p.Y)+uint16(top)), 0, actor.Lifted[0x178bc])
	t.Z = 0x20 << 16
	t.Angle = 0
	if p.Lives > 0 || p.Unlimited {
		p.respawn = respawnDelay + 2 // the new vehicle task needs a pass to start
	} else {
		p.Over = true
	}
}

// playerStep is the helicopter's task and the control task between lives
// ($18260, $16050): the shield and invulnerability counters, the death wait,
// the next life, bonus lives.
func (e *Enemies) playerStep(top, ticks int) {
	p := e.player
	if p.Alive {
		p.Age = min(p.Age+1, 0x7FFF) // $160BA
		// $18260: a shield just granted (negative) gets its glow; while it
		// lasts the invulnerability is kept topped up
		switch {
		case p.Shield < 0:
			e.shieldGlow()
			p.Shield = shieldTicks
		case p.Shield > 0:
			p.Invuln = shieldHold
			p.Shield = max(0, p.Shield-ticks)
		}
		if p.Invuln > 0 {
			p.Invuln = max(0, p.Invuln-ticks)
		}
		e.syncProxy(top)
	} else if !p.Over {
		p.respawn -= ticks
		if p.respawn <= 0 {
			if !p.Unlimited {
				p.Lives--
			}
			p.Alive, p.Invuln, p.Shield = true, startInvuln, 0
			p.X, p.Y = playerStartX, playerStartY
			p.newLife(true)
		}
	}
	if p.Alive && p.Score >= p.nextLife { // $160CC
		p.Lives++
		e.world.Sound(0x145C0, p.X) // $160E8: the extra life jingle
		p.nextLife += nextBonus
		p.Age = min(p.Age+0x1770, 0x7FFF) // $160E2: an extra life counts as time survived
	}
}

const (
	shieldTicks = 500 // $1F4 at $1828C
	shieldHold  = 100 // $64 at $1826C: the invulnerability kept while shielded
)

// syncProxy keeps the helicopter's stand-in task at the helicopter.
func (e *Enemies) syncProxy(top int) {
	if e.proxy == nil || e.proxy.Done() {
		e.proxy = e.world.Start(0, 0, 0, func(t *actor.Task) {
			t.NoDespawn()
			t.Forever()
		})
	}
	e.proxy.X = int32(e.player.X) << 16
	e.proxy.Y = int32(int16(uint16(e.player.Y)+uint16(top))) << 16
	e.proxy.Z = 0x20 << 16
}

// shieldGlow starts the shield's picture ($188B2), a child of the helicopter.
func (e *Enemies) shieldGlow() {
	if e.proxy != nil {
		e.proxy.Child(actor.Lifted[0x188b2])
	}
}

// decal is a wreck stamped into the ground.
type decal struct {
	word uint16
	x, y int // world position
}

// target is the position enemies aim at: the player's, in world coordinates.
func (e *Enemies) target() (int16, int16, bool) {
	return int16(e.player.X), int16(e.player.Y) + int16(e.world.Scroll), e.player.Alive
}

func bankNames(prog []byte) []string {
	var names []string
	for p := 0xEFC4 - 0xEFC0; p < len(prog); {
		e := p
		for e < len(prog) && prog[e] != 0 {
			e++
		}
		s := prog[p:e]
		if len(s) < 3 || len(s) > 20 {
			break
		}
		names = append(names, string(s))
		p = e + 1
	}
	return names
}

func loadBank(d *adf.Disk, name string) (*lin.Bank, error) {
	raw, err := d.ReadName(name)
	if err != nil {
		return nil, err
	}
	return lin.Parse(raw)
}

// bank returns the picture bank of an image word.
func (e *Enemies) bank(word uint16) *lin.Bank {
	i := int(word & 0xFF)
	if i >= len(e.names) {
		return nil
	}
	name := e.names[i]
	if b, ok := e.banks[name]; ok {
		return b
	}
	b, err := loadBank(e.disk, name)
	if err != nil {
		b = nil
	}
	e.banks[name] = b
	return b
}

// Reset restarts the enemies for a level beginning at scroll value top.
func (e *Enemies) Reset(startTop int) {
	e.world.Close()
	e.world = actor.New(e.prog)
	e.world.Seed = 0x2F6E2B1
	e.world.Scroll, e.world.Logic = uint16(startTop), uint16(startTop)
	e.world.Target = e.target
	e.hook()
	e.next, e.Kills = 0, 0
	e.proxy = nil
	e.decals = nil
	e.create()
}

// create is the script loader ($12612/$126BE): records whose row is within 256
// rows above the top of the screen get a task, unless they are below the top
// already (those belong to the screen the level starts on and are skipped).
func (e *Enemies) create() {
	top := e.world.Logic
	for e.next < len(e.spawns) {
		sp := e.spawns[e.next]
		// a record is read once the one before it is within 256 rows
		if top-0x100 > uint16(sp.Prev) {
			return
		}
		e.next++
		if int16(uint16(sp.Scroll)-top) > 0 {
			continue
		}
		e.start(sp)
	}
}

// start creates the task for a script record ($12704-$1273C).
func (e *Enemies) start(sp level.Spawn) {
	entry, ok := e.world.Prog.Entry(sp.Word)
	var body actor.Routine
	if ok {
		body = actor.Routines[entry]
	}
	if body == nil {
		body = e.placeholder(sp)
	}
	e.world.Start(int16(sp.X), int16(sp.Scroll), sp.Kind, body)
}

// placeholder stands in for the enemy types whose routine is not ported yet:
// the object sits where the script puts it.
func (e *Enemies) placeholder(sp level.Spawn) actor.Routine {
	def := e.defs[sp.Word]
	if def == nil {
		return func(t *actor.Task) {}
	}
	return func(t *actor.Task) {
		t.Construct(def.Draw, def.Flags, def.Damage, max(def.HP, 1), def.Score, def.Weight)
		t.Attr |= actor.AttrGround
		t.Forever()
	}
}

// Step advances the enemies by one logic step. top is the scroll value of the
// screen's top row.
func (e *Enemies) Step(ticks, top int, shots *Shots) {
	w := e.world
	w.Scroll, w.Logic = uint16(top), uint16(top)
	w.Tick += uint16(ticks)
	w.Dt = ticks
	e.pre(top)
	e.create()
	w.Run()
	e.post(shots, top, ticks)
}

// pre is what happens before the tasks run in a pass: the helicopter's
// damage handler, if it was touched in the last pass.
func (e *Enemies) pre(top int) {
	e.updateRank()
	if e.hitPending {
		e.hitPending = false
		e.damage(top)
	}
}

// post is what happens after: collisions, the player's counters.
func (e *Enemies) post(shots *Shots, top, ticks int) {
	e.collide(shots, top)
	e.playerStep(top, ticks)
}

// flashColour is the colour index the hit flash fills silhouettes with.
const flashColour = 9

// Draw paints the enemies (shadows, ground objects, flyers) into the index
// buffer. Hardware-sprite pictures (the bullets) are left to DrawSprites; a
// task hit in the last pass is drawn as a white silhouette.
func (e *Enemies) Draw(idx []uint8, w, h, top int) {
	type item struct {
		t    *actor.Task
		b    *lin.Bank
		img  int
		x, y int
	}
	var items []item
	for _, t := range e.world.Tasks() {
		if !t.Active || t.Image == 0 && t.Draw == 0 || t.Attr&actor.AttrHidden != 0 {
			continue
		}
		b := e.bank(t.Image)
		if b == nil {
			continue
		}
		img := int(t.Image>>8) / 2
		if img >= len(b.Images) || b.Hardware(img) {
			continue
		}
		items = append(items, item{t, b, img, t.PX(), rel(t.PY(), top)})
	}
	// wrecks lie on the ground under everything
	for _, d := range e.decals {
		if b := e.bank(d.word); b != nil {
			if im := int(d.word>>8) / 2; im < len(b.Images) {
				b.BlitIndexed(idx, w, h, im, d.x, rel(d.y, top), false)
			}
		}
	}
	// shadows first ($1D0 objects: the same picture, drawn in erase mode at
	// (x + z/2, y + z))
	for _, it := range items {
		t := it.t
		z := t.PZ()
		if z == 0 || t.Flags&actor.FlagNoShadow != 0 {
			continue
		}
		it.b.BlitIndexed(idx, w, h, it.img, it.x+z/2, it.y+z, true)
	}
	for pass := 0; pass < 2; pass++ {
		for _, it := range items {
			ground := it.t.Attr&actor.AttrGround != 0
			if ground != (pass == 0) {
				continue
			}
			if it.t.Flags&actor.FlagFlash != 0 {
				it.b.BlitSolid(idx, w, h, it.img, it.x, it.y, flashColour)
			} else {
				it.b.BlitIndexed(idx, w, h, it.img, it.x, it.y, false)
			}
			// the overlay channel ($1BA object) is drawn with attribute bit 5
			// set: the blur pictures are checkerboard masks that darken the
			// ground under the rotor
			if t := it.t; t.Image2 != 0 && t.Attr2&actor.AttrHidden == 0 {
				if b := e.bank(t.Image2); b != nil {
					if im := int(t.Image2>>8) / 2; im < len(b.Images) {
						b.BlitIndexed(idx, w, h, im, it.x, it.y, true)
					}
				}
			}
		}
	}
}

// DrawSprites paints the enemies drawn as hardware sprites (bullets: BULLET.LIN
// pictures with frame flag 8) over the finished playfield with the sprite
// palette (white, grey and an animated colour), like Shots.DrawSprites does for
// the player's bullets. Channels go round in drawing order.
func (e *Enemies) DrawSprites(dst *image.RGBA, top, ctr int) {
	k := 0
	for _, t := range e.world.Tasks() {
		if !t.Active || t.Attr&actor.AttrHidden != 0 {
			continue
		}
		b := e.bank(t.Image)
		if b == nil {
			continue
		}
		img := int(t.Image>>8) / 2
		if img >= len(b.Images) || !b.Hardware(img) {
			continue
		}
		g := ((-k) & 7) / 2
		k++
		b.Each(img, t.PX(), rel(t.PY(), top), func(px, py int, c uint8) {
			if px < 0 || px >= 320 || py < 0 || py >= 256 || c == 0 {
				return
			}
			col := hud.SpriteColour(int(c)&3, g, ctr)
			o := py*dst.Stride + 4*px
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = col.R, col.G, col.B, 0xFF
		})
	}
}

// Alive counts the active enemies.
func (e *Enemies) Alive() int {
	c := 0
	for _, t := range e.world.Tasks() {
		if t.Active && t.HP > 0 {
			c++
		}
	}
	return c
}

// updateRank is $10C94, run every pass by the main loop: the difficulty rank
// grows with the time the helicopter has survived (one point per 256 passes
// of $6E, shared with player 2's idle record), its weapon level and the
// number of the level script being read ($B8, one less, from the second
// level on). It is what lets proximity mines and pop-up guns into the town.
func (e *Enemies) updateRank() {
	p := e.player
	d := (p.Age >> 8) + (p.Level+2)>>2
	r := d >> 3
	e.world.Rank = min(r+max(e.levelIndex()-1, 0), 10)
}

// levelIndex is $B8: the index of the script the loader is on.
func (e *Enemies) levelIndex() int {
	n := 0
	for _, end := range e.ends {
		// the loader goes on to the next script once the last record of this
		// one has come within 256 rows of the top
		if int(e.world.Logic)-0x100 > end {
			break
		}
		n++
	}
	return n
}
