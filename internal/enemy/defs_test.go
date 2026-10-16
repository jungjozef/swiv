package enemy

import (
	"testing"

	"github.com/jungjozef/swiv/internal/adf"
)

func TestTownEnemyDefs(t *testing.T) {
	d, err := adf.Open("../../SWIV.adf")
	if err != nil {
		t.Skipf("no disk image: %v", err)
	}
	prog, err := d.ReadName("AMPROG.OBJ")
	if err != nil {
		t.Fatal(err)
	}
	defs, err := Load(prog)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) < 70 {
		t.Fatalf("only %d enemy types decoded", len(defs))
	}
	// Values read off the running game's constructor calls.
	want := map[uint16][3]int{ // hp, score, weight
		0x0404: {1, 12, 10},  // foddera (green helicopter)
		0x180D: {4, 35, 10},  // rotobase
		0x0011: {10, 25, 7},  // mine
		0x000F: {4, 30, 10},  // proximity mine
		0x0016: {2, 40, 10},  // camogun
		0x000E: {3, 70, 14},  // pop-up gun
		0x0013: {3, 40, 10},  // flame
		0x0015: {2, 55, 10},  // bird
		0x0058: {10, 70, 15}, // mill
	}
	if df := defs[0x180D]; df == nil || df.Draw != 0x080D {
		t.Errorf("rotobase draws with %04x, want 080d", df.Draw)
	}
	for w, v := range want {
		df, ok := defs[w]
		if !ok {
			t.Errorf("type %04x missing", w)
			continue
		}
		if df.HP != v[0] || df.Score != v[1] || df.Weight != v[2] {
			t.Errorf("type %04x: hp/score/weight = %d/%d/%d, want %v", w, df.HP, df.Score, df.Weight, v)
		}
	}
}
