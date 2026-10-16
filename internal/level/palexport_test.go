package level

import (
	"fmt"
	"os"
	"testing"
)

// Writes the Go palette for each scroll value to a text file so it can be
// compared with the emulator (only when PALEXPORT is set).
func TestExportPalettes(t *testing.T) {
	out := os.Getenv("PALEXPORT")
	if out == "" {
		t.Skip()
	}
	d := disk(t)
	l, err := Load(d, 0)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.Create(out)
	defer f.Close()
	for v := 0xDC00; v < 0xEB00; v++ {
		p := l.paletteAt(v)
		fmt.Fprintf(f, "%04x", v)
		for i := 0; i < 16; i++ {
			fmt.Fprintf(f, " %x%x%x", p[i].R/17, p[i].G/17, p[i].B/17)
		}
		fmt.Fprintln(f)
	}
}
