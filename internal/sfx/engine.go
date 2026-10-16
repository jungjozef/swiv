// Package sfx is the sound-effect player of SWIV, ported from the Amiga
// program ($13A60-$14720).
//
// The original drives the four Paula channels with a tiny scheduler: every
// interrupt of CIA-B timer A (about 205 times a second, not the 50 Hz of the
// picture; the interrupt at $13A7C) it resumes each voice in turn; a voice is a coroutine with its own stack whose program ($13C32,
// $13CC8, ...) writes the channel registers, builds its waveform in a
// 256-byte buffer and calls $13AA0 to wait for the next frame. A sound is
// started by $13B4C/$13B74 (prefer the right or the left pair of channels),
// which takes the voice of the pair with the lowest remaining priority if the
// new priority (d0 * 4 frames) is greater. Here the voices are goroutines
// handed control one at a time, and the channel registers are rendered to
// 44.1 kHz stereo.
package sfx

import (
	"encoding/binary"
	"sync"
)

const (
	// SampleRate is the output rate.
	SampleRate = 44100
	palClock   = 3546895.0
	// The voices are not driven by the vertical blank: the interrupt at $13A7C
	// is CIA-B timer A ($13A26: latch $0D88 = 3464 ticks of the 709379 Hz E
	// clock, restarted at the end of every interrupt), about 204.8 times a
	// second. Every yield of a voice program is one of these ticks.
	voiceRate = 709379.0 / 3464.0
)

// Engine plays the sounds.
type Engine struct {
	mu      sync.Mutex
	voices  [4]*voice // voice 0 = Paula channel 3 (left), 1 = 2 (right), 2 = 1 (right), 3 = 0 (left)
	jingles []*jingle
	bigexpl []int8 // BIGEXPL.SND
	smart   []int8 // SMART.SND
	sine    [128]int8
	rnd     uint32
	vAcc    float64 // samples since the last voice interrupt
	jAcc    float64 // samples since the last game frame (the jingle task)
	gameTPS float64 // game frames per second (the jingle task waits in frames)
	muted   bool
}

// New builds the engine from the program (for the sine table at $15A42) and
// the two sampled sounds of the disk.
func New(prog, bigexpl, smart []byte) *Engine {
	e := &Engine{bigexpl: toInt8(bigexpl), smart: toInt8(smart), rnd: 0x2F6E2B1, gameTPS: 50}
	// $14346: 128 entries of the (sin, cos) table, halved and clipped to 127
	for i := range e.sine {
		o := 0x15A42 - 0xEFC0 + 4*i
		if o+2 <= len(prog) {
			s := int(int16(binary.BigEndian.Uint16(prog[o:]))) >> 1
			if s > 127 {
				s = 127
			}
			e.sine[i] = int8(s)
		}
	}
	for i := range e.voices {
		e.voices[i] = &voice{e: e, idx: i}
	}
	return e
}

func toInt8(b []byte) []int8 {
	out := make([]int8, len(b))
	for i, c := range b {
		out[i] = int8(c)
	}
	return out
}

// SetTPS tells the engine how fast the game runs (50 frames a second in the
// original): the voices keep the hardware's pace, but the jingle task, which
// waits in game frames, follows the game.
func (e *Engine) SetTPS(tps int) {
	if tps < 1 {
		return
	}
	e.mu.Lock()
	e.gameTPS = float64(tps)
	e.mu.Unlock()
}

// SetMuted silences the engine.
func (e *Engine) SetMuted(m bool) {
	e.mu.Lock()
	e.muted = m
	e.mu.Unlock()
}

// Close ends the voice programs.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, v := range e.voices {
		v.stop()
	}
}

// rand is $177FC's role in the sound code (a separate generator here, so that
// the sounds do not disturb the game's random sequence).
func (e *Engine) rand() int {
	e.rnd = e.rnd*1664525 + 1013904223
	return int(e.rnd >> 8)
}

// Read produces interleaved signed 16-bit little-endian stereo samples.
func (e *Engine) Read(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := len(p) / 4
	for i := 0; i < n; i++ {
		if e.jAcc++; e.jAcc >= SampleRate/e.gameTPS {
			e.jAcc -= SampleRate / e.gameTPS
			e.frame()
		}
		if e.vAcc++; e.vAcc >= SampleRate/voiceRate {
			e.vAcc -= SampleRate / voiceRate
			e.tick()
		}
		l, r := e.mix()
		binary.LittleEndian.PutUint16(p[4*i:], uint16(l))
		binary.LittleEndian.PutUint16(p[4*i+2:], uint16(r))
	}
	return n * 4, nil
}

// frame is one game frame: the jingle tasks ($1460E) step.
func (e *Engine) frame() {
	live := e.jingles[:0]
	for _, j := range e.jingles {
		if j.step(e) {
			live = append(live, j)
		}
	}
	e.jingles = live
}

// tick is one interrupt of the sound timer: every voice runs in turn.
func (e *Engine) tick() {
	for _, v := range e.voices {
		v.run()
	}
}

// mix renders one output sample.
func (e *Engine) mix() (l, r int16) {
	if e.muted {
		return 0, 0
	}
	var sl, sr int
	for _, v := range e.voices {
		s := v.sample()
		if v.idx == 0 || v.idx == 3 {
			sl += s
		} else {
			sr += s
		}
	}
	clip := func(x int) int16 {
		x *= 128
		if x > 32767 {
			x = 32767
		}
		if x < -32768 {
			x = -32768
		}
		return int16(x)
	}
	return clip(sl), clip(sr)
}

// pair returns the voices of the right (true) or left pair.
func (e *Engine) pair(right bool) (a, b *voice) {
	if right {
		return e.voices[1], e.voices[2]
	}
	return e.voices[0], e.voices[3]
}

// tryPair is $13B5C: the voice of the pair with the lower remaining priority
// is taken when prio (already times four) is greater than it.
func (e *Engine) tryPair(right bool, prio int, d1, d2 int, prog program) bool {
	a, b := e.pair(right)
	v, low := a, a.prio
	if b.prio < low {
		v, low = b, b.prio
	}
	if prio <= low {
		return false
	}
	v.start(prio, d1, d2, prog)
	return true
}

// alloc is $13B4C (preferRight) / $13B74: d0 is the priority in units of four
// frames; the preferred pair is tried first, then the other.
func (e *Engine) alloc(preferRight bool, d0, d1, d2 int, prog program) bool {
	prio := (d0 * 4) & 0xFFFF
	if e.tryPair(preferRight, prio, d1, d2, prog) {
		return true
	}
	return e.tryPair(!preferRight, prio, d1, d2, prog)
}

// byX is $13BCC: a sound at x < $A0 prefers the left channels.
func byX(x int) (preferRight bool) { return int16(x) >= 0xA0 }
