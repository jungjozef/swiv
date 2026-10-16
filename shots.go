package main

import (
	"image"

	"github.com/jungjozef/swiv/internal/actor"
	"github.com/jungjozef/swiv/internal/adf"
	"github.com/jungjozef/swiv/internal/hud"
	"github.com/jungjozef/swiv/internal/lin"
	"github.com/jungjozef/swiv/internal/weapon"
)

// The player's bullets, modelled on the original (measured on the running
// game):
//
//   - a pool of 31 bullets shared by everything that shoots;
//   - holding fire launches a volley whenever the cooldown (12 ticks) has run
//     out; a volley is N bullets at the offsets of weapon level N;
//   - bullets are placed in scroll (world) coordinates, spawn at the craft's
//     position plus the volley offset, then move by their velocity once per
//     tick (the helicopter's is 9 pixels per tick upwards);
//   - a projectile is freed when it leaves the screen.
const (
	poolSize     = 31
	fireCooldown = 12 // ticks between volleys ($62 in the player record)
	defaultLevel = 2  // weapon level a new helicopter starts with
	maxLevel     = 6
	bulletHeight = 16 // used only to cull off-screen bullets
)

type projectile struct {
	active bool
	x, y   int // y is a scroll value, like level objects
	vx, vy int
	image  int
}

// Shots owns the projectile pool and the player's weapon state.
type Shots struct {
	bank     *lin.Bank
	table    *weapon.Table
	pool     [poolSize]projectile
	Level    int
	cooldown int
	rec      *actor.PlayerRec // the player's weapon state, when there is one
	Bullets  int              // bullets launched ($30C6)
	Fired    int              // volleys launched, for the HUD/statistics later
}

// NewShots loads BULLET.LIN and the weapon patterns.
func NewShots(d *adf.Disk) (*Shots, error) {
	raw, err := d.ReadName("BULLET.LIN")
	if err != nil {
		return nil, err
	}
	b, err := lin.Parse(raw)
	if err != nil {
		return nil, err
	}
	prog, err := d.ReadName("AMPROG.OBJ")
	if err != nil {
		return nil, err
	}
	t, err := weapon.Load(prog)
	if err != nil {
		return nil, err
	}
	return &Shots{bank: b, table: t, Level: defaultLevel}, nil
}

// SetLevel changes the weapon level (clamped to 1..maxLevel).
func (s *Shots) SetLevel(l int) {
	s.Level = min(max(l, 1), maxLevel)
	if s.rec != nil {
		s.rec.Level = s.Level
	}
}

// Step advances the weapon by `ticks` display ticks. (px, py) is the craft's
// hotspot in window coordinates, top the scroll value of the window's top row.
func (s *Shots) Step(ticks int, fire bool, px, py, top int) {
	period := fireCooldown
	if s.rec != nil {
		// the weapon state lives in the player record: tokens change it
		s.Level = min(max(s.rec.Level, 1), maxLevel)
		period = max(s.rec.Cool, 2)
	}
	if s.cooldown > 0 {
		s.cooldown = max(s.cooldown-ticks, 0)
	}
	if fire && s.cooldown == 0 {
		s.launch(px, py, top)
		s.cooldown = period
	}
	for i := range s.pool {
		b := &s.pool[i]
		if !b.active {
			continue
		}
		b.x += b.vx * ticks
		b.y += b.vy * ticks
		sy := b.y - top
		if sy < 0 || sy >= 256+bulletHeight || b.x < 0 || b.x >= 320 {
			b.active = false
		}
	}
}

func (s *Shots) launch(px, py, top int) {
	s.Fired++
	playSound(0x13EFE, px) // $17A64: the volley's whoosh, panned by the helicopter's x
	spread := s.rec != nil && s.rec.Type != 0
	for _, sh := range s.table.Volley(weapon.HeadingUp, s.Level, spread) {
		slot := s.free()
		if slot == nil {
			return
		}
		s.Bullets++
		*slot = projectile{
			active: true,
			x:      px + sh.DX,
			y:      top + py + sh.DY,
			vx:     sh.VX,
			vy:     sh.VY,
			image:  sh.Image,
		}
	}
}

func (s *Shots) free() *projectile {
	for i := range s.pool {
		if !s.pool[i].active {
			return &s.pool[i]
		}
	}
	return nil
}

// Active reports how many bullets are in flight.
func (s *Shots) Active() int {
	n := 0
	for i := range s.pool {
		if s.pool[i].active {
			n++
		}
	}
	return n
}

// DrawSprites paints the bullets over the finished playfield image the way
// the original shows them: as hardware sprites, which use the sprite palette
// (white, grey and an animated colour) instead of the playfield's.
//
// Sprite channels are handed out in drawing order as (-k) & 7 (the original's
// counter runs down from 64), and the objects are drawn from the highest pool
// slot to the lowest; channel pair g = channel/2 selects which animated
// colour entry a bullet gets.
func (s *Shots) DrawSprites(dst *image.RGBA, top, ctr int) {
	k := 0
	for i := len(s.pool) - 1; i >= 0; i-- {
		b := &s.pool[i]
		if !b.active {
			continue
		}
		ch := (-k) & 7
		k++
		g := ch / 2
		s.bank.Each(b.image, b.x, b.y-top, func(px, py int, c uint8) {
			if px < 0 || px >= 320 || py < 0 || py >= 256 || c == 0 {
				return
			}
			col := hud.SpriteColour(int(c)&3, g, ctr)
			o := py*dst.Stride + 4*px
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = col.R, col.G, col.B, 0xFF
		})
	}
}

// Absorb removes the bullets whose position lies inside r (screen
// coordinates for a window whose top row has scroll value top) and reports
// how many there were.
func (s *Shots) Absorb(r image.Rectangle, top int) int {
	n := 0
	for i := range s.pool {
		b := &s.pool[i]
		if b.active && image.Pt(b.x, b.y-top).In(r) {
			b.active = false
			n++
		}
	}
	return n
}
