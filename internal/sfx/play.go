package sfx

// A jingle is the little task of $1460E: one voice per note, five frames
// apart, until a zero note.
type jingle struct {
	right bool
	notes []int
	wait  int
}

// step runs the jingle for one frame; false when it has ended.
func (j *jingle) step(e *Engine) bool {
	if j.wait > 0 {
		j.wait--
		return true
	}
	if len(j.notes) == 0 || j.notes[0] == 0 {
		return false
	}
	// $1460E: priority $78, d1 = $40, the period of the note in d2
	e.alloc(j.right, 0x78, 0x40, j.notes[0], progNote)
	j.notes = j.notes[1:]
	j.wait = 5 // jsr -$592(a6)
	return true
}

var (
	notes145c6 = []int{0x1A8, 0x150, 0x10A, 0xD4, 0xA8, 0x85, 0}
	notes14602 = []int{0x9F, 0xD4, 0x9F, 0x8D, 0}
)

// The sounds, named by the address of their entry in the original. x is the
// d0 the caller passes (the horizontal position, which pans the sound).
const (
	BigExplosion   = 0x13BDC // four voices of BIGEXPL: the helicopter's death
	SmallExplosion = 0x13BFC // two voices of BIGEXPL
	Explosion      = 0x13C18 // one voice of BIGEXPL at a random pitch
	SmartBomb      = 0x13C72 // four voices of SMART
	Blip           = 0x13CB8
	Sweep          = 0x13D2A
	Warble         = 0x13D86
	NoiseL         = 0x13DEE // noise on both sides
	NoiseR         = 0x13E06
	Whoosh         = 0x13EFE
	Hum            = 0x13FBE
	Hit            = 0x14030 // an enemy hit but not destroyed
	Swell          = 0x14090
	Popup          = 0x140F8
	WindUp         = 0x14194
	Hiss           = 0x1424A
	Rumble         = 0x14298
	Buzz           = 0x14310
	Shot           = 0x1437E // an enemy shot
	Chirp          = 0x143DE
	ChirpLong      = 0x143F6
	Siren          = 0x1446C
	Chorus         = 0x144FA
	Notes3         = 0x14570
	Jingle1        = 0x145C0 // the extra life
	Jingle2        = 0x145D4 // a token picked up
	// the two entries that only forward
	Chorus2 = 0x177F8
	NoiseB  = 0x177F4
)

// Play starts a sound. It is safe to call from the game's goroutine.
func (e *Engine) Play(id int, x int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	right := byX(x)
	switch id {
	case BigExplosion:
		for _, d1 := range []int{0x400, 0x408, 0x480, 0x488} {
			e.alloc(true, 0x64, d1, 0, prog13c32)
		}
	case SmallExplosion:
		e.alloc(true, 0x3C, 0x300, 0, prog13c32)
		e.alloc(true, 0x3C, 0x301+e.rand()&7, 0, prog13c32)
	case Explosion:
		e.alloc(right, 0x32, 0x250+e.rand()&0x1F, 0, prog13c32)
	case SmartBomb:
		for _, d1 := range []int{0x410, 0x401, 0x3F2, 0x3E4} {
			e.alloc(true, 0x7F, d1, 0, prog13c92)
		}
	case Blip:
		e.alloc(right, 0x1E, 0xFA, 0, prog13cc8)
	case Sweep:
		e.alloc(right, 0x28, 0x320, 0, prog13d3a)
	case Warble:
		e.alloc(right, 0x0A, 0x5DC, 0x40, prog13d9a)
	case NoiseL:
		e.alloc(false, 0x28, 0x40, 0x190, prog13e1e)
		e.alloc(true, 0x28, 0x40, 0x190, prog13e1e)
	case NoiseR, NoiseB:
		e.alloc(false, 0x28, 0x40, 0x96, prog13e1e)
		e.alloc(true, 0x28, 0x40, 0x96, prog13e1e)
	case Whoosh:
		e.alloc(right, 0x14, 0x20, 0x3E8, prog13f10)
	case Hum:
		e.alloc(right, 0x3C, 0x96, 0, prog13fce)
	case Hit:
		e.alloc(right, 0x14, 0x40, 0xC8, prog14042)
	case Swell:
		e.alloc(right, 0x28, 0x40, 0, prog1409e)
	case Popup:
		e.alloc(right, 0x50, 0x32, 0x9C4, prog14112)
		e.alloc(right, 0x50, 0x5A, 0x8B3, prog14112)
	case WindUp:
		for _, d1 := range []int{0x190, 0x1E0, 0x19D, 0x1B9} {
			e.alloc(false, 0x7F, d1, 0, prog141b4)
		}
	case Hiss:
		e.alloc(right, 0x50, 0x40, 0x140, prog1425c)
	case Rumble:
		e.alloc(right, 0x46, 0, 0x12C, prog142a8)
	case Buzz:
		e.alloc(right, 0xC8, 0x7F, 0, prog1432e)
		e.alloc(false, 0x3C, 0x7F, 0, prog1432e)
		e.alloc(true, 0x3C, 0x7F, 0, prog1432e)
	case Shot:
		e.alloc(right, 0x32, 0x100, 0x60, prog14392)
	case Chirp:
		e.alloc(right, 0x64, 0x32, 0x1F4, prog14416)
		e.alloc(right, 0x64, 0x32, 0x1F4, prog1440e)
	case ChirpLong:
		e.alloc(right, 0x64, 0x14, 0xC8, prog14416)
		e.alloc(right, 0x64, 0x14, 0xC8, prog1440e)
	case Siren:
		e.alloc(false, 0x2710, 0x2710, 0, prog14488)
		e.alloc(true, 0x2710, 0x2AF8, 0, prog14488)
	case Chorus, Chorus2:
		e.alloc(true, 0x64, 0x20, 0xC8, prog14516)
		e.alloc(false, 0x64, 0x20, 0xCA, prog14516)
	case Notes3:
		e.alloc(right, 0x14, 0, 0, prog1457c)
	case Jingle1:
		e.jingles = append(e.jingles, &jingle{right: right, notes: notes145c6})
	case Jingle2:
		e.jingles = append(e.jingles, &jingle{right: right, notes: notes14602})
	}
}
