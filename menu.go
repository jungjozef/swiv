package main

import (
	"fmt"
	"image"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/jungjozef/swiv/internal/adf"
	"github.com/jungjozef/swiv/internal/font"
	"github.com/jungjozef/swiv/internal/raw"
)

// Control schemes of the options screen (the original selects joystick ports
// and the keyboard with the function keys F1-F3 for 1up and F6-F8 for 2up).
const (
	ctlArrows  = iota // arrow keys, Space / Z / Ctrl fire
	ctlWASD           // W A S D, Left Shift / J / Space fire
	ctlGamepad        // first gamepad: stick or D-pad, A fires
)

var ctlNames = [3]string{"ARROWS + SPACE", "WASD + SHIFT", "GAMEPAD"}

// controls is the scheme selected for 1up and 2up.
var controls = [2]int{ctlArrows, ctlArrows}

// readInput returns the direction and fire state of the 1up controls.
func readInput() (dx, dy int, fire bool) {
	switch controls[0] {
	case ctlWASD:
		dx, dy = axis(ebiten.KeyA, ebiten.KeyD), axis(ebiten.KeyW, ebiten.KeyS)
		fire = ebiten.IsKeyPressed(ebiten.KeyShiftLeft) || ebiten.IsKeyPressed(ebiten.KeyJ) || ebiten.IsKeyPressed(ebiten.KeySpace)
	case ctlGamepad:
		for _, id := range ebiten.AppendGamepadIDs(nil) {
			if !ebiten.IsStandardGamepadLayoutAvailable(id) {
				continue
			}
			x := ebiten.StandardGamepadAxisValue(id, ebiten.StandardGamepadAxisLeftStickHorizontal)
			y := ebiten.StandardGamepadAxisValue(id, ebiten.StandardGamepadAxisLeftStickVertical)
			switch {
			case x < -0.4 || ebiten.IsStandardGamepadButtonPressed(id, ebiten.StandardGamepadButtonLeftLeft):
				dx = -1
			case x > 0.4 || ebiten.IsStandardGamepadButtonPressed(id, ebiten.StandardGamepadButtonLeftRight):
				dx = 1
			}
			switch {
			case y < -0.4 || ebiten.IsStandardGamepadButtonPressed(id, ebiten.StandardGamepadButtonLeftTop):
				dy = -1
			case y > 0.4 || ebiten.IsStandardGamepadButtonPressed(id, ebiten.StandardGamepadButtonLeftBottom):
				dy = 1
			}
			fire = fire || ebiten.IsStandardGamepadButtonPressed(id, ebiten.StandardGamepadButtonRightBottom)
		}
	default:
		dx, dy = axis(ebiten.KeyArrowLeft, ebiten.KeyArrowRight), axis(ebiten.KeyArrowUp, ebiten.KeyArrowDown)
		fire = ebiten.IsKeyPressed(ebiten.KeySpace) || ebiten.IsKeyPressed(ebiten.KeyZ) || ebiten.IsKeyPressed(ebiten.KeyControlLeft)
	}
	return
}

func axis(neg, pos ebiten.Key) int {
	v := 0
	if ebiten.IsKeyPressed(neg) {
		v--
	}
	if ebiten.IsKeyPressed(pos) {
		v++
	}
	return v
}

// firePressed is true on the frame a fire button goes down (any scheme, any
// device: in the original the title reacts to every joystick and key).
func firePressed() bool {
	for _, k := range []ebiten.Key{ebiten.KeySpace, ebiten.KeyEnter, ebiten.KeyZ, ebiten.KeyControlLeft, ebiten.KeyShiftLeft, ebiten.KeyJ} {
		if inpututil.IsKeyJustPressed(k) {
			return true
		}
	}
	for _, id := range ebiten.AppendGamepadIDs(nil) {
		if inpututil.IsStandardGamepadButtonJustPressed(id, ebiten.StandardGamepadButtonRightBottom) {
			return true
		}
	}
	return false
}

