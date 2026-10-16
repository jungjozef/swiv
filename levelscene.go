package main

import (
	"fmt"
	"image"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/jungjozef/swiv/internal/adf"
	"github.com/jungjozef/swiv/internal/font"
	"github.com/jungjozef/swiv/internal/hud"
	"github.com/jungjozef/swiv/internal/level"
)

// ticksPerRow is the original's scroll speed: 25 rows per 100 frames of the
// 50 Hz display (measured from the running game).
const ticksPerRow = 4

// LevelScene scrolls a level upwards through the playfield window.
type LevelScene struct {
	lv      *level.Level
	top     int // scroll value of the top screen row; counts down
	tick    int
	paused  bool
	debug   bool
	buf     *image.RGBA
	idx     []uint8
	screen  *ebiten.Image
	startAt int

	disk   *adf.Disk
	player *Player
	shots  *Shots
	enemy  *Enemies
	font   *font.Font
	// Status line contents (the game state behind them is not ported yet).
	left, right string
	// end is the end-of-game sequence: statistics, name entry, the table.
	end    *endScreen
	scores *HighScores
	// onExit leaves the game after the end screens (back to the title).
	onExit   func()
	overWait int  // passes since the last vehicle was lost
	runUp    bool // playing the time up to the start position: no end screens
}

// endScreen phases ($FD62, $12000, $12330).
const (
	phaseStats = iota // "Game Over" / "Game Complete" and the counters
	phaseName         // the name of a score that made the table
	phaseTable        // TODAY'S BEST SCORES
)

type endScreen struct {
	title string
	phase int
	wait  int // display ticks in the phase
	rank  int // row of the new score, -1 when it did not make the table
	name  string
}

const (
	endTimeout  = 500 // 10 s: the original waits $190..$1F4 frames
	nameMaxLen  = 31  // $1F at $12180
	nameMaxPix  = 0xB0
	scoreColumn = 290 // $122
)

// NewLevelScene starts level lv at scroll value top, with the player and the
// status line.
func NewLevelScene(d *adf.Disk, lv *level.Level, top int) (*LevelScene, error) {
	prog, err := d.ReadName("AMPROG.OBJ")
	if err != nil {
		return nil, err
	}
	f, err := font.Load(prog)
	if err != nil {
		return nil, err
	}
	pl, err := NewPlayer(d)
	if err != nil {
		return nil, err
	}
	sh, err := NewShots(d)
	if err != nil {
		return nil, err
	}
	sh.rec = pl.PlayerRec
	en, err := NewEnemies(d, lv, pl, level.StartTop)
	if err != nil {
		return nil, err
	}
	sc := &LevelScene{
		lv:      lv,
		top:     top,
		startAt: top,
		buf:     image.NewRGBA(image.Rect(0, 0, level.ScreenW, level.ScreenH)),
		idx:     make([]uint8, level.ScreenW*level.ScreenH),
		screen:  ebiten.NewImage(level.ScreenW, level.ScreenH),
		debug:   true,
		disk:    d,
		player:  pl,
		shots:   sh,
		enemy:   en,
		font:    f,
		scores:  NewHighScores(d),
		left:    "Heli 3[ 2* 0000000",
		right:   "Jeep 0[ 2* 0000000",
	}
	sc.restart()
	return sc, nil
}

// restart puts the level back to its start. Starting further down the level
// than the beginning (-top) plays the intervening time without input, so the
// enemies are where they would be.
func (s *LevelScene) restart() {
	s.top, s.tick = level.StartTop, 0
	s.player.Reset()
	s.end, s.overWait = nil, 0
	s.shots.Bullets = 0
	s.enemy.Reset(level.StartTop)
	switch {
	case s.startAt < level.StartTop:
		s.runUp, s.enemy.Silent = true, true
		s.Advance((level.StartTop-s.startAt)*ticksPerRow, 0, 0, false)
		s.runUp, s.enemy.Silent = false, false
	case s.startAt > level.StartTop:
		s.top = s.startAt
	}
	s.end, s.overWait = nil, 0 // the run-up to the start position has no end screen
}

// Advance runs the game for the given number of display ticks. Game logic
// runs on every second tick (25 Hz, like the original); the scroll moves one
// row every four ticks. dx, dy is the joystick, fire the fire button.
func (s *LevelScene) Advance(ticks int, dx, dy int, fire bool) {
	for i := 0; i < ticks && s.end == nil; i++ {
		s.tick++
		s.enemy.world.TickFlash()
		if s.tick%ticksPerRow == 0 && !s.enemy.world.BossEngaged() {
			s.top--
		}
		if s.tick%2 != 0 {
			continue
		}
		if s.player.Alive {
			s.player.Move(dx, dy)
		}
		s.shots.Step(2, fire && s.player.Alive, s.player.X, s.player.Y, s.top)
		s.enemy.Step(2, s.top, s.shots)
		switch {
		case s.runUp:
		case s.enemy.world.GameComplete() && s.enemy.world.Black() >= 0x100:
			s.beginEnd("Game Complete") // ($1B16E) the fade to black is done
		case s.player.Over && !s.player.Alive:
			if s.overWait += 2; s.overWait >= overDelay {
				s.beginEnd("Game Over")
			}
		}
		switch {
		case s.player.Over && !s.player.Alive:
			s.left = "GAME OVER"
		default:
			s.left = fmt.Sprintf("Heli %d[ 2* %07d", s.player.Lives, s.player.Score*10)
		}
	}
}

