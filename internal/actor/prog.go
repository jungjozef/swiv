// Package actor runs the enemy tasks of SWIV.
//
// The original game gives every enemy a small coroutine ("task") with a
// $222-byte state block; the routines are plain 68000 code written against a
// handful of helper subroutines. This package is a line-for-line port of that
// layer: the state block, the per-tick integrator ($15292), the steering
// helpers ($155b2, $1557e, $155d0), the animation scripts ($15c48) and the
// task routines of the enemy types, each annotated with the address of the
// original code it was ported from (AMPROG.OBJ, loaded at $EFC0).
//
// Everything that is data in the original (tables, animation scripts) is read
// out of AMPROG.OBJ at its original address.
package actor

import "encoding/binary"

const progBase = 0xEFC0

// Prog is AMPROG.OBJ addressed by its original load address.
type Prog []byte

// Word returns the big-endian word at original address a.
func (p Prog) Word(a int) uint16 {
	o := a - progBase
	if o < 0 || o+2 > len(p) {
		return 0
	}
	return binary.BigEndian.Uint16(p[o:])
}

// Byte returns the byte at original address a.
func (p Prog) Byte(a int) uint8 {
	o := a - progBase
	if o < 0 || o >= len(p) {
		return 0
	}
	return p[o]
}

// AnimScript returns the animation script words starting at a, up to and
// including the $8000 terminator (the scan in $15c48).
func (p Prog) AnimScript(a int) []uint16 {
	var ws []uint16
	for {
		w := p.Word(a)
		a += 2
		ws = append(ws, w)
		if w == 0x8000 || a-progBase >= len(p) {
			return ws
		}
	}
}

// Tables read from AMPROG.OBJ.
type tables struct {
	sin  [256]int16 // $15a42: amplitude 256 over a 256-step circle
	atan [32 * 32]uint8
}

const (
	sinTable  = 0x15A42
	atanTable = 0x1562A
)

func loadTables(p Prog) *tables {
	t := &tables{}
	for i := range t.sin {
		t.sin[i] = int16(p.Word(sinTable + 2*i))
	}
	for i := range t.atan {
		t.atan[i] = p.Byte(atanTable + i)
	}
	return t
}

// sincos is $15A2A: d1 = sin(a), d0 = cos(a), amplitude 256.
func (t *tables) sincos(a uint16) (sin, cos int32) {
	a &= 0xFF
	return int32(t.sin[a]), int32(t.sin[(a+0x40)&0xFF])
}

// aim is $155D0: the heading (0..255, 0 = +x, 64 = +y, i.e. down) from
// (sx,sy) to (tx,ty).
func (t *tables) aim(tx, ty, sx, sy int16) uint8 {
	dy := int(ty) - int(sy)
	dx := int(tx) - int(sx)
	negY, negX := false, false
	if int16(dy) < 0 {
		dy, negY = -int(int16(dy)), true
	} else {
		dy = int(int16(dy))
	}
	if int16(dx) < 0 {
		dx, negX = -int(int16(dx)), true
	} else {
		dx = int(int16(dx))
	}
	// the original halves both until both fit the 32x32 table
	for dx >= 0x20 || dy >= 0x20 {
		dx >>= 1
		dy >>= 1
	}
	a := t.atan[dy<<5+dx]
	if negX {
		a = 0x80 - a
	}
	if negY {
		a = -a
	}
	return a
}
