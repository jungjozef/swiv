package sfx

// The voice programs. Names are the addresses of the original routines; a5+0
// and a5+2 are v.a and v.b, the registers $A6 (period) and $A8 (volume) are
// v.per and v.vol, $A0/$A4 are v.lc/v.lenW, and $96(a3) = v.on().

// 16-byte waves of the original ($204060 ...).
var (
	waveTri  = w8(0x00, 0x20, 0x40, 0x60, 0x7f, 0x60, 0x40, 0x20, 0x00, 0xe0, 0xc0, 0xa0, 0x80, 0xa0, 0xc0, 0xe0)
	wave13f1 = w8(0x00, 0x10, 0x20, 0x40, 0x7f, 0x40, 0x20, 0x10, 0x00, 0xc0, 0xa0, 0x90, 0x81, 0x90, 0xa0, 0xc0)
	wave1411 = w8(0x00, 0x40, 0x7f, 0x40, 0x00, 0xc0, 0x81, 0xc0, 0x00, 0x20, 0x40, 0x20, 0x00, 0xe0, 0xc0, 0xe0)
	wave1441 = w8(0x7f, 0x80, 0x7f, 0x80, 0x7f, 0x7f, 0x80, 0x80)
	wave1451 = w8(0x00, 0x40, 0x7f, 0x40, 0x00, 0xc0, 0x80, 0xc0)
	wave1457 = w8(0x7f, 0x81)
	jingleT  = w8(0x08, 0x18, 0x28, 0x38, 0xc8, 0xd8, 0xe8, 0xf8, 0x08, 0x18, 0x28, 0x38, 0xc8, 0xd8, 0xe8, 0xf8)
)

// sampled plays a sampled sound once ($13C32 with BIGEXPL, $13C92 with SMART):
// the period is d1; after two frames the channel is pointed at a silent word
// so that it stops when the sample has played.
func sampled(pick func(e *Engine) []int8) program {
	return func(v *voice) {
		v.lc = pick(v.e)
		v.lenW = len(v.lc) / 2
		v.vol = 0x40
		v.per = int(v.a)
		v.on()
		v.y(2)
		v.lc, v.lenW = []int8{0, 0}, 1
		v.wait(0xFFFF)
	}
}

var (
	prog13c32 = sampled(func(e *Engine) []int8 { return e.bigexpl })
	prog13c92 = sampled(func(e *Engine) []int8 { return e.smart })
)

// prog13cc8 ($13CB8): a falling-in blip with a wobbling pitch.
func prog13cc8(v *voice) {
	v.wave(waveTri)
	v.setBuf(8)
	v.on()
	v.b = 0x30
	for {
		v.vol = int(v.b)
		d := int(v.b)
		if d&4 != 0 {
			d = ^d
		}
		v.per = int(v.a) + (d&3)<<5
		v.yield()
		v.a += 3
		v.b--
		if v.b == 0 {
			return
		}
	}
}

// prog13d3a ($13D2A): the pitch rises in a quick sweep.
func prog13d3a(v *voice) {
	v.wave(waveTri)
	v.setBuf(8)
	v.vol = 0x30
	v.on()
	for {
		v.per = int(v.a)
		v.yield()
		v.a -= 0x14
		if v.a < 0x1F4 {
			return
		}
	}
}

// prog13d9a ($13D86): a warble that fades out.
func prog13d9a(v *voice) {
	v.wave(waveTri)
	v.setBuf(8)
	v.vol = 0x30
	v.on()
	for {
		v.vol = int(v.b)
		v.per = int(v.a)
		v.yield()
		v.a ^= 0x40
		v.a -= 2
		v.b--
		if v.b == 0 {
			return
		}
	}
}

// prog13e1e ($13DEE, $13E06): a burst of noise whose pitch falls.
func prog13e1e(v *voice) {
	v.setBuf(0xC)
	v.on()
	r := v.e.rand()
	v.buf[0], v.buf[1], v.buf[2], v.buf[3] = int8(r>>24), int8(r>>16), int8(r>>8), int8(r)
	for {
		v.noise(6)
		v.vol = int(v.a)
		d := int(v.b)
		v.per = d
		v.b += int16(d >> 5)
		v.yield()
		v.a--
		if v.a == 0 {
			return
		}
	}
}

// prog13f10 ($13EFE).
func prog13f10(v *voice) {
	v.wave(wave13f1)
	v.setBuf(8)
	v.on()
	for {
		v.vol = int(v.a)
		d := int(v.b)
		v.per = d
		v.b += int16(d >> 4)
		v.yield()
		v.a--
		if v.a == 0 {
			return
		}
	}
}

// prog13fce ($13FBE): fading with a stepped vibrato.
func prog13fce(v *voice) {
	v.wave(waveTri)
	v.setBuf(8)
	v.on()
	v.b = 0x30
	for {
		v.vol = int(v.b)
		d := int(v.b)
		if d&4 != 0 {
			d = ^d
		}
		v.per = int(v.a) + (d&3)*4
		v.yield()
		v.b--
		if v.b == 0 {
			return
		}
	}
}

// prog14042 ($14030, the hit sound): the pitch drops quickly.
func prog14042(v *voice) {
	v.wave(waveTri)
	v.setBuf(8)
	v.on()
	for {
		v.vol = int(v.a)
		d := int(v.b)
		v.per = d
		v.b += int16(d >> 2)
		v.yield()
		v.a -= 4
		if v.a == 0 {
			return
		}
	}
}

// prog1409e ($14090): noise that swells and fades.
func prog1409e(v *voice) {
	v.setBuf(0x10)
	v.per = 0x1F4
	v.on()
	vol := 0
	for {
		v.noise(8)
		v.vol = vol
		v.yield()
		vol += 2
		if vol >= int(v.a) {
			break
		}
	}
	v.per = 0x3E8
	for {
		v.noise(8)
		v.vol = vol
		v.y(2)
		vol--
		if vol == 0 {
			return
		}
	}
}

