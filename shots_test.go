package main

import (
	"testing"

	"github.com/jungjozef/swiv/internal/adf"
)

func newShots(t *testing.T) *Shots {
	t.Helper()
	d, err := adf.Open("SWIV.adf")
	if err != nil {
		t.Skipf("no disk image: %v", err)
	}
	s, err := NewShots(d)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Measured on the running game with fire held and the craft at (288, 192):
// a twin pair every 12 ticks, each bullet visible for 10 logic steps
// (20 ticks) before it leaves the top of the screen, 18 rows per step.
func TestFireCadenceAndLifetime(t *testing.T) {
	s := newShots(t)
	const top = 0xE841
	launches, maxActive := 0, 0
	prevFired := 0
	for tick := 0; tick < 120; tick += 2 { // logic runs every second tick
		s.Step(2, true, 288, 192, top)
		if s.Fired != prevFired {
			launches++
			prevFired = s.Fired
		}
		maxActive = max(maxActive, s.Active())
	}
	if launches != 10 {
		t.Fatalf("%d volleys in 120 ticks, want 10", launches)
	}
	if maxActive != 4 {
		t.Fatalf("at most %d bullets in flight, want 4 (two twin pairs)", maxActive)
	}
}

func TestBulletTravel(t *testing.T) {
	s := newShots(t)
	const top = 0xE841
	s.Step(2, true, 288, 192, top) // spawn at y-4, then one step of 18
	var ys, xs []int
	for i := range s.pool {
		if s.pool[i].active {
			ys = append(ys, s.pool[i].y-top)
			xs = append(xs, s.pool[i].x)
		}
	}
	if len(ys) != 2 || ys[0] != 192-4-18 || xs[0] != 286 || xs[1] != 290 {
		t.Fatalf("after first step: x=%v screen y=%v, want x=[286 290] y=170", xs, ys)
	}
	// ten steps later it has left the screen
	for i := 0; i < 11; i++ {
		s.Step(2, false, 288, 192, top)
	}
	if s.Active() != 0 {
		t.Fatalf("%d bullets still active", s.Active())
	}
}
