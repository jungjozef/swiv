package lin_test

import (
	"strings"
	"testing"

	"github.com/jungjozef/swiv/internal/adf"
	"github.com/jungjozef/swiv/internal/lin"
)

// Every .LIN on the disk must parse exactly to its end.
func TestParseAllLIN(t *testing.T) {
	d, err := adf.Open("../../SWIV.adf")
	if err != nil {
		t.Skipf("no disk image: %v", err)
	}
	ok, bad := 0, 0
	for i, e := range d.Entries {
		if !strings.HasSuffix(strings.ToUpper(e.Name), ".LIN") {
			continue
		}
		b, err := d.Read(i)
		if err != nil {
			t.Fatal(err)
		}
		bank, err := lin.Parse(b)
		if err != nil {
			t.Logf("%-14s %v", e.Name, err)
			bad++
			continue
		}
		ok++
		if len(bank.Frames) == 0 {
			t.Errorf("%s: no frames", e.Name)
		}
	}
	t.Logf("%d parsed, %d failed", ok, bad)
	if bad > 0 {
		t.Fail()
	}
}

// PLOP.LIN frame 1 is a 15x15 ring: known pixel values pin down the plane
// order and the transparent key.
func TestPlopPixels(t *testing.T) {
	d, err := adf.Open("../../SWIV.adf")
	if err != nil {
		t.Skipf("no disk image: %v", err)
	}
	b, err := d.ReadName("PLOP.LIN")
	if err != nil {
		t.Fatal(err)
	}
	bank, err := lin.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(bank.Frames) != 3 || len(bank.Images) != 3 {
		t.Fatalf("frames=%d images=%d, want 3/3", len(bank.Frames), len(bank.Images))
	}
	f := bank.Frames[1]
	if f.W != 15 || f.H != 15 || f.Key != 14 || f.HotX != 7 || f.HotY != 7 {
		t.Fatalf("header = %+v", f)
	}
	for _, c := range []struct {
		x, y int
		want uint8
	}{{0, 0, 14}, {0, 7, 7}, {7, 7, 8}, {14, 7, 7}, {14, 0, 14}} {
		if got := f.Pixel(c.x, c.y); got != c.want {
			t.Errorf("pixel(%d,%d) = %d, want %d", c.x, c.y, got, c.want)
		}
	}
}

// _RUNWAY.LIN has 13 frames forming 7 images; the game's own table (read from
// emulated memory) lists image 0 = frames 0-2, 1 = 3, 2 = 4, 3 = 5,
// 4 = 6-7, 5 = 8-9, 6 = 10-12.
func TestRunwayImages(t *testing.T) {
	d, err := adf.Open("../../SWIV.adf")
	if err != nil {
		t.Skipf("no disk image: %v", err)
	}
	b, err := d.ReadName("_RUNWAY.LIN")
	if err != nil {
		t.Fatal(err)
	}
	bank, err := lin.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]int{{0, 1, 2}, {3}, {4}, {5}, {6, 7}, {8, 9}, {10, 11, 12}}
	if len(bank.Images) != len(want) {
		t.Fatalf("images = %v, want %v", bank.Images, want)
	}
	for i := range want {
		if len(bank.Images[i]) != len(want[i]) {
			t.Fatalf("image %d = %v, want %v", i, bank.Images[i], want[i])
		}
		for j := range want[i] {
			if bank.Images[i][j] != want[i][j] {
				t.Fatalf("image %d = %v, want %v", i, bank.Images[i], want[i])
			}
		}
	}
}

// Hotspots are signed bytes: _RUNWAY.LIN image 0 has strips with hotX 26, -6, -38.
func TestSignedHotspots(t *testing.T) {
	d, err := adf.Open("../../SWIV.adf")
	if err != nil {
		t.Skipf("no disk image: %v", err)
	}
	b, err := d.ReadName("_RUNWAY.LIN")
	if err != nil {
		t.Fatal(err)
	}
	bank, err := lin.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	got := []int{bank.Frames[0].HotX, bank.Frames[1].HotX, bank.Frames[2].HotX}
	if got[0] != 26 || got[1] != -6 || got[2] != -38 {
		t.Fatalf("hotX = %v, want [26 -6 -38]", got)
	}
}
