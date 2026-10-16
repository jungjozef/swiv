// Package weapon reads the player's bullet patterns out of AMPROG.OBJ.
//
// The firing routine ($17A60) picks a pattern from a table indexed by the
// craft's heading (8 directions; the helicopter always faces up, index 6) and
// the weapon level:
//
//	level 1..5   `level` bullets sharing the first record's velocity; each
//	             record is (vx, vy, dx, dy) as signed words, the dx/dy
//	             offsets placing the bullet relative to the craft. Odd levels
//	             start at record 0, even levels at record 5.
//	level 6+     eight bullets fanned out in all directions
//	             (vx, vy, dx, dy, image word) at $17D96.
//
// Bullet images are image numbers of BULLET.LIN: 8 + heading index for the
// directional pattern.
package weapon

import (
	"encoding/binary"
	"fmt"
)

const (
	progBase    = 0xEFC0
	angleTable  = 0x17B46 // 8 word offsets to per-heading pattern blocks
	fanTable    = 0x17D96 // level 6+: 8 records of 5 words
	recordsPer  = 9
	HeadingUp   = 6 // heading index of "up", the helicopter's only heading
	MaxPattern  = 5
	firstBullet = 8 // BULLET.LIN image for heading index 0
)

// Shot is one bullet of a volley.
type Shot struct {
	VX, VY int // movement per display tick
	DX, DY int // offset from the craft
	Image  int // image number in BULLET.LIN
}

// Table holds the decoded patterns.
type Table struct {
	rec [8][recordsPer][4]int
	fan [8]Shot
}

func s16(b []byte, off int) int { return int(int16(binary.BigEndian.Uint16(b[off:]))) }

// Load decodes the tables from AMPROG.OBJ.
func Load(prog []byte) (*Table, error) {
	at := angleTable - progBase
	if at+16 > len(prog) || fanTable-progBase+80 > len(prog) {
		return nil, fmt.Errorf("weapon: AMPROG.OBJ too short")
	}
	t := &Table{}
	for h := 0; h < 8; h++ {
		blk := at + int(binary.BigEndian.Uint16(prog[at+2*h:]))
		if blk+recordsPer*8 > len(prog) {
			return nil, fmt.Errorf("weapon: pattern %d outside file", h)
		}
		for r := 0; r < recordsPer; r++ {
			for k := 0; k < 4; k++ {
				t.rec[h][r][k] = s16(prog, blk+8*r+2*k)
			}
		}
	}
	for i := 0; i < 8; i++ {
		o := fanTable - progBase + 10*i
		word := int(binary.BigEndian.Uint16(prog[o+8:]))
		t.fan[i] = Shot{VX: s16(prog, o), VY: s16(prog, o+2), DX: s16(prog, o+4), DY: s16(prog, o+6),
			Image: (word >> 8) / 2}
	}
	return t, nil
}

// Volley returns the bullets fired at the given heading index and weapon
// level. With spread (weapon type $FF, the other kind of token) every bullet
// takes the velocity of its own record, so the volley fans out; otherwise all
// share the first record's.
func (t *Table) Volley(heading, level int, spread bool) []Shot {
	if level < 1 {
		return nil
	}
	if level > MaxPattern {
		return append([]Shot(nil), t.fan[:]...)
	}
	r := &t.rec[heading&7]
	start := 0
	if level%2 == 0 {
		start = 5
	}
	out := make([]Shot, 0, level)
	for i := 0; i < level; i++ {
		vx, vy := r[0][0], r[0][1]
		if spread {
			vx, vy = r[start+i][0], r[start+i][1]
		}
		out = append(out, Shot{VX: vx, VY: vy, DX: r[start+i][2], DY: r[start+i][3],
			Image: firstBullet + heading&7})
	}
	return out
}
