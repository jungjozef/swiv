// Package level loads a SWIV level from the disk image and renders any
// scroll position of it in software.
//
// Everything comes from the original files: the level table and dictionaries
// are read out of AMPROG.OBJ, the script from the level's .PAM and the
// pictures from the .LIN banks it names.
package level

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"sort"

	"github.com/jungjozef/swiv/internal/adf"
	"github.com/jungjozef/swiv/internal/lin"
	"github.com/jungjozef/swiv/internal/pam"
)

// Addresses inside AMPROG.OBJ (loaded at progBase by the original kernel).
const (
	progBase   = 0xEFC0
	nameTable  = 0xEFC4  // NUL-terminated file names; file-name index -> name
	levelTable = 0x1280C // 6 bytes per level: name index, dictionary offset, parameter
	numLevels  = 7

	// ScreenW and ScreenH are the playfield window the game shows.
	ScreenW = 320
	ScreenH = 256

	// StartTop is the scroll value of the screen's top row when a level begins.
	StartTop = 0xE860
)

// Names of the seven levels, in table order.
var Names = [numLevels]string{"town", "desert", "grass", "river", "ice", "scifi", "final"}

type strip struct {
	w, h   int
	hx, hy int
	key    uint8
	px     []uint8
}

type picture struct {
	strips     []strip
	yMin, yMax int  // vertical extent relative to the object's position
	hidden     bool // marker frames (header flag 0x10): the game never draws them
}

// Spawn is a non-zero-kind object: an enemy or other active object. Word is
// its dictionary word (image*2 << 8 | bank), which names the enemy type.
type Spawn struct {
	Scroll int // y, in scroll units
	Prev   int // y of the record before it in the script (when it gets loaded)
	X      int // placed x, 0..511
	Kind   int
	Word   uint16
	Bank   string
	Image  int
}

// Level is a loaded level.
type Level struct {
	Spawns []Spawn // active objects, in script order

	Name   string
	Script *pam.Script
	EndTop int // scroll value of the last screen top
	// Ends lists the scroll value at which each chained level's script ends.
	Ends      []int
	Skipped   int // objects of kinds this package does not draw yet
	objects   []placed
	palettes  [][16]color.RGBA // state after n commands; [0] is all black
	cmdScroll []int            // scroll value of each command, descending

	cov, tcov []bool
	tmp       []uint8
}

// placed is a scenery object with its picture.
type placed struct {
	pam.Object
	pic *picture
}

// Load reads level n (0 = town ... 6 = final) from the disk.
func Load(d *adf.Disk, n int) (*Level, error) {
	return loadSegment(d, n, pam.StartScroll, nil)
}

// Chain reads levels first .. first+count-1 as the game plays them: every
// level's script carries on where the one before it ended, with its own
// dictionary and pictures, and the scenery, the palette and the enemy list
// simply continue.
func Chain(d *adf.Disk, first, count int) (*Level, error) {
	var all *Level
	for n := first; n < first+count && n < numLevels; n++ {
		start := pam.StartScroll
		if all != nil {
			start = all.EndTop
		}
		seg, err := loadSegment(d, n, start, all)
		if err != nil {
			return nil, err
		}
		all = seg
	}
	if all == nil {
		return nil, fmt.Errorf("level: no level %d", first)
	}
	return all, nil
}

