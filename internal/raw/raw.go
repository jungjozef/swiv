// Package raw decodes SWIV's full-screen .RAW images: 4 bitplanes of 320x256
// followed by a 16-entry Amiga (0x0RGB) palette.
package raw

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
)

const (
	Width     = 320
	Height    = 256
	Planes    = 4
	PlaneSize = Width / 8 * Height // 10240
	Size      = PlaneSize*Planes + 32
)

// Layout selects how the bitplanes are arranged in the file.
type Layout int

const (
	// Sequential stores plane 0 whole, then plane 1, ...
	Sequential Layout = iota
	// Interleaved stores one row of each plane in turn.
	Interleaved
)

// Palette returns the image's 16 colours.
func Palette(b []byte) ([16]color.RGBA, error) {
	var pal [16]color.RGBA
	if len(b) != Size {
		return pal, fmt.Errorf("raw: want %d bytes, got %d", Size, len(b))
	}
	for i := range pal {
		w := binary.BigEndian.Uint16(b[PlaneSize*Planes+2*i:])
		pal[i] = color.RGBA{
			R: uint8(w>>8&0xF) * 17,
			G: uint8(w>>4&0xF) * 17,
			B: uint8(w&0xF) * 17,
			A: 0xFF,
		}
	}
	return pal, nil
}

// Decode converts a .RAW file to an RGBA image.
func Decode(b []byte, l Layout) (*image.RGBA, error) {
	pal, err := Palette(b)
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, Width, Height))
	rowBytes := Width / 8
	for y := range Height {
		for x := range Width {
			idx := 0
			for p := range Planes {
				var off int
				if l == Interleaved {
					off = (y*Planes+p)*rowBytes + x/8
				} else {
					off = p*PlaneSize + y*rowBytes + x/8
				}
				if b[off]>>(7-uint(x&7))&1 != 0 {
					idx |= 1 << p
				}
			}
			img.SetRGBA(x, y, pal[idx])
		}
	}
	return img, nil
}
