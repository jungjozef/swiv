package level

import (
	"image"
	_ "image/png"
	"os"
	"testing"

	"github.com/jungjozef/swiv/internal/adf"
	"github.com/jungjozef/swiv/internal/pam"
)

func disk(t *testing.T) *adf.Disk {
	t.Helper()
	d, err := adf.Open("../../SWIV.adf")
	if err != nil {
		t.Skipf("no disk image: %v", err)
	}
	return d
}

func TestFileNames(t *testing.T) {
	d := disk(t)
	prog, err := d.ReadName("AMPROG.OBJ")
	if err != nil {
		t.Fatal(err)
	}
	n := fileNames(prog)
	if len(n) != 126 || n[0] != "jeepheli.lin" || n[90] != "town.pam" {
		t.Fatalf("names: %d entries, [0]=%q [90]=%q", len(n), n[0], n[90])
	}
}

func TestPAMTown(t *testing.T) {
	d := disk(t)
	raw, err := d.ReadName("TOWN.PAM")
	if err != nil {
		t.Fatal(err)
	}
	s, err := pam.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	// Facts established against the running game.
	if len(s.Commands) != 49 || len(s.Objects) != 859 {
		t.Fatalf("commands=%d objects=%d, want 49/859", len(s.Commands), len(s.Objects))
	}
	c := s.Commands[0]
	if c.Scroll != 0xE960 || c.Index != 0 || c.RGB != 0 {
		t.Fatalf("first command %+v", c)
	}
	o := s.Objects[0]
	if o.Scroll != 0xE960 || o.Type != 1 || o.X != 312 || o.Z != 1 {
		t.Fatalf("first object %+v", o)
	}
	last := s.Objects[11] // x = -1 ground tile
	if last.X != -1 || last.Z != 4 {
		t.Fatalf("object 11 %+v", last)
	}
	if s.EndScroll != 0xDC4F {
		t.Fatalf("end scroll %x", s.EndScroll)
	}
}

func TestLoadAllLevels(t *testing.T) {
	d := disk(t)
	for n := 0; n < numLevels; n++ {
		l, err := Load(d, n)
		if err != nil {
			t.Fatalf("level %d: %v", n, err)
		}
		img := image.NewRGBA(image.Rect(0, 0, ScreenW, ScreenH))
		l.Render(StartTop, img)
		l.Render(l.EndTop, img)
	}
}

// The first gameplay window the game shows must contain the ground colour
// and the bunker: spot-check against pixels taken from the emulator
// (scroll top 0xE828).
func TestTownWindowColours(t *testing.T) {
	d := disk(t)
	l, err := Load(d, 0)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, ScreenW, ScreenH))
	l.Render(0xE828, img)
	counts := map[[3]uint8]int{}
	for i := 0; i < len(img.Pix); i += 4 {
		counts[[3]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2]}]++
	}
	// Ground grain uses #653/#542/#764 (index 11/10/12).
	ground := counts[[3]uint8{0x66, 0x55, 0x33}] + counts[[3]uint8{0x55, 0x44, 0x22}] + counts[[3]uint8{0x77, 0x66, 0x44}]
	if frac := float64(ground) / float64(ScreenW*ScreenH); frac < 0.6 {
		t.Fatalf("ground fraction %.2f, want >= 0.6", frac)
	}
}

// Cross-check against the Python composite that was verified against the
// emulator (reverse/frames/town_composed_e828.png), when it is present.
func TestMatchesPythonComposite(t *testing.T) {
	f, err := os.Open("../../reverse/frames/town_composed_e828.png")
	if err != nil {
		t.Skip("no reference composite")
	}
	defer f.Close()
	ref, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	d := disk(t)
	l, err := Load(d, 0)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, ScreenW, ScreenH))
	l.Render(0xE828, img)
	same := 0
	for y := 0; y < ScreenH; y++ {
		for x := 0; x < ScreenW; x++ {
			r1, g1, b1, _ := img.At(x, y).RGBA()
			r2, g2, b2, _ := ref.At(x, y).RGBA()
			if r1 == r2 && g1 == g2 && b1 == b2 {
				same++
			}
		}
	}
	// Differences: the Python composite uses one fixed palette, this one
	// follows the per-scanline colour commands.
	if frac := float64(same) / float64(ScreenW*ScreenH); frac < 0.99 {
		t.Fatalf("only %.1f%% of pixels match the verified composite", 100*frac)
	}
}

// The game plays the levels back to back: the desert's script carries on where
// the town's ends (at $DC4F), with its own dictionary, palette and enemies.
func TestChainContinuesTheTown(t *testing.T) {
	d := disk(t)
	town, err := Load(d, 0)
	if err != nil {
		t.Fatal(err)
	}
	both, err := Chain(d, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if both.Name != "town+desert" {
		t.Fatalf("name %q", both.Name)
	}
	if len(both.Spawns) <= len(town.Spawns) || len(both.objects) <= len(town.objects) {
		t.Fatalf("the chain adds nothing: %d/%d spawns, %d/%d objects", len(both.Spawns), len(town.Spawns), len(both.objects), len(town.objects))
	}
	if both.EndTop >= town.EndTop {
		t.Fatalf("the chain ends at %04x, the town at %04x", both.EndTop, town.EndTop)
	}
	// the first desert record is a palette command 96 rows after the town's end
	var first *Spawn
	for i := range both.Spawns[len(town.Spawns):] {
		first = &both.Spawns[len(town.Spawns)+i]
		break
	}
	if first == nil || first.Prev > town.EndTop || first.Scroll > town.EndTop {
		t.Fatalf("first desert enemy %+v is not after %04x", first, town.EndTop)
	}
	// rows the scenery does not cover show the ring buffer's older content
	idx := make([]uint8, ScreenW*ScreenH)
	both.Draw(0xDC3E, idx)
	black := 0
	for _, v := range idx {
		if v == 0 {
			black++
		}
	}
	if black > len(idx)/2 {
		t.Fatalf("%d of %d pixels are colour 0 at the town/desert seam", black, len(idx))
	}
}
