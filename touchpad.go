package main

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// The virtual pad for touch screens: a direction pad bottom left, one fire
// button bottom right and a small pause button top right, drawn over the game
// (positions in the 320x256 game screen). It appears once the screen has been
// touched (the browser build also shows it from the start on touch devices) and
// works next to the keyboard.
const (
	padX, padY, padR     = 50.0, 202.0, 40.0 // centre and radius of the direction pad
	padZone              = 70.0              // a touch starting this near it steers
	fireX, fireY, fireR  = 272.0, 206.0, 30.0
	fireZone             = 46.0
	pauseX, pauseY       = 300.0, 18.0
	pauseR, pauseZone    = 11.0, 20.0
	padDead              = 10.0 // pixels from the centre before a direction counts
	roleNone, roleDir    = 0, 1
	roleFire, rolePause_ = 2, 3
)

// touch is one finger on the screen, in game-screen coordinates.
type touch struct {
	id   int
	x, y float64
}

// touchSource lists the fingers now on the screen. The default asks Ebitengine;
// the browser build replaces it, because Ebitengine converts the browser's
// touch positions as if the canvas filled the page, which it does not in the
// framed layout (touches_wasm.go).
var touchSource = nativeTouches

func nativeTouches() []touch {
	var out []touch
	for _, id := range ebiten.AppendTouchIDs(nil) {
		x, y := ebiten.TouchPosition(id)
		out = append(out, touch{int(id), float64(x), float64(y)})
	}
	return out
}

type touchPad struct {
	enabled bool // draw the pad and react to it
	roles   map[int]int
	dx, dy  int
	fire    bool
	firePr  bool // the fire button went down this frame
	pausePr bool // the pause button went down this frame
}

// pad is the one virtual pad of the game.
var pad = &touchPad{roles: map[int]int{}}

// update reads the touches; call it once per game tick before the scenes.
func (t *touchPad) update() {
	t.firePr, t.pausePr = false, false
	fingers := touchSource()
	for _, f := range fingers {
		if _, known := t.roles[f.id]; known {
			continue
		}
		t.enabled = true
		switch {
		case dist(f.x, f.y, fireX, fireY) <= fireZone:
			t.roles[f.id] = roleFire
			t.firePr = true
		case dist(f.x, f.y, pauseX, pauseY) <= pauseZone:
			t.roles[f.id] = rolePause_
			t.pausePr = true
		case dist(f.x, f.y, padX, padY) <= padZone:
			t.roles[f.id] = roleDir
		default:
			t.roles[f.id] = roleNone
		}
	}
	alive := map[int]bool{}
	t.dx, t.dy, t.fire = 0, 0, false
	for _, f := range fingers {
		alive[f.id] = true
		switch t.roles[f.id] {
		case roleFire:
			t.fire = true
		case roleDir:
			vx, vy := f.x-padX, f.y-padY
			d := math.Hypot(vx, vy)
			if d < padDead {
				break
			}
			// eight directions: an axis counts when it is more than 38% of the way
			if math.Abs(vx) > 0.38*d {
				t.dx = sign(vx)
			}
			if math.Abs(vy) > 0.38*d {
				t.dy = sign(vy)
			}
		}
	}
	for id := range t.roles {
		if !alive[id] {
			delete(t.roles, id)
		}
	}
}

func dist(x, y, cx, cy float64) float64 { return math.Hypot(x-cx, y-cy) }

func sign(v float64) int {
	if v < 0 {
		return -1
	}
	return 1
}

// draw paints the pad over the finished frame.
func (t *touchPad) draw(screen *ebiten.Image) {
	if !t.enabled {
		return
	}
	rim := color.NRGBA{255, 255, 255, 110}
	fill := color.NRGBA{255, 255, 255, 38}
	lit := color.NRGBA{232, 176, 48, 120}
	vector.FillCircle(screen, padX, padY, padR, fill, true)
	vector.StrokeCircle(screen, padX, padY, padR, 2, rim, true)
	arrows := [4]struct {
		dx, dy float32
		on     bool
	}{{0, -1, t.dy < 0}, {0, 1, t.dy > 0}, {-1, 0, t.dx < 0}, {1, 0, t.dx > 0}}
	for _, a := range arrows {
		c := rim
		if a.on {
			c = lit
		}
		cx, cy := float32(padX)+a.dx*26, float32(padY)+a.dy*26
		var p vector.Path
		p.MoveTo(cx+a.dx*8, cy+a.dy*8)
		p.LineTo(cx-a.dy*8-a.dx*5, cy+a.dx*8-a.dy*5)
		p.LineTo(cx+a.dy*8-a.dx*5, cy-a.dx*8-a.dy*5)
		p.Close()
		var cs ebiten.ColorScale
		cs.ScaleWithColor(c)
		vector.FillPath(screen, &p, nil, &vector.DrawPathOptions{AntiAlias: true, ColorScale: cs})
	}
	fc := fill
	if t.fire {
		fc = lit
	}
	vector.FillCircle(screen, fireX, fireY, fireR, fc, true)
	vector.StrokeCircle(screen, fireX, fireY, fireR, 2, rim, true)
	vector.FillCircle(screen, fireX, fireY, 9, rim, true)
	// pause: two bars
	vector.StrokeCircle(screen, pauseX, pauseY, pauseR, 1.5, rim, true)
	vector.FillRect(screen, pauseX-4, pauseY-5, 3, 10, rim, true)
	vector.FillRect(screen, pauseX+1, pauseY-5, 3, 10, rim, true)
}
