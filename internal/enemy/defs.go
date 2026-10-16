// Package enemy reads the enemy type definitions out of AMPROG.OBJ.
//
// Every enemy type has a small task routine; the table at $16422 maps the
// dictionary word of an object (image*2 << 8 | bank file-name index) to the
// routine's offset from $16564. The routine's first act is to call the common
// constructor at $19286 with
//
//	d0 dictionary word, d1 flags, d2 damage class,
//	d3 hit points,      d4 score, d5 enemy-count weight
//
// so the numbers are recovered by decoding the immediate loads that precede
// that call (moveq #n,dX and move.w #n,dX).
package enemy

import (
	"encoding/binary"
	"fmt"
)

const (
	progBase    = 0xEFC0
	typeTable   = 0x16422
	codeBase    = 0x16564
	constructor = 0x19286
	maxScan     = 160 // instructions to look through per routine
)

// Def describes one enemy type.
type Def struct {
	Word     uint16 // dictionary word identifying the type
	Draw     uint16 // dictionary word of the image it is drawn with (d0 of the constructor)
	Entry    int    // address of the type's task routine
	HP       int
	Score    int // points / 10 (the game shows score*10)
	Weight   int
	Damage   int // d2: damage class / hitbox byte
	Flags    int // d1
	HasHP    bool
	ParamsOK bool // the constructor call was found with all parameters
}

// Defs maps dictionary word to definition.
type Defs map[uint16]*Def

// Load decodes the type table and each routine's constructor parameters.
func Load(prog []byte) (Defs, error) {
	t := typeTable - progBase
	if t >= len(prog) {
		return nil, fmt.Errorf("enemy: AMPROG.OBJ too short")
	}
	defs := Defs{}
	for off := t; off+4 <= len(prog); off += 4 {
		id := binary.BigEndian.Uint16(prog[off:])
		if id == 0 {
			break
		}
		entry := codeBase + 2*int(binary.BigEndian.Uint16(prog[off+2:]))
		d := &Def{Word: id, Draw: id, Entry: entry}
		scan(prog, d)
		defs[id] = d
	}
	return defs, nil
}

// scan walks the routine linearly collecting register immediates until the
// constructor call.
func scan(prog []byte, d *Def) {
	var reg [8]int
	var set [8]bool
	pc := d.Entry - progBase
	for n := 0; n < maxScan && pc+2 <= len(prog); n++ {
		op := binary.BigEndian.Uint16(prog[pc:])
		switch {
		case op&0xF100 == 0x7000: // moveq #imm,dN
			r := int(op >> 9 & 7)
			reg[r], set[r] = int(int8(op&0xFF)), true
			pc += 2
		case op&0xF1FF == 0x303C: // move.w #imm,dN
			if pc+4 > len(prog) {
				return
			}
			r := int(op >> 9 & 7)
			reg[r], set[r] = int(int16(binary.BigEndian.Uint16(prog[pc+2:]))), true
			pc += 4
		case op == 0x6100: // bsr.w
			if pc+4 > len(prog) {
				return
			}
			target := progBase + pc + 2 + int(int16(binary.BigEndian.Uint16(prog[pc+2:])))
			if target == constructor {
				if set[0] {
					d.Draw = uint16(reg[0])
				}
				d.Flags, d.Damage = reg[1], reg[2]
				d.HP, d.Score, d.Weight = reg[3], reg[4], reg[5]
				d.HasHP = set[3]
				d.ParamsOK = set[3] && set[4] && set[5]
				return
			}
			pc += 4
		case op == 0x4E75: // rts
			return
		default:
			// other opcodes: step over by decoding length is not worth a
			// full disassembler; instructions here are mostly 2 or 4 bytes.
			pc += 2
		}
	}
}