// loadSegment reads level n with its script starting at scroll value start;
// when prev is not nil the result continues it.
func loadSegment(d *adf.Disk, n int, start int, prev *Level) (*Level, error) {
	if n < 0 || n >= numLevels {
		return nil, fmt.Errorf("level: no level %d", n)
	}
	prog, err := d.ReadName("AMPROG.OBJ")
	if err != nil {
		return nil, err
	}
	names := fileNames(prog)

	ent := levelTable - progBase + 6*n
	if ent+6 > len(prog) {
		return nil, fmt.Errorf("level: AMPROG.OBJ too short")
	}
	nameIdx := int(binary.BigEndian.Uint16(prog[ent:]))
	dictAddr := levelTable + int(binary.BigEndian.Uint16(prog[ent+2:]))
	if nameIdx >= len(names) {
		return nil, fmt.Errorf("level %d: bad file-name index %d", n, nameIdx)
	}

	raw, err := d.ReadName(names[nameIdx])
	if err != nil {
		return nil, err
	}
	script, err := pam.ParseAt(raw, start)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", names[nameIdx], err)
	}

	l := &Level{Name: Names[n], Script: script}
	pics := map[int]*picture{}

	// Dictionary: one u16 per type (frame*2, bank name index).
	maxType := 0
	for _, o := range script.Objects {
		if o.Type > maxType {
			maxType = o.Type
		}
	}
	dictOff := dictAddr - progBase
	if dictOff < 0 || dictOff+2*(maxType+1) > len(prog) {
		return nil, fmt.Errorf("level %d: dictionary outside AMPROG.OBJ", n)
	}

	banks := map[string]*lin.Bank{}
	used := map[int]bool{}
	for _, o := range script.Objects {
		if o.Kind == 0 {
			used[o.Type] = true
		} else {
			l.Skipped++
			a := int(prog[dictOff+2*o.Type])
			bank := int(prog[dictOff+2*o.Type+1])
			sp := Spawn{Scroll: o.Scroll, Prev: o.Prev, X: o.Raw & 0x1FF, Kind: o.Kind, Word: uint16(a<<8 | bank), Image: a / 2}
			if bank < len(names) {
				sp.Bank = names[bank]
			}
			l.Spawns = append(l.Spawns, sp)
		}
	}
	for typ := range used {
		a := int(prog[dictOff+2*typ])
		bank := int(prog[dictOff+2*typ+1])
		if bank >= len(names) {
			return nil, fmt.Errorf("level %d type %d: bad bank index %d", n, typ, bank)
		}
		name := names[bank]
		b, ok := banks[name]
		if !ok {
			data, err := d.ReadName(name)
			if err != nil {
				return nil, fmt.Errorf("level %d bank %s: %w", n, name, err)
			}
			if b, err = lin.Parse(data); err != nil {
				return nil, fmt.Errorf("bank %s: %w", name, err)
			}
			banks[name] = b
		}
		img := a / 2
		if img >= len(b.Images) {
			return nil, fmt.Errorf("level %d type %d: image %d not in %s (%d images)", n, typ, img, name, len(b.Images))
		}
		pics[typ] = makePicture(b, img)
	}

	// Draw order: back to front by layer; within a layer the newest first.
	for _, o := range script.Objects {
		if o.Kind == 0 {
			l.objects = append(l.objects, placed{o, pics[o.Type]})
		}
	}

	// Palette states after each command (commands arrive in descending scroll).
	var cur [16]color.RGBA
	for i := range cur {
		cur[i] = color.RGBA{A: 0xFF}
	}
	if prev != nil {
		cur = prev.palettes[len(prev.palettes)-1]
	}
	l.palettes = append(l.palettes, cur)
	for _, c := range script.Commands {
		cur[c.Index] = color.RGBA{
			R: uint8(c.RGB>>8&0xF) * 17,
			G: uint8(c.RGB>>4&0xF) * 17,
			B: uint8(c.RGB&0xF) * 17,
			A: 0xFF,
		}
		l.palettes = append(l.palettes, cur)
		l.cmdScroll = append(l.cmdScroll, c.Scroll)
	}
	l.EndTop = script.EndScroll
	l.Ends = []int{script.EndScroll}
	if prev != nil {
		l.Ends = append(append([]int(nil), prev.Ends...), script.EndScroll)
		// the segment continues the course: its scenery, palette commands and
		// enemies come after the previous ones
		l.objects = append(append([]placed(nil), prev.objects...), l.objects...)
		l.palettes = append(append([][16]color.RGBA(nil), prev.palettes...), l.palettes[1:]...)
		l.cmdScroll = append(append([]int(nil), prev.cmdScroll...), l.cmdScroll...)
		l.Spawns = append(append([]Spawn(nil), prev.Spawns...), l.Spawns...)
		l.Skipped += prev.Skipped
		l.Name = prev.Name + "+" + l.Name
		l.Script = prev.Script
	}
	sort.SliceStable(l.objects, func(i, j int) bool {
		a, b := l.objects[i], l.objects[j]
		if a.Z != b.Z {
			return a.Z > b.Z
		}
		return a.Ordinal > b.Ordinal
	})
	return l, nil
}

func makePicture(b *lin.Bank, img int) *picture {
	p := &picture{}
	for k, fi := range b.Images[img] {
		f := &b.Frames[fi]
		if f.Flags&0x10 != 0 {
			p.hidden = true
		}
		p.strips = append(p.strips, strip{w: f.W, h: f.H, hx: f.HotX, hy: f.HotY, key: f.Key, px: f.Indices()})
		y0, y1 := -f.HotY, -f.HotY+f.H
		if k == 0 || y0 < p.yMin {
			p.yMin = y0
		}
		if k == 0 || y1 > p.yMax {
			p.yMax = y1
		}
	}
	return p
}

