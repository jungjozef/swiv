// Package font reads the game's proportional 7-row font out of AMPROG.OBJ.
//
// The table at $1C344 has 16 bytes per character code: a width word and seven
// row words. The glyph occupies the low `width` bits of each row word, most
// significant bit leftmost. Lower-case codes are shown with the upper-case
// glyphs (code - $20), and characters advance by width+1.
package font

import (
	"encoding/binary"
	"fmt"
)

const (
	progBase   = 0xEFC0
	glyphTable = 0x1C344
	// Rows is the height of every glyph.
	Rows = 7
)

type glyph struct {
	w    int
	rows [Rows]uint16
}

// Font is the game's text font.
type Font struct {
	g [256]glyph
}

// Load reads the glyph table from AMPROG.OBJ.
func Load(prog []byte) (*Font, error) {
	off := glyphTable - progBase
	if off >= len(prog) {
		return nil, fmt.Errorf("font: AMPROG.OBJ too short")
	}
	f := &Font{}
	for c := 0; c < 256; c++ {
		o := off + c*16
		if o+16 > len(prog) {
			break // the table ends with the file
		}
		f.g[c].w = int(binary.BigEndian.Uint16(prog[o:]))
		for r := 0; r < Rows; r++ {
			f.g[c].rows[r] = binary.BigEndian.Uint16(prog[o+2+2*r:])
		}
	}
	return f, nil
}

// code maps a character to its glyph index.
func code(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 0x20
	}
	return c
}

// Width is the advance of string s in pixels.
func (f *Font) Width(s string) int {
	w := 0
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 {
			continue
		}
		w += f.g[code(s[i])].w + 1
	}
	return w
}

// Draw calls set(x, y) for every lit pixel of s with its top-left at (x, y).
func (f *Font) Draw(s string, x, y int, set func(x, y int)) {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 {
			continue
		}
		g := &f.g[code(s[i])]
		for r := 0; r < Rows; r++ {
			row := g.rows[r]
			for b := 0; b < g.w; b++ {
				if row>>(uint(g.w-1-b))&1 != 0 {
					set(x+b, y+r)
				}
			}
		}
		x += g.w + 1
	}
}
