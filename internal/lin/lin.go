// Package lin decodes SWIV's .LIN sprite banks.
//
// File layout (big endian):
//
//	u16 count           number of images
//	frames, back to back until end of file.  A frame whose flags bit 0 is
//	set continues into the next frame: an image is a chain of frames, in
//	practice 32-pixel-wide strips laid out left to right (each strip's
//	hotspot is 32 less than the previous one's), so `count` images are
//	made of at least `count` frames:
//	    u16 size            bytes of pixel data
//	    u8  w, h            size in pixels
//	    i8  hotX, hotY      hotspot (signed: e.g. -6, or 63 in a 128-wide image)
//	    u8  key             colour index treated as transparent
//	    u8  flags           bit 0 = continued by the next frame; other bits
//	                        (0x02, 0x04, ...) not yet understood
//	    u8  cx, cy          secondary offset / collision point
//	    size bytes          h rows; each row = 4 planes, each plane
//	                        ceil(w/16) 16-bit words, plane-major
package lin

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
)

const Planes = 4

// Frame is one sprite image.
type Frame struct {
	W, H       int
	HotX, HotY int
	Key        uint8 // transparent colour index
	Flags      uint8
	CX, CY     int8
	data       []byte
}

// WordsPerRow is the number of 16-bit words per plane per row.
func (f *Frame) WordsPerRow() int { return (f.W + 15) / 16 }

// Pixel returns the colour index (0..15) at x, y.
func (f *Frame) Pixel(x, y int) uint8 {
	wpr := f.WordsPerRow()
	rowBytes := Planes * wpr * 2
	var idx uint8
	for p := range Planes {
		off := y*rowBytes + (p*wpr+x/16)*2
		w := binary.BigEndian.Uint16(f.data[off:])
		if w>>(15-uint(x&15))&1 != 0 {
			idx |= 1 << p
		}
	}
	return idx
}

// Indices returns the frame's colour indices, row-major, W*H entries.
func (f *Frame) Indices() []uint8 {
	out := make([]uint8, f.W*f.H)
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			out[y*f.W+x] = f.Pixel(x, y)
		}
	}
	return out
}

// Image renders the frame with pal, making Key transparent.
func (f *Frame) Image(pal *[16]color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, f.W, f.H))
	for y := range f.H {
		for x := range f.W {
			i := f.Pixel(x, y)
			if i == f.Key {
				continue
			}
			img.SetRGBA(x, y, pal[i])
		}
	}
	return img
}

// Bank is a decoded .LIN file.
type Bank struct {
	Frames []Frame
	// Images[i] lists the indices (into Frames) of the strips of image i.
	// This is the numbering the game uses (frame index = A/2 in a dictionary word).
	Images [][]int
}

// ImageRect returns the bounding box of image i relative to its hotspot:
// strip j is drawn at (-HotX_j, -HotY_j) from the object position.
func (b *Bank) ImageRect(i int) image.Rectangle {
	var r image.Rectangle
	for k, fi := range b.Images[i] {
		f := &b.Frames[fi]
		fr := image.Rect(-f.HotX, -f.HotY, -f.HotX+f.W, -f.HotY+f.H)
		if k == 0 {
			r = fr
		} else {
			r = r.Union(fr)
		}
	}
	return r
}

// DrawImage composites image i onto dst with its hotspot at (x, y).
func (b *Bank) DrawImage(dst *image.RGBA, i, x, y int, pal *[16]color.RGBA) {
	for _, fi := range b.Images[i] {
		f := &b.Frames[fi]
		for py := 0; py < f.H; py++ {
			for px := 0; px < f.W; px++ {
				c := f.Pixel(px, py)
				if c == f.Key {
					continue
				}
				dx, dy := x-f.HotX+px, y-f.HotY+py
				if image.Pt(dx, dy).In(dst.Bounds()) {
					dst.SetRGBA(dx, dy, pal[c])
				}
			}
		}
	}
}