func (s *LevelScene) Update() error {
	if s.end != nil {
		s.updateEnd()
		return nil
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyP) || pad.pausePr {
		s.paused = !s.paused
	}
	if debugKeys {
		if inpututil.IsKeyJustPressed(ebiten.KeyH) {
			s.debug = !s.debug
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyHome) {
			s.restart()
		}
		// T drops five tokens (the town's boss does that when it is destroyed;
		// it is not ported yet): shoot a token to change its kind, fly into it.
		if inpututil.IsKeyJustPressed(ebiten.KeyT) {
			s.enemy.world.DropTokens(160, int16(uint16(s.top)+60), 5)
		}
		// Digits 1-6 set the weapon level.
		for k := ebiten.Key1; k <= ebiten.Key6; k++ {
			if inpututil.IsKeyJustPressed(k) {
				s.shots.SetLevel(int(k-ebiten.Key1) + 1)
			}
		}
	}
	// PageUp/PageDown scroll by hand (the original has no such control, it is
	// for looking around).
	switch {
	case debugKeys && ebiten.IsKeyPressed(ebiten.KeyPageUp):
		s.top--
	case debugKeys && ebiten.IsKeyPressed(ebiten.KeyPageDown):
		s.top++
	case !s.paused:
		dx, dy, fire := readInput()
		s.Advance(1, dx, dy, fire)
	}
	if s.top < s.lv.EndTop {
		s.top = s.lv.EndTop
	}
	if s.top > level.StartTop+0x100 {
		s.top = level.StartTop + 0x100
	}
	return nil
}

// Compose renders the full frame (scenery, helicopter, status line) into buf.
func (s *LevelScene) Compose() {
	s.lv.Draw(s.top, s.idx)
	s.enemy.Draw(s.idx, level.ScreenW, level.ScreenH, s.top)
	s.player.Draw(s.idx, level.ScreenW, level.ScreenH, s.tick)
	s.lv.Colourise(s.top, s.idx, s.buf)
	// Bullets are hardware sprites in the original: drawn over the playfield
	// with the sprite palette. Its animated colours step with a counter that
	// advances on three of every four ticks.
	s.shots.DrawSprites(s.buf, s.top, s.tick*3/4)
	s.enemy.DrawSprites(s.buf, s.top, s.tick*3/4)
	hud.Draw(s.buf, s.font, s.left, s.right)
	if s.end != nil {
		s.drawEnd()
		return
	}
	if l := s.enemy.world.Black(); l > 0 {
		fadeBlack(s.buf, l)
	}
	if l := s.enemy.world.Flash(); l > 0 {
		flashWhite(s.buf, l)
	}
}

// drawStatistics is the first end screen ($FD62): "Game Complete" and the counters
// the game kept ($30C6 bullets, $30D2 destroyed, $30CE constructed, $30CA
// tokens), the percentage of the level is 100 when the game is complete.
func (s *LevelScene) drawStatistics(title string) {
	s.clear()
	put := s.putText
	w := s.enemy.world
	put(title, 20, 48, 255)
	rows := []struct {
		label string
		value int
	}{
		{"Bullets fired:", s.shots.Bullets},
		{"Enemies destroyed:", s.enemy.Kills},
		{"Enemies escaped:", w.Created - s.enemy.Kills},
		{"Tokens picked up:", w.Tokens()},
		{"Percentage completed:", 100},
	}
	for i, r := range rows {
		y := 80 + 16*i
		put(r.label, 20, y, 204)
		put(fmt.Sprint(r.value), 230, y, 204)
	}
}

// flashWhite blends every colour towards white by level/256, nibble by
// nibble as the display code does ($13A00): 15 - ((15 - c) * (256 - level) >> 8).
func flashWhite(img *image.RGBA, level int) {
	var lut [16]uint8
	for n := 0; n < 16; n++ {
		lut[n] = uint8((15 - ((15-n)*(256-level))>>8) * 17)
	}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = lut[img.Pix[i]/17]
		img.Pix[i+1] = lut[img.Pix[i+1]/17]
		img.Pix[i+2] = lut[img.Pix[i+2]/17]
	}
}