// prog14112 ($140F8): a long sweep down, a hold and a fade.
func prog14112(v *voice) {
	v.wave(wave1411)
	v.setBuf(8)
	v.vol = 0x30
	v.on()
	d := int(v.b) * 4 & 0xFFFF
	for {
		v.per = d
		d -= d >> 5
		v.yield()
		if d <= int(v.b) {
			break
		}
	}
	v.wait(int(v.a))
	vol := 0x30
	for {
		v.per = int(v.b)
		v.b += v.b >> 4
		v.vol = vol
		v.y(4)
		vol--
		if vol == 0 {
			return
		}
	}
}

// prog141b4 ($14194): a chord of noise that swells.
func prog141b4(v *voice) {
	v.setBuf(8)
	v.vol = 0
	v.per = int(v.a)
	v.on()
	v.b = 0
	for {
		v.noise(4)
		v.vol = int(v.b) >> 7
		v.b++
		v.yield()
	}
}

// prog1425c ($1424A): a short noise hiss.
func prog1425c(v *voice) {
	v.setBuf(0x10)
	for {
		v.noise(8)
		v.per = int(v.b)
		v.b--
		v.vol = int(v.a)
		v.on()
		v.yield()
		v.a--
		if v.a == 0 {
			return
		}
	}
}

// prog142a8 ($14298): a noise swell that dies away.
func prog142a8(v *voice) {
	v.setBuf(0x20)
	v.on()
	v.a = 0
	for {
		v.noise(0x10)
		v.per = int(v.b)
		v.vol = int(v.a) >> 3
		v.y(2)
		v.a += 0x10
		if v.a >= 0x200 {
			break
		}
	}
	for {
		v.noise(0x10)
		v.per = int(v.b)
		v.b += 2
		v.vol = int(v.a) >> 3
		v.y(2)
		v.a -= 3
		if v.a < 0 {
			return
		}
	}
}

// prog1432e ($14310): a long buzz of a sine with random pitch.
func prog1432e(v *voice) {
	copy(v.buf[:128], v.e.sine[:])
	v.setBuf(0x40)
	v.on()
	for {
		d := v.e.rand()&0x7F + 0x7F
		v.per = d
		v.vol = int(v.a)
		v.y(4)
		v.a--
		if v.a == 0 {
			return
		}
	}
}

// prog14392 ($1437E, an enemy shot): a noise pop whose pitch falls away.
func prog14392(v *voice) {
	v.setBuf(6)
	v.on()
	for i := 0; i < 12; i++ {
		v.buf[i] = 0
	}
	for {
		v.noise(2)
		v.per = int(v.a)
		v.a += v.a >> 3
		d := int(v.b)
		v.vol = d
		v.lenW = d&3 + 1
		v.yield()
		v.b -= 2
		if v.b == 0 {
			return
		}
	}
}

// prog14416 / prog1440e ($143DE, $143F6): a warbling square tone; the second
// voice starts eight frames later.
func prog1440e(v *voice) {
	v.y(8)
	prog14416(v)
}

func prog14416(v *voice) {
	v.wave(wave1441)
	v.setBuf(4)
	v.on()
	n := 0x7E
	if v.a == 0x32 {
		n = 0x30
	}
	for {
		v.vol = n
		d := int(v.b)
		v.per = d + int(v.a)
		v.b += int16(d >> 5)
		v.a = -v.a
		v.yield()
		n--
		if n == 0 {
			return
		}
	}
}

// prog14488 ($1446C): a long sweep of the pitch with a slowly fading vibrato.
func prog14488(v *voice) {
	v.wave(waveTri)
	v.setBuf(8)
	v.on()
	v.vol = 0x40
	n := 0x60
	for {
		for {
			v.per = int(uint16(v.a))
			v.a -= v.a >> 8
			v.yield()
			if int16(v.a) <= 0x5DC {
				break
			}
		}
		for {
			v.per = int(uint16(v.a))
			v.a += v.a >> 8
			v.yield()
			if int16(v.a) >= 0x640 {
				break
			}
		}
		v.vol = n
		n -= 3
		if n < 0 {
			return
		}
	}
}

// prog14516 ($144FA): a narrowing warble.
func prog14516(v *voice) {
	v.wave(wave1451)
	v.setBuf(4)
	v.vol = 0x20
	v.on()
	for {
		v.vol = int(v.a)
		v.per = int(v.a)*4 + int(v.b)
		v.y(3)
		v.a = -v.a
		v.per = int(v.a)*4 + int(v.b)
		v.y(3)
		v.a = -v.a
		v.a--
		if v.a == 0 {
			return
		}
	}
}

// prog1457c ($14570): three quick falling notes.
func prog1457c(v *voice) {
	v.wave(wave1457)
	v.setBuf(1)
	v.vol = 0x40
	v.on()
	v.per = 0x12C
	v.y(2)
	v.per = 0x1F4
	v.y(2)
	v.per = 0x2BC
	v.y(2)
}

// progNote ($14632): one note of a jingle, a plucked tone with a changing
// timbre.
func progNote(v *voice) {
	v.setBuf(4)
	v.per = int(v.b)
	v.vol = 0x40
	v.on()
	v.a = 0x40
	for {
		v.vol = int(v.a)
		off := int(v.a) & 0x1F >> 2
		for i := 0; i < 8; i++ {
			v.buf[i] = jingleT[i] - jingleT[i+off]
		}
		v.y(2)
		v.a--
		if v.a == 0 {
			return
		}
	}
}