// Parse decodes a .LIN file.
func Parse(b []byte) (*Bank, error) {
	if len(b) < 2 {
		return nil, fmt.Errorf("lin: file too short")
	}
	n := int(binary.BigEndian.Uint16(b))
	var frames []Frame
	p := 2
	for i := 0; p < len(b); i++ {
		if p+10 > len(b) {
			return nil, fmt.Errorf("lin: frame %d header beyond end", i)
		}
		size := int(binary.BigEndian.Uint16(b[p:]))
		f := Frame{
			W: int(b[p+2]), H: int(b[p+3]),
			HotX: int(int8(b[p+4])), HotY: int(int8(b[p+5])),
			Key: b[p+6], Flags: b[p+7],
			CX: int8(b[p+8]), CY: int8(b[p+9]),
		}
		p += 10
		if p+size > len(b) {
			return nil, fmt.Errorf("lin: frame %d data beyond end", i)
		}
		if want := f.H * f.WordsPerRow() * Planes * 2; want != size {
			return nil, fmt.Errorf("lin: frame %d: size %d, expected %d for %dx%d", i, size, want, f.W, f.H)
		}
		f.data = b[p : p+size]
		p += size
		frames = append(frames, f)
	}
	bank := &Bank{Frames: frames}
	cont := false
	for i, f := range frames {
		if !cont {
			bank.Images = append(bank.Images, nil)
		}
		last := len(bank.Images) - 1
		bank.Images[last] = append(bank.Images[last], i)
		cont = f.Flags&1 != 0
	}
	if cont {
		return nil, fmt.Errorf("lin: last frame claims a continuation")
	}
	if n != len(bank.Images) {
		return nil, fmt.Errorf("lin: header count %d, but frames form %d images", n, len(bank.Images))
	}
	return bank, nil
}

// BlitIndexed draws image i into dst (w x h colour indices) with its hotspot
// at (x, y). Pixels equal to the frame's key are transparent. With erase set,
// every opaque pixel is written as colour 0 instead (the game's shadow mode).
func (b *Bank) BlitIndexed(dst []uint8, w, h, i, x, y int, erase bool) {
	for _, fi := range b.Images[i] {
		f := &b.Frames[fi]
		px := f.Indices()
		for sy := 0; sy < f.H; sy++ {
			dy := y - f.HotY + sy
			if dy < 0 || dy >= h {
				continue
			}
			for sx := 0; sx < f.W; sx++ {
				dx := x - f.HotX + sx
				if dx < 0 || dx >= w {
					continue
				}
				c := px[sy*f.W+sx]
				if c == f.Key {
					continue
				}
				if erase {
					c = 0
				}
				dst[dy*w+dx] = c
			}
		}
	}
}

// BlitSolid draws the silhouette of image i (every opaque pixel) in colour c:
// the game's hit flash (blit mode "A or C" on planes 0 and 3, which is
// colour 9, white in every level palette).
func (b *Bank) BlitSolid(dst []uint8, w, h, i, x, y int, c uint8) {
	for _, fi := range b.Images[i] {
		f := &b.Frames[fi]
		for sy := 0; sy < f.H; sy++ {
			dy := y - f.HotY + sy
			if dy < 0 || dy >= h {
				continue
			}
			for sx := 0; sx < f.W; sx++ {
				dx := x - f.HotX + sx
				if dx < 0 || dx >= w || f.Pixel(sx, sy) == f.Key {
					continue
				}
				dst[dy*w+dx] = c
			}
		}
	}
}

// Hardware reports whether image i is drawn as a hardware sprite (frame flag
// 0x08): such images use the sprite palette, not the playfield's.
func (b *Bank) Hardware(i int) bool {
	return len(b.Images[i]) > 0 && b.Frames[b.Images[i][0]].Flags&8 != 0
}

// Each calls fn for every opaque pixel of image i drawn with its hotspot at
// (x, y): (px, py) is the pixel position and c its colour index.
func (b *Bank) Each(i, x, y int, fn func(px, py int, c uint8)) {
	for _, fi := range b.Images[i] {
		f := &b.Frames[fi]
		for sy := 0; sy < f.H; sy++ {
			for sx := 0; sx < f.W; sx++ {
				if c := f.Pixel(sx, sy); c != f.Key {
					fn(x-f.HotX+sx, y-f.HotY+sy, c)
				}
			}
		}
	}
}
