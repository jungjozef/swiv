package main

import (
	"github.com/jungjozef/swiv/internal/actor"
	"github.com/jungjozef/swiv/internal/adf"
	"github.com/jungjozef/swiv/internal/lin"
)

// The player's helicopter, from images 0-4 of JEEPHELI.LIN, as the original
// draws it (observed on the running game):
//
//   - the craft is image 0 (fuselage) on one logic step and one of the four
//     rotor-blur images on the next, so the blades appear to spin; logic runs
//     every second display tick (25 Hz);
//   - the rotor images go 3, 4, 1, 2;
//   - a shadow, the same image drawn in "erase" mode (opaque pixels become
//     colour 0), sits 16 pixels right and 32 rows below the craft.
const (
	playerStartX = 288
	playerStartY = 192
	shadowDX     = 16
	shadowDY     = 32
	heliSpeed    = 6 // pixels per logic step (every second tick), measured
)

var rotorImages = [4]int{3, 4, 1, 2}

// Player is the helicopter and its game state ($2BA8 in the original).
type Player struct {
	// PlayerRec holds the fields the enemy and token code shares with the
	// player record: score, weapon level and cooldown, weapon type, tokens
	// collected, shield and invulnerability.
	*actor.PlayerRec

	bank *lin.Bank
	X, Y int // hotspot, in playfield window coordinates

	Alive bool // $36: the vehicle exists
	Lives int  // lives left after the current one (HUD "Heli 3")
	Over  bool // no lives left
	// Unlimited keeps the lives from running out (a command line option).
	Unlimited bool
	nextLife  int // $54: score of the next extra life
	respawn   int // ticks until the next vehicle
	Age       int // $6E: logic passes survived since the last death (saturates at $7FFF)
}

const (
	startLives   = 3
	startLevel   = 2     // weapon level of a new game ($64)
	startInvuln  = 200   // $C8 at $1808E, in ticks
	respawnDelay = 100   // $64 at $16110
	firstBonus   = 10000 // $2710 at $1604C
	nextBonus    = 30000 // $7530 at $160D6
)

// Reset starts a game: three lives and a fresh helicopter at the start
// position (the grid search at $18006 ends there on the runway).
func (p *Player) Reset() {
	p.X, p.Y = playerStartX, playerStartY
	p.Alive, p.Lives, p.Over = true, startLives, false
	*p.PlayerRec = actor.PlayerRec{Active: true, Cool: 12, Level: startLevel, Invuln: startInvuln}
	p.nextLife, p.respawn = firstBonus, 0
	p.newLife(false)
}

// weaponCaps is the table at $16080: what the number of tokens collected
// (in fives) allows a new helicopter to start with: weapon level and frames
// between volleys.
var weaponCaps = [4][2]int{{2, 11}, {3, 10}, {4, 10}, {5, 8}}

// newLife applies $16096: a new helicopter keeps its weapon level up to the
// cap its owner's token count allows and takes that cap's fire rate.
func (p *Player) newLife(keep bool) {
	c := weaponCaps[min(p.Pickups/5, 3)]
	if c[0] < p.Level {
		p.Level = c[0]
	}
	p.Cool = c[1]
}

// NewPlayer loads the helicopter graphics from the disk.
func NewPlayer(d *adf.Disk) (*Player, error) {
	raw, err := d.ReadName("JEEPHELI.LIN")
	if err != nil {
		return nil, err
	}
	b, err := lin.Parse(raw)
	if err != nil {
		return nil, err
	}
	p := &Player{bank: b, PlayerRec: &actor.PlayerRec{}}
	p.Reset()
	return p, nil
}

// Move advances the helicopter by one logic step in direction (dx, dy), each
// -1, 0 or 1. Speed is constant (no acceleration) and the craft is limited to
// x 4..316 and y 4..252, as measured on the original (the joystick held left and right).
func (p *Player) Move(dx, dy int) {
	p.X = min(max(p.X+dx*heliSpeed, 4), 316)
	p.Y = min(max(p.Y+dy*heliSpeed, 4), 252)
}

// image returns the image shown at display tick t.
func (p *Player) image(t int) int {
	step := t / 2
	if step%2 == 0 {
		return 0
	}
	return rotorImages[(step/2)%4]
}

// Draw paints the shadow and the craft into the playfield index buffer.
func (p *Player) Draw(idx []uint8, w, h, tick int) {
	if !p.Alive {
		return
	}
	img := p.image(tick)
	p.bank.BlitIndexed(idx, w, h, img, p.X+shadowDX, p.Y+shadowDY, true)
	if p.Invuln > 0 && p.Invuln&8 != 0 {
		// the craft flashes white while it is immune: the original raises its
		// hit-flash flag whenever bit 3 of the invulnerability counter is set
		p.bank.BlitSolid(idx, w, h, img, p.X, p.Y, flashColour)
		return
	}
	p.bank.BlitIndexed(idx, w, h, img, p.X, p.Y, false)
}