// fadeBlack darkens every colour by level/256 (the same nibble blend,
// $13A08: c * (256 - level) >> 8).
func fadeBlack(img *image.RGBA, level int) {
	var lut [16]uint8
	for n := 0; n < 16; n++ {
		lut[n] = uint8((n * (256 - level) >> 8) * 17)
	}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = lut[img.Pix[i]/17]
		img.Pix[i+1] = lut[img.Pix[i+1]/17]
		img.Pix[i+2] = lut[img.Pix[i+2]/17]
	}
}

func (s *LevelScene) Draw(screen *ebiten.Image) {
	s.Compose()
	s.screen.WritePixels(s.buf.Pix)
	screen.DrawImage(s.screen, nil)
	if s.debug && debugKeys {
		pos := float64(level.StartTop-s.top) / float64(level.StartTop-s.lv.EndTop)
		ebitenutil.DebugPrintAt(screen, fmt.Sprintf("%s  scroll %04X  %3.0f%%", strings.Split(s.lv.Name, "+")[0], s.top, 100*pos), 4, 232)
		ebitenutil.DebugPrintAt(screen, fmt.Sprintf("arrows fly  Space fire  1-6 weapon (now %d)  P pause  H hide", s.shots.Level), 4, 244)
	}
}

func (s *LevelScene) Layout(_, _ int) (int, int) { return level.ScreenW, level.ScreenH }

const overDelay = 150 // passes between the last explosion and the end screens

// beginEnd starts the end-of-game sequence.
func (s *LevelScene) beginEnd(title string) {
	rank := -1
	if s.scores != nil {
		rank = s.scores.Rank(s.player.Score)
	}
	s.end = &endScreen{title: title, rank: rank}
}

func (s *LevelScene) clear() {
	for i := 0; i < len(s.buf.Pix); i += 4 {
		s.buf.Pix[i], s.buf.Pix[i+1], s.buf.Pix[i+2], s.buf.Pix[i+3] = 0, 0, 0, 255
	}
}

// putText draws text at x, y with the grey level v.
func (s *LevelScene) putText(text string, x, y int, v uint8) {
	s.font.Draw(text, x, y, func(px, py int) {
		if px >= 0 && py >= 0 && px < level.ScreenW && py < level.ScreenH {
			o := (py*level.ScreenW + px) * 4
			s.buf.Pix[o], s.buf.Pix[o+1], s.buf.Pix[o+2] = v, v, v
		}
	})
}

// drawEnd draws the current end screen.
func (s *LevelScene) drawEnd() {
	e := s.end
	if e.phase == phaseStats {
		s.drawStatistics(e.title)
		return
	}
	s.clear()
	title := "TODAY'S BEST SCORES"
	s.putText(title, 160-s.font.Width(title)/2, 48, 255)
	if e.phase == phaseName {
		head := "1up player please enter your name"
		s.putText(head, 160-s.font.Width(head)/2, 24, 255)
	}
	for i, en := range s.scores.Entries {
		y := 80 + 16*i
		name := en.Name
		score := en.Score
		if e.phase == phaseName && i == e.rank {
			name = e.name
			if e.wait/25%2 == 0 {
				name += "_" // the blinking cursor
			}
			score = s.player.Score
		}
		s.putText(name, 32, y, 204)
		num := fmt.Sprint(score * 10)
		s.putText(num, scoreColumn-s.font.Width(num), y, 204)
	}
}

// updateEnd handles the keys of the end screens.
func (s *LevelScene) updateEnd() {
	e := s.end
	e.wait++
	pressed := inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeySpace) ||
		inpututil.IsKeyJustPressed(ebiten.KeyZ) || inpututil.IsKeyJustPressed(ebiten.KeyControlLeft) || pad.firePr
	next := func() {
		e.wait = 0
		switch e.phase {
		case phaseStats:
			if e.rank >= 0 {
				e.phase = phaseName
			} else {
				e.phase = phaseTable
			}
		case phaseName:
			name := strings.TrimSpace(e.name)
			if name == "" {
				name = "SWIV"
			}
			s.scores.Insert(name, s.player.Score)
			e.phase = phaseTable
		default:
			if s.onExit != nil {
				s.onExit()
			} else {
				s.restart()
			}
		}
	}
	switch e.phase {
	case phaseName:
		for _, r := range ebiten.AppendInputChars(nil) {
			c := byte(strings.ToUpper(string(r))[0])
			if r > 0x7f || c < 0x20 || c > 0x5a || len(e.name) >= nameMaxLen {
				continue
			}
			if s.font.Width(e.name+string(rune(c))) < nameMaxPix {
				e.name += string(rune(c))
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) && e.name != "" {
			e.name = e.name[:len(e.name)-1]
		}
		// without a keyboard the fire button accepts the name as it is
		if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || pad.firePr || e.wait > 40*50 {
			next()
		}
	default:
		if pressed || e.wait > endTimeout {
			next()
		}
	}
}
