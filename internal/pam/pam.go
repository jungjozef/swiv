// Package pam decodes SWIV level scripts (.PAM files).
//
// A script is a stream of big-endian 32-bit records. The file has no
// terminator on disk: the game's loader appends a zero long after reading it,
// and a zero long (or the end of the file) ends the script. Each record carries a distance in scroll rows since the previous
// one; the level scroll counter starts at StartScroll and counts down, so
// later records have smaller scroll values and appear nearer the top.
//
//	bit 31 = 1   palette command
//	    bits 16-23   scroll delta
//	    bits  4-15   RGB444 colour
//	    bits  0-3    colour index
//	bit 31 = 0   object
//	    bits  0-3    kind (0 = scenery/air object, others not yet understood)
//	    bits  4-11   type (index into the level dictionary)
//	    bits 12-19   scroll delta
//	    bits 20-30   position, an 11-bit signed value (the game sign-extends
//	                 bit 30). Values <= -92 get +$800, then $200 is
//	                 subtracted while the value is >= $1A0; x is the result
//	                 and the layer is z = 4 - (number of subtractions), so
//	                 small negative x (-92..-1) stays on layer 4.
package pam

import (
	"encoding/binary"
	"fmt"
)

// StartScroll is the scroll counter value at which every level's script begins.
const StartScroll = 0xE9C0

// Command sets one colour of the 16-colour playfield palette.
type Command struct {
	Scroll int    // scroll value at which it takes effect
	Index  int    // colour register 0..15
	RGB    uint16 // 0x0RGB, 4 bits per channel
}

// Object places one image of the level dictionary.
type Object struct {
	Scroll  int // y in scroll units
	Kind    int
	Type    int
	X       int // signed; may be slightly outside 0..319
	Z       int // layer: 4 = back (ground) ... 0 = front
	Ordinal int // position of the record among the objects
	// Prev is the y of the preceding record of any kind (the game's loader
	// reads a record once that one has come within reach).
	Prev int
	// Raw is the unprocessed 12-bit position field. Non-zero-kind objects
	// (enemies) use its low 9 bits as an unsigned x.
	Raw int
}

// Script is a decoded .PAM file.
type Script struct {
	Commands  []Command
	Objects   []Object
	EndScroll int // scroll value of the terminating record
}

// Parse decodes a .PAM file whose first record is relative to StartScroll.
func Parse(b []byte) (*Script, error) { return ParseAt(b, StartScroll) }

// ParseAt decodes a .PAM file whose first record is relative to scroll value
// start: the game chains the levels, each script carrying on where the one
// before it ended.
func ParseAt(b []byte, start int) (*Script, error) {
	if len(b) < 4 || len(b)%4 != 0 {
		return nil, fmt.Errorf("pam: bad length %d", len(b))
	}
	s := &Script{}
	scroll := start
	for i := 0; i+4 <= len(b); i += 4 {
		v := binary.BigEndian.Uint32(b[i:])
		if v == 0 {
			s.EndScroll = scroll
			return s, nil
		}
		if v&0x80000000 != 0 {
			scroll -= int(v >> 16 & 0xFF)
			s.Commands = append(s.Commands, Command{
				Scroll: scroll,
				Index:  int(v & 0xF),
				RGB:    uint16(v >> 4 & 0xFFF),
			})
			continue
		}
		raw := int(v >> 20 & 0xFFF)
		pos := int(v >> 20 & 0x7FF)
		if pos&0x400 != 0 {
			pos -= 0x800 // sign-extend the 11-bit field
		}
		if pos <= -92 {
			pos += 0x800
		}
		k := 0
		for pos >= 0x1A0 && k < 4 {
			pos -= 0x200
			k++
		}
		prev := scroll
		scroll -= int(v >> 12 & 0xFF)
		s.Objects = append(s.Objects, Object{
			Prev:    prev,
			Scroll:  scroll,
			Kind:    int(v & 0xF),
			Type:    int(v >> 4 & 0xFF),
			X:       pos,
			Z:       4 - k,
			Ordinal: len(s.Objects),
			Raw:     raw,
		})
	}
	s.EndScroll = scroll
	return s, nil
}
