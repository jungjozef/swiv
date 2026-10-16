// Package hud draws the game's status line over the playfield, and holds the
// sprite palette (colours 17-31) used by the hardware sprites.
//
// The status text is always drawn in the blue bevel gradient of colour 16
// (see topColour), whatever is underneath it. (The original keeps the text in
// a separate 1-bitplane strip shown as a fifth bitplane on screen rows 8-14,
// so a lit pixel is colour 16 + the playfield index below; the game shows it
// as the gradient everywhere.)
//
// Colours 16-31 are arranged in groups of four, one group per pair of
// hardware sprites:
//
//	16+4g+0  transparent for sprites (group 0: the text gradient)
//	16+4g+1  white
//	16+4g+2  grey
//	16+4g+3  animated (cycles through a table, indexed by a frame counter)
package hud

import (
	"image"
	"image/color"

	"github.com/jungjozef/swiv/internal/font"
)

const (
	// Top is the first screen row of the strip and Rows its height.
	Top  = 8
	Rows = font.Rows
	// Width is the playfield width.
	Width = 320

	// LeftX and RightX are the text anchors used by the game: the left text
	// starts at x=8 and the right text ends at x=312.
	LeftX  = 8
	RightX = 312
)

// anim is the colour table at $11B22 (16 entries, indexed by counter & 15).
var anim = [16]uint16{
	0x0C00, 0x0FF0, 0x0FC0, 0x0800, 0x0F80, 0x0F00, 0x0C00, 0x0FF0,
	0x0C80, 0x0FF0, 0x0F80, 0x0800, 0x0FF0, 0x0000, 0x0FF0, 0x0800,
}

// topColour is colour 16 on each strip row: the bevel shading behind text
// that sits over the black part of the playfield.
var topColour = [Rows]uint16{0xDDF, 0xAAE, 0xCCF, 0xCCF, 0xCCF, 0xAAE, 0x88D}

func rgb12(v uint16) color.RGBA {
	return color.RGBA{R: uint8(v>>8&0xF) * 17, G: uint8(v>>4&0xF) * 17, B: uint8(v&0xF) * 17, A: 0xFF}
}

// SpriteColour is the colour of a hardware sprite pixel of value v (1..3)
// for sprite channel group g (channels 2g and 2g+1), with animation counter
// ctr. Value 0 is transparent.
func SpriteColour(v, g, ctr int) color.RGBA {
	switch v {
	case 1:
		return rgb12(0xFFF)
	case 2:
		return rgb12(0x999)
	default:
		return rgb12(anim[(ctr+g)&15])
	}
}

// Mask renders left (anchored at LeftX) and right (right-aligned to RightX)
// into a Rows x Width bit mask.
func Mask(f *font.Font, left, right string) [Rows][Width]bool {
	var m [Rows][Width]bool
	set := func(x, y int) {
		if x >= 0 && x < Width && y >= 0 && y < Rows {
			m[y][x] = true
		}
	}
	if left != "" {
		f.Draw(left, LeftX, 0, set)
	}
	if right != "" {
		f.Draw(right, RightX-f.Width(right), 0, set)
	}
	return m
}

// Draw overlays the status line on dst in the gradient colours.
func Draw(dst *image.RGBA, f *font.Font, left, right string) {
	m := Mask(f, left, right)
	for r := 0; r < Rows; r++ {
		y := Top + r
		for x := 0; x < Width; x++ {
			if !m[r][x] {
				continue
			}
			c := rgb12(topColour[r])
			o := y*dst.Stride + 4*x
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = c.R, c.G, c.B, 0xFF
		}
	}
}
