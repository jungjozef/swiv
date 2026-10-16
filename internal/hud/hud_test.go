package hud

import (
	"os"
	"strings"
	"testing"

	"github.com/jungjozef/swiv/internal/adf"
	"github.com/jungjozef/swiv/internal/font"
)

func loadFont(t *testing.T) *font.Font {
	t.Helper()
	d, err := adf.Open("../../SWIV.adf")
	if err != nil {
		t.Skipf("no disk image: %v", err)
	}
	prog, err := d.ReadName("AMPROG.OBJ")
	if err != nil {
		t.Fatal(err)
	}
	f, err := font.Load(prog)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestWidths(t *testing.T) {
	f := loadFont(t)
	// Decoded from the original table: 'H' is 8 wide, digits 7, space 4.
	if got := f.Width("H"); got != 9 {
		t.Fatalf("Width(H) = %d, want 9", got)
	}
	if got := f.Width("0"); got != 8 {
		t.Fatalf("Width(0) = %d, want 8", got)
	}
	if got := f.Width(" "); got != 5 {
		t.Fatalf("Width(space) = %d, want 5", got)
	}
}

// The status line must be pixel-identical to the strip the original draws
// (captured from the running game at frame 1301: left "Heli 3[ 2* 0000000",
// right "Jeep 0[ 2* 0000000").
func TestMatchesOriginalStrip(t *testing.T) {
	raw, err := os.ReadFile("../../reverse/frames/hud_ref_mask.txt")
	if err != nil {
		t.Skip("no reference mask")
	}
	f := loadFont(t)
	m := Mask(f, "Heli 3[ 2* 0000000", "Jeep 0[ 2* 0000000")
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != Rows {
		t.Fatalf("reference has %d rows", len(lines))
	}
	bad := 0
	for r := 0; r < Rows; r++ {
		for x := 0; x < Width && x < len(lines[r]); x++ {
			if (lines[r][x] == '#') != m[r][x] {
				bad++
			}
		}
	}
	if bad != 0 {
		t.Fatalf("%d pixels differ from the original status strip", bad)
	}
}

// Status text is the blue gradient on every row regardless of what is
// underneath; sprite colours follow the white / grey / animated pattern.
func TestColours(t *testing.T) {
	for r := 0; r < Rows; r++ {
		c := rgb12(topColour[r])
		if c.B < c.R { // bluish: blue never below red
			t.Fatalf("row %d colour %v is not blue-ish", r, c)
		}
	}
	if c := SpriteColour(1, 0, 0); c.R != 0xFF || c.G != 0xFF || c.B != 0xFF {
		t.Fatalf("sprite colour 1 = %v, want white", c)
	}
	if c := SpriteColour(2, 3, 5); c.R != 0x99 || c.G != 0x99 || c.B != 0x99 {
		t.Fatalf("sprite colour 2 = %v, want grey", c)
	}
	// animated entry: counter 14, group 0 -> table[14] = 0x0FF0 (yellow)
	if c := SpriteColour(3, 0, 14); c.R != 0xFF || c.G != 0xFF || c.B != 0 {
		t.Fatalf("animated colour = %v, want yellow", c)
	}
}