// The pages of the attract loop, in the order the original's main loop
// ($FD24) runs them, and how long each stays (50 Hz ticks).
const (
	pageTitle   = iota // COVER.RAW with the title tune
	pageInfo           // the credits line and "PRESS HELP FOR OPTIONS"
	pageHeli           // SWIV-1 blueprints and specification
	pageJeep           // SWIV-2
	pageScores         // TODAY'S BEST SCORES
	pageCredits        // the programmers
	pageCount
)

var pageTicks = [pageCount]int{500, 450, 900, 900, 500, 500}

const (
	infoText = "_x160_y048_c15_a0PUBLISHED BY THE SALES CURVE LTD_n_nON THE STORM LABEL._n_mPROGRAMMED BY RANDOM ACCESS._n_mGAME DESIGNED BY NED LANGMAN,_n_nRONALD PIEKET WEESERIK AND_n_nDAN MARCHANT._n_mMUSIC COMPOSED BY ANDREW BARNABAS._n_mPRESS HELP FOR OPTIONS SCREEN._n_nPRESS FIRE TO START GAME._n"

	creditsText = "_x044_y022_c02_a1Amiga & ST_mProgramming_mRonald Pieket_m_mAmiga & ST_mProgramming_mJohn Croudy_m_mSpectrum & CPC_mProgramming_mKen Murfitt_m_mCommodore 64_mProgramming_mRob Henderson_m_m" +
		"_x206_y022Amiga & ST_mGraphics_mNed Langman_m_mSpectrum & CPC_mGraphics_mTahir Rashid_m_mCommodore 64_mGraphics_mRob Whitaker_m_mProgrammer_mPsychotherapy_mPuntettay_n"

	helpText = "_x084_y024_c15_a01up_f_x234 2up_f_x160_y210Press function keys to select controls_n"
)

// specs of the two vehicles ($105B6-$108C6).
type spec struct {
	picture, wire string // the coloured picture and the blueprint
	blurb         string
	labels, vals  string
}

var specs = [2]spec{
	{"HELIBP1.RAW", "HELIBP2.RAW",
		"Twin overhead fuel injected emission pods.\nDouble-ionised turbo propulsion system.\nRear-ventilated hypersaturation control with undulating\nsynchro-backspring.\n",
		"SWIV-1\n\nWeight\nSpeed\nRange\nCapacity\nWeapon\nComputer\nCost\n",
		"Heli MKIV-A\n\n1,500 stdm\n1.8 l-msec\n0.49 nc\n9500 cc\nPulse cannon\n8K UK101\n3,200 kCred"},
	{"JEEPBP1.RAW", "JEEPBP2.RAW",
		"Carbalite armoured combat chassis.\nY-rigged jump suspension system with OCL MKIII shocks.\nDUO X rough rider safety tyres.\nAuto reverse holosound system fitted as standard.\n",
		"SWIV-2\n\nWeight\nSpeed\nRange\nCapacity\nWeapon\nComputer\nCost\n",
		"Jeep MKIV+\n\n1,700 stdm\n1.4 l-msec\n0.55 nc\n9950 cc\nFusion cannon\nEnhanced MK14\n2,800 kCred"},
}

const vehicleTitle = "Special Weapons Interdiction Vehicle"

// Attract is the title screen and its loop of information pages; fire starts
// the game, HELP opens the options screen.
type Attract struct {
	font    *font.Font
	scores  *HighScores
	onStart func()
	img     map[string]*image.RGBA
	buf     *image.RGBA
	screen  *ebiten.Image

	page, tick int
	options    bool
}

// NewAttract loads the pictures; the caller starts the title tune.
func NewAttract(d *adf.Disk, scores *HighScores, onStart func()) (*Attract, error) {
	prog, err := d.ReadName("AMPROG.OBJ")
	if err != nil {
		return nil, err
	}
	f, err := font.Load(prog)
	if err != nil {
		return nil, err
	}
	a := &Attract{font: f, scores: scores, onStart: onStart, img: map[string]*image.RGBA{},
		buf: image.NewRGBA(image.Rect(0, 0, raw.Width, raw.Height)), screen: ebiten.NewImage(raw.Width, raw.Height)}
	names := []string{"COVER.RAW"}
	for _, s := range specs {
		names = append(names, s.picture, s.wire)
	}
	for _, n := range names {
		b, err := d.ReadName(n)
		if err != nil {
			return nil, err
		}
		im, err := raw.Decode(b, raw.Sequential)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		rgba := image.NewRGBA(im.Bounds())
		for y := 0; y < rgba.Rect.Dy(); y++ {
			for x := 0; x < rgba.Rect.Dx(); x++ {
				rgba.Set(x, y, im.At(x, y))
			}
		}
		a.img[n] = rgba
	}
	return a, nil
}

