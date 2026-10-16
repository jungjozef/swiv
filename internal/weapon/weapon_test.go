package weapon

import (
	"testing"

	"github.com/jungjozef/swiv/internal/adf"
)

func load(t *testing.T) *Table {
	t.Helper()
	d, err := adf.Open("../../SWIV.adf")
	if err != nil {
		t.Skipf("no disk image: %v", err)
	}
	prog, err := d.ReadName("AMPROG.OBJ")
	if err != nil {
		t.Fatal(err)
	}
	tb, err := Load(prog)
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

// Observed on the running game: the helicopter's default weapon (level 2) is
// a twin shot at x-2 and x+2, moving up 9 pixels per tick, image 14.
func TestDefaultTwinShot(t *testing.T) {
	v := load(t).Volley(HeadingUp, 2, false)
	if len(v) != 2 {
		t.Fatalf("volley = %v", v)
	}
	for i, wantDX := range []int{-2, 2} {
		s := v[i]
		if s.VX != 0 || s.VY != -9 || s.DX != wantDX || s.DY != -4 || s.Image != 14 {
			t.Fatalf("shot %d = %+v", i, s)
		}
	}
}

func TestLevels(t *testing.T) {
	tb := load(t)
	// level 1 is a single shot straight ahead; odd levels add the side guns.
	if v := tb.Volley(HeadingUp, 1, false); len(v) != 1 || v[0].DX != 0 || v[0].DY != -8 {
		t.Fatalf("level 1 = %v", v)
	}
	if v := tb.Volley(HeadingUp, 3, false); len(v) != 3 || v[1].DX != -4 || v[2].DX != 4 {
		t.Fatalf("level 3 = %v", v)
	}
	if v := tb.Volley(HeadingUp, 5, false); len(v) != 5 || v[3].DX != -8 || v[4].DX != 8 || v[3].DY != 8 {
		t.Fatalf("level 5 = %v", v)
	}
	// beyond 5: eight bullets in all directions with images 8..15.
	v := tb.Volley(HeadingUp, 6, false)
	if len(v) != 8 {
		t.Fatalf("level 6 = %v", v)
	}
	seen := map[int]bool{}
	for _, s := range v {
		seen[s.Image] = true
	}
	for img := 8; img <= 15; img++ {
		if !seen[img] {
			t.Fatalf("fan lacks image %d: %v", img, v)
		}
	}
}
