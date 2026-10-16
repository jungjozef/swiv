package main

import (
	"bytes"
	"testing"

	"protracker-go/engine"
	"protracker-go/loader"

	"github.com/jungjozef/swiv/internal/adf"
	"github.com/jungjozef/swiv/internal/raw"
)

// The title tune and cover must load straight from the disk image.
func TestTitleAssets(t *testing.T) {
	d, err := adf.Open("SWIV.adf")
	if err != nil {
		t.Skipf("no disk image: %v", err)
	}
	cover, err := d.ReadName("COVER.RAW")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Decode(cover, raw.Sequential); err != nil {
		t.Fatal(err)
	}
	tune, err := d.ReadName("AMTITUNE.MOD")
	if err != nil {
		t.Fatal(err)
	}
	m, err := loader.LoadPTModule(bytes.NewReader(tune))
	if err != nil {
		t.Fatal(err)
	}
	// Render a second of audio to make sure the replayer accepts it.
	r := engine.NewReplayerState(m)
	for range 50 {
		if len(engine.RenderTick(r)) == 0 {
			t.Fatal("replayer produced no samples")
		}
	}
}

// The helicopter's image sequence as observed on the running game: logic
// steps (every second tick) alternate fuselage (0) and a rotor image, the
// rotor images cycling 3, 4, 1, 2.
func TestHeliAnimation(t *testing.T) {
	p := &Player{}
	var got []int
	for tick := 0; tick < 16; tick += 2 {
		got = append(got, p.image(tick))
	}
	want := []int{0, 3, 0, 4, 0, 1, 0, 2}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sequence = %v, want %v", got, want)
		}
	}
	if p.image(1) != p.image(0) || p.image(3) != p.image(2) {
		t.Fatal("image must only change every second tick")
	}
}