func (a *Attract) Update() error {
	if a.options {
		a.updateOptions()
		return nil
	}
	a.tick++
	if firePressed() {
		a.onStart()
		return nil
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyH) || inpututil.IsKeyJustPressed(ebiten.KeyF10) {
		a.options = true
		return nil
	}
	if a.tick >= pageTicks[a.page] {
		a.page, a.tick = (a.page+1)%pageCount, 0
	}
	return nil
}

// updateOptions: F1-F3 choose the 1up controls, F6-F8 the 2up ones.
func (a *Attract) updateOptions() {
	for i, k := range []ebiten.Key{ebiten.KeyF1, ebiten.KeyF2, ebiten.KeyF3} {
		if inpututil.IsKeyJustPressed(k) {
			controls[0] = i
		}
	}
	for i, k := range []ebiten.Key{ebiten.KeyF6, ebiten.KeyF7, ebiten.KeyF8} {
		if inpututil.IsKeyJustPressed(k) {
			controls[1] = i
		}
	}
	if firePressed() || inpututil.IsKeyJustPressed(ebiten.KeyEscape) || inpututil.IsKeyJustPressed(ebiten.KeyH) {
		a.options, a.tick = false, 0
	}
}

func (a *Attract) Draw(screen *ebiten.Image) {
	a.compose()
	a.screen.WritePixels(a.buf.Pix)
	screen.DrawImage(a.screen, nil)
}

func (a *Attract) Layout(_, _ int) (int, int) { return ScreenW, ScreenH }

func clearImage(img *image.RGBA) {
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 0, 0, 0, 255
	}
}

func (a *Attract) compose() {
	clearImage(a.buf)
	if a.options {
		a.drawOptions()
		return
	}
	switch a.page {
	case pageTitle:
		copy(a.buf.Pix, a.img["COVER.RAW"].Pix)
	case pageInfo:
		drawMarkup(a.buf, a.font, infoText, -1)
	case pageHeli, pageJeep:
		a.drawSpec(specs[a.page-pageHeli])
	case pageScores:
		drawTable(a.buf, a.font, a.scores, -1, "", 0)
	case pageCredits:
		drawMarkup(a.buf, a.font, creditsText, -1)
	}
}

// drawSpec shows the blueprint with the specification typed out over it,
// then dissolves into the coloured picture.
func (a *Attract) drawSpec(s spec) {
	wire, pic := a.img[s.wire], a.img[s.picture]
	const typeTicks, hold, fade = 450, 50, 100
	copy(a.buf.Pix, wire.Pix)
	chars := a.tick * 2 // two characters a tick
	put := func(text string, x, y int, v uint8) {
		n := min(chars, len(text))
		chars -= n
		drawLines(a.buf, a.font, text[:n], x, y, v)
	}
	put(vehicleTitle, 16, 236, 255)
	put(wrap(a.font, s.blurb, 288), 16, 182, 220)
	put(s.labels, 150, 96, 255)
	put(s.vals, 222, 96, 255)
	if t := a.tick - typeTicks - hold; t > 0 {
		k := min(256, t*256/fade)
		for i := 0; i < len(a.buf.Pix); i += 4 {
			for c := 0; c < 3; c++ {
				a.buf.Pix[i+c] = uint8((int(a.buf.Pix[i+c])*(256-k) + int(pic.Pix[i+c])*k) >> 8)
			}
		}
	}
}

