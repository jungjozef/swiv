package sfx

// program is a voice program: it runs inside the voice's goroutine and calls
// v.yield (the original's $13AA0) to wait for the next frame.
type program func(v *voice)

type cancelled struct{}

type run struct {
	resume   chan struct{}
	done     chan struct{}
	cancel   bool
	finished bool
}

// voice is one of the four sound voices: the state block at $2A22 + n * $10C
// and the Paula channel it drives.
type voice struct {
	e   *Engine
	idx int

	prio int   // +4: frames of priority left
	a, b int16 // +0 and +2: the parameters (d1, d2) and the program's variables
	buf  [256]int8

	// the channel registers
	lc      []int8
	lenW    int
	per     int
	vol     int
	dma     bool
	latched []int8
	pos     float64

	cur *run
}

// start is $13B8A: take the voice, stop its channel and give it a program
// that runs from the next frame on.
func (v *voice) start(prio, d1, d2 int, prog program) {
	v.stop()
	v.prio = prio
	v.a, v.b = int16(d1), int16(d2)
	v.dma = false // the channel is switched off
	r := &run{resume: make(chan struct{}), done: make(chan struct{})}
	v.cur = r
	go func() {
		defer func() {
			if x := recover(); x != nil {
				if _, ok := x.(cancelled); !ok {
					panic(x)
				}
			}
			r.finished = true
			r.done <- struct{}{}
		}()
		<-r.resume
		if r.cancel {
			panic(cancelled{})
		}
		v.yield() // every program starts with bsr $13AA0
		prog(v)
		v.end()
	}()
}

// stop abandons the running program.
func (v *voice) stop() {
	r := v.cur
	if r == nil || r.finished {
		return
	}
	r.cancel = true
	r.resume <- struct{}{}
	<-r.done
}

// run is one interrupt's worth of the voice: until its program yields.
func (v *voice) run() {
	r := v.cur
	if r == nil || r.finished {
		// the end loop of $13BB2: channel off, priority zero
		v.dma, v.prio, v.vol = false, 0, 0
		return
	}
	r.resume <- struct{}{}
	<-r.done
}

// yield is $13AA0: wait for the next frame; the priority runs down.
func (v *voice) yield() {
	r := v.cur
	if v.prio > 0 {
		v.prio--
	}
	r.done <- struct{}{}
	<-r.resume
	if r.cancel {
		panic(cancelled{})
	}
}

// y waits n frames ($13A74: 4, $13A76: 3, $13A78: 2, $13A7A: 1).
func (v *voice) y(n int) {
	for ; n > 0; n-- {
		v.yield()
	}
}

// wait is $14696: n frames (a 16-bit count, none when zero).
func (v *voice) wait(n int) {
	for n &= 0xFFFF; n != 0; n-- {
		v.yield()
	}
}

// end is $13BB2: the channel is switched off, the priority is freed.
func (v *voice) end() {
	v.dma, v.prio, v.vol = false, 0, 0
}

// on switches the channel's DMA on ($96): the registers are latched.
func (v *voice) on() {
	if v.dma {
		return
	}
	v.dma = true
	v.latch()
	v.pos = 0
}

func (v *voice) latch() {
	n := v.lenW * 2
	if n > len(v.lc) {
		n = len(v.lc)
	}
	v.latched = v.lc[:n]
}

// setBuf points the channel at the voice buffer with n words.
func (v *voice) setBuf(words int) {
	v.lc = v.buf[:]
	v.lenW = words
}

// sample is the channel's contribution to one output sample.
func (v *voice) sample() int {
	if !v.dma || len(v.latched) == 0 {
		return 0
	}
	per := v.per & 0xFFFF
	if per < 113 {
		per = 113
	}
	v.pos += palClock / float64(per) / SampleRate
	for int(v.pos) >= len(v.latched) {
		v.pos -= float64(len(v.latched))
		v.latch() // the loop restarts from the registers
		if len(v.latched) == 0 {
			return 0
		}
	}
	vol := v.vol
	if vol > 64 {
		vol = 64
	}
	if vol < 0 {
		vol = 0
	}
	return int(v.latched[int(v.pos)]) * vol / 64
}

// noise is $146A6: n+1 longs of the shift-register noise written over the
// start of the buffer, seeded from its first long.
func (v *voice) noise(n int) {
	d6 := ^(uint32(uint8(v.buf[0]))<<24 | uint32(uint8(v.buf[1]))<<16 | uint32(uint8(v.buf[2]))<<8 | uint32(uint8(v.buf[3])))
	const d7 = 0x1D872B41
	for i := 0; i <= n; i++ {
		carry := d6 >> 31
		d6 <<= 1
		if !(carry == 0 && d6 != 0) { // bhi not taken
			d6 ^= d7
		}
		d6 = d6<<16 | d6>>16
		v.buf[4*i] = int8(d6 >> 24)
		v.buf[4*i+1] = int8(d6 >> 16)
		v.buf[4*i+2] = int8(d6 >> 8)
		v.buf[4*i+3] = int8(d6)
	}
}

// wave copies bytes into the buffer.
func (v *voice) wave(b []int8) { copy(v.buf[:], b) }

func w8(b ...byte) []int8 {
	out := make([]int8, len(b))
	for i, c := range b {
		out[i] = int8(c)
	}
	return out
}
