package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/hajimehoshi/ebiten/v2/audio"

	"github.com/jungjozef/swiv/internal/adf"
	"github.com/jungjozef/swiv/internal/sfx"

	"protracker-go/loader"
	"protracker-go/mod"
	player "protracker-player"
)

const (
	sampleRate = 44100
	// Amiga-style hard panning softened a little, as in protracker-go's default.
	stereoSep = 30
)

// Music is the seam between the game and the MOD player.
type Music interface {
	Play(track string) error
	// PlayBytes plays a ProTracker module held in memory (e.g. from the disk).
	PlayBytes(module []byte) error
	Stop()
}

// NopMusic is a silent Music, useful for tests and headless runs.
type NopMusic struct{}

func (NopMusic) Play(string) error      { return nil }
func (NopMusic) PlayBytes([]byte) error { return nil }
func (NopMusic) Stop()                  {}

// ModMusic plays ProTracker modules through Ebiten's audio context, so it
// shares the output device with any future sound effects.
type ModMusic struct {
	ctx    *audio.Context
	cur    *audio.Player
	filter bool
}

// NewModMusic creates the player. filter enables the Amiga low-pass filter.
func NewModMusic(filter bool) *ModMusic {
	return &ModMusic{ctx: audio.NewContext(sampleRate), filter: filter}
}

func (m *ModMusic) Play(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := m.PlayBytes(b); err != nil {
		return fmt.Errorf("load %s: %w", path, err)
	}
	return nil
}

func (m *ModMusic) PlayBytes(module []byte) error {
	m.Stop()

	mo, err := loader.LoadPTModule(bytes.NewReader(module))
	if err != nil {
		return err
	}

	p, err := m.ctx.NewPlayer(&loopReader{mod: mo, filter: m.filter})
	if err != nil {
		return err
	}
	p.Play()
	m.cur = p // keep a reference so the player isn't garbage collected
	return nil
}

func (m *ModMusic) Stop() {
	if m.cur != nil {
		m.cur.PauseAndStopReading()
		m.cur = nil
	}
}

// loopReader restarts the module from the beginning whenever it ends.
type loopReader struct {
	mod    *mod.PTModule
	filter bool
	r      *player.ModReader
}

func (l *loopReader) Read(p []byte) (int, error) {
	if l.r == nil {
		l.r = player.NewModReader(l.mod, stereoSep, l.filter)
	}
	n, err := l.r.Read(p)
	if err == io.EOF {
		l.r = nil
		if n > 0 {
			return n, nil
		}
		return l.Read(p)
	}
	return n, err
}

// sounds is the sound-effect player (package sfx); nil when muted.
var sounds *sfx.Engine

// startSounds loads the sampled sounds from the disk and starts the effect
// stream on the audio context.
func startSounds(ctx *audio.Context, d *adf.Disk) error {
	prog, err := d.ReadName("AMPROG.OBJ")
	if err != nil {
		return err
	}
	big, err := d.ReadName("BIGEXPL.SND")
	if err != nil {
		return err
	}
	smart, err := d.ReadName("SMART.SND")
	if err != nil {
		return err
	}
	e := sfx.New(prog, big, smart)
	p, err := ctx.NewPlayer(e)
	if err != nil {
		return err
	}
	p.SetBufferSize(40 * time.Millisecond) // small, so that the effects stay in step with the picture
	p.Play()
	sounds = e
	soundPlayer = p // keep a reference so the player isn't garbage collected
	return nil
}

var soundPlayer *audio.Player

// playSound is the hook the enemy world calls for a sound effect.
func playSound(id, x int) {
	if sounds != nil {
		sounds.Play(id, x)
	}
}