func (a *Attract) drawOptions() {
	drawMarkup(a.buf, a.font, helpText, -1)
	for _, p := range []struct {
		x, y, i int
		key     string
		player  int
	}{
		{20, 55, 0, "F1", 0}, {20, 110, 1, "F2", 0}, {20, 165, 2, "F3", 0},
		{176, 55, 0, "F6", 1}, {176, 110, 1, "F7", 1}, {176, 165, 2, "F8", 1},
	} {
		v := uint8(150)
		if controls[p.player] == p.i {
			v = 255
		}
		label := p.key + " " + ctlNames[p.i]
		drawLines(a.buf, a.font, label, p.x, p.y, v)
	}
}

// drawLines draws text with newlines at x, y (8 pixel lines).
func drawLines(img *image.RGBA, f *font.Font, text string, x, y int, v uint8) {
	for i, l := range strings.Split(text, "\n") {
		f.Draw(l, x, y+8*i, func(px, py int) { setGrey(img, px, py, v) })
	}
}

func setGrey(img *image.RGBA, x, y int, v uint8) {
	if image.Pt(x, y).In(img.Rect) {
		o := img.PixOffset(x, y)
		img.Pix[o], img.Pix[o+1], img.Pix[o+2] = v, v, v
	}
}

// drawMarkup renders the game's inline markup: _xNNN _yNNN position, _cNN
// colour, _aN alignment (0 centred on x, 1 left, 2 right), _n a new line,
// _m a half line, _f a flash marker (ignored). limit (>= 0) shows only that
// many characters.
func drawMarkup(img *image.RGBA, f *font.Font, s string, limit int) {
	x, y, align, grey := 0, 0, 1, uint8(255)
	var line strings.Builder
	flush := func() {
		text := line.String()
		line.Reset()
		if text == "" {
			return
		}
		px := x
		switch align {
		case 0:
			px = x - f.Width(text)/2
		case 2:
			px = x - f.Width(text)
		}
		f.Draw(text, px, y, func(a, b int) { setGrey(img, a, b, grey) })
	}
	for i := 0; i < len(s); {
		if s[i] == '_' && i+1 < len(s) {
			c := s[i+1]
			num := func() int {
				n := 0
				j := i + 2
				for j < len(s) && s[j] >= '0' && s[j] <= '9' {
					n = n*10 + int(s[j]-'0')
					j++
				}
				i = j
				return n
			}
			switch c {
			case 'x':
				flush()
				x = num()
			case 'y':
				flush()
				y = num()
			case 'c':
				flush()
				if n := num(); n == 15 {
					grey = 255
				} else {
					grey = 170
				}
			case 'a':
				flush()
				align = int(s[i+2] - '0') // a single digit: "_a01up" is _a0 and "1up"
				i += 3
			case 'n':
				flush()
				y += 10
				i += 2
			case 'm':
				flush()
				y += 8
				i += 2
			case 'f':
				i += 2
			default:
				line.WriteByte(s[i])
				i++
			}
			continue
		}
		line.WriteByte(s[i])
		i++
	}
	flush()
}

// drawTable draws "TODAY'S BEST SCORES". With rank >= 0 the row shows name
// (with a blinking cursor at blink/25 even) and the player's score instead.
func drawTable(img *image.RGBA, f *font.Font, h *HighScores, rank int, name string, blink int) {
	title := "TODAY'S BEST SCORES"
	f.Draw(title, 160-f.Width(title)/2, 48, func(a, b int) { setGrey(img, a, b, 255) })
	for i, e := range h.Entries {
		y := 80 + 16*i
		n, score := e.Name, e.Score
		if i == rank {
			n = name
			if blink/25%2 == 0 {
				n += "_"
			}
		}
		f.Draw(n, 32, y, func(a, b int) { setGrey(img, a, b, 204) })
		num := fmt.Sprint(score * 10)
		f.Draw(num, scoreColumn-f.Width(num), y, func(a, b int) { setGrey(img, a, b, 204) })
	}
}

// wrap breaks lines at spaces so that none is wider than width pixels.
func wrap(f *font.Font, text string, width int) string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		line := ""
		for _, w := range strings.Fields(l) {
			if line != "" && f.Width(line+" "+w) > width {
				out = append(out, line)
				line = w
			} else if line == "" {
				line = w
			} else {
				line += " " + w
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