// fileNames reads the NUL-terminated name table out of AMPROG.OBJ.
func fileNames(prog []byte) []string {
	var names []string
	for p := nameTable - progBase; p < len(prog); {
		e := p
		for e < len(prog) && prog[e] != 0 {
			e++
		}
		s := prog[p:e]
		if len(s) < 3 || len(s) > 20 {
			break
		}
		ok := true
		for _, c := range s {
			if c < 0x20 || c > 0x7E {
				ok = false
			}
		}
		if !ok {
			break
		}
		names = append(names, string(s))
		p = e + 1
	}
	return names
}

// copperLag is how many rows later than a command's scroll value its colours
// take effect: the original's copper wait lands two lines below the nominal
// position (measured against the running game, rows with L = S and L = S-1
// still show the old colours).
const copperLag = 2

// paletteAt returns the palette in effect for the screen row whose scroll
// value is v: the state after every command with Scroll >= v + copperLag.
func (l *Level) paletteAt(v int) *[16]color.RGBA {
	v += copperLag
	n := sort.Search(len(l.cmdScroll), func(i int) bool { return l.cmdScroll[i] < v })
	return &l.palettes[n]
}

// Draw paints the 320x256 window whose top row has scroll value top into idx
// as colour indices 0..15 (ScreenW*ScreenH entries).
//
// The display buffer is a ring of 320 rows that is only drawn where scenery
// objects land: a row nothing covers still holds what was drawn there when it
// was 320 rows further down the level. Draw reproduces that, so the gap
// between one level's last scenery and the next one's first still shows
// ground.
func (l *Level) Draw(top int, idx []uint8) {
	clear(idx)
	if len(l.cov) != len(idx) {
		l.cov = make([]bool, len(idx))
		l.tmp = make([]uint8, len(idx))
		l.tcov = make([]bool, len(idx))
	}
	clear(l.cov)
	l.paint(top, idx, l.cov)
	for k := 1; k <= 3; k++ {
		missing := false
		for _, c := range l.cov {
			if !c {
				missing = true
				break
			}
		}
		if !missing {
			return
		}
		clear(l.tmp)
		clear(l.tcov)
		l.paint(top+RingRows*k, l.tmp, l.tcov)
		for i := range l.cov {
			if !l.cov[i] && l.tcov[i] {
				idx[i], l.cov[i] = l.tmp[i], true
			}
		}
	}
}

// RingRows is the height of the display buffer's ring.
const RingRows = 320

// paint draws the objects into idx and marks the pixels they cover.
func (l *Level) paint(top int, idx []uint8, cov []bool) {
	for _, o := range l.objects {
		p := o.pic
		y := o.Scroll - top
		if p.hidden || y+p.yMax <= 0 || y+p.yMin >= ScreenH {
			continue
		}
		for si := range p.strips {
			s := &p.strips[si]
			x0, y0 := o.X-s.hx, y-s.hy
			for sy := 0; sy < s.h; sy++ {
				py := y0 + sy
				if py < 0 || py >= ScreenH {
					continue
				}
				row := s.px[sy*s.w : (sy+1)*s.w]
				out := idx[py*ScreenW : (py+1)*ScreenW]
				for sx, c := range row {
					px := x0 + sx
					if c == s.key || px < 0 || px >= ScreenW {
						continue
					}
					out[px] = c
					cov[py*ScreenW+px] = true
				}
			}
		}
	}
}

// Colourise converts an index buffer for the window starting at scroll value
// top to RGBA, applying the level's per-scanline palette.
func (l *Level) Colourise(top int, idx []uint8, dst *image.RGBA) {
	for y := 0; y < ScreenH; y++ {
		pal := l.paletteAt(top + y)
		o := y * dst.Stride
		for x := 0; x < ScreenW; x++ {
			c := pal[idx[y*ScreenW+x]&15]
			dst.Pix[o+4*x], dst.Pix[o+4*x+1], dst.Pix[o+4*x+2], dst.Pix[o+4*x+3] = c.R, c.G, c.B, 0xFF
		}
	}
}

// Render draws the window straight to RGBA (scenery only).
func (l *Level) Render(top int, dst *image.RGBA) {
	var idx [ScreenW * ScreenH]uint8
	l.Draw(top, idx[:])
	l.Colourise(top, idx[:], dst)
}
