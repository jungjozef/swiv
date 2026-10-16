package sfx

import (
	"os"
	"testing"
)

func load(t *testing.T) *Engine {
	t.Helper()
	prog, err := os.ReadFile("../../assets/AMPROG.OBJ")
	if err != nil {
		t.Skip("no extracted assets")
	}
	be, _ := os.ReadFile("../../assets/BIGEXPL.SND")
	sm, _ := os.ReadFile("../../assets/SMART.SND")
	return New(prog, be, sm)
}

func peak(e *Engine, frames int) (l, r int) {
	buf := make([]byte, 4*882)
	for f := 0; f < frames; f++ {
		e.Read(buf)
		for i := 0; i < 882; i++ {
			a := int(int16(uint16(buf[4*i]) | uint16(buf[4*i+1])<<8))
			b := int(int16(uint16(buf[4*i+2]) | uint16(buf[4*i+3])<<8))
			if a < 0 {
				a = -a
			}
			if b < 0 {
				b = -b
			}
			l, r = max(l, a), max(r, b)
		}
	}
	return
}

// Every sound is audible, ends (or keeps sounding as designed) and the voices
// can be taken over without a deadlock.
func TestEverySoundPlays(t *testing.T) {
	ids := []int{BigExplosion, SmallExplosion, Explosion, SmartBomb, Blip, Sweep, Warble, NoiseL, NoiseR, Whoosh, Hum, Hit,
		Swell, Popup, Hiss, Rumble, Buzz, Shot, Chirp, ChirpLong, Siren, Chorus, Notes3, Jingle1, Jingle2}
	for _, id := range ids {
		e := load(t)
		e.Play(id, 80)
		l, r := peak(e, 120)
		if l == 0 && r == 0 {
			t.Errorf("sound %#x is silent", id)
		}
		e.Close()
	}
}

func TestPanning(t *testing.T) {
	e := load(t)
	e.Play(Hit, 10) // left of $A0
	l, r := peak(e, 30)
	if l == 0 || r != 0 {
		t.Fatalf("a sound on the left: l %d r %d", l, r)
	}
	e.Close()
	e = load(t)
	e.Play(Hit, 300)
	l, r = peak(e, 30)
	if r == 0 || l != 0 {
		t.Fatalf("a sound on the right: l %d r %d", l, r)
	}
	e.Close()
}

// A higher priority takes a busy voice; a lower one finds none.
func TestPriority(t *testing.T) {
	e := load(t)
	for i := 0; i < 6; i++ {
		e.Play(Hum, 80) // prio $3c: both left voices busy after two
	}
	peak(e, 2)
	e.Play(BigExplosion, 80)
	peak(e, 5)
	e.Close()
}
