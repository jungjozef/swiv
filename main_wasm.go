//go:build wasm

// The browser build (GOOS=js GOARCH=wasm): no command line, no development
// keys, no text line at the bottom of the screen. The disk image is fetched
// from the page's own server (SWIV.adf next to the page).
package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"syscall/js"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"

	"github.com/jungjozef/swiv/internal/adf"
)

func init() { debugKeys = false }

// boot is shown while the disk image downloads and then hands over to the
// game (or shows why it could not start).
type boot struct {
	mu   sync.Mutex
	game ebiten.Game
	err  error
	tick int
}

func (l *boot) current() (ebiten.Game, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.game, l.err
}

func (l *boot) set(g ebiten.Game, err error) {
	l.mu.Lock()
	l.game, l.err = g, err
	l.mu.Unlock()
}

func (l *boot) Update() error {
	if g, _ := l.current(); g != nil {
		return g.Update()
	}
	l.tick++
	return nil
}

func (l *boot) Draw(screen *ebiten.Image) {
	g, err := l.current()
	switch {
	case g != nil:
		g.Draw(screen)
	case err != nil:
		ebitenutil.DebugPrintAt(screen, "SWIV could not start:\n"+err.Error(), 8, 8)
	default:
		ebitenutil.DebugPrintAt(screen, "Loading"+[]string{"", ".", "..", "..."}[l.tick/20%4], 8, 8)
	}
}

func (l *boot) Layout(_, _ int) (int, int) { return ScreenW, ScreenH }

// fetchDisk downloads SWIV.adf from the directory of the page.
func fetchDisk() (*adf.Disk, error) {
	base, err := url.Parse(js.Global().Get("location").Get("href").String())
	if err != nil {
		return nil, err
	}
	resp, err := http.Get(base.ResolveReference(&url.URL{Path: "SWIV.adf"}).String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SWIV.adf: %s", resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return adf.Parse(b)
}

// touchDevice reports whether the browser has a touch screen: the virtual pad
// is then drawn from the start.
func touchDevice() bool {
	if js.Global().Get("ontouchstart").Truthy() {
		return true
	}
	nav := js.Global().Get("navigator")
	return nav.Truthy() && nav.Get("maxTouchPoints").Truthy() && nav.Get("maxTouchPoints").Int() > 0
}

func main() {
	pad.enabled = touchDevice()
	l := &boot{}
	go func() {
		disk, err := fetchDisk()
		if err != nil {
			l.set(nil, err)
			return
		}
		// the audio context can be made once: music and effects share it
		music := NewModMusic(false)
		l.set(startDisk(music, disk), nil)
	}()
	ebiten.SetWindowTitle("SWIV")
	ebiten.SetTPS(50)
	if err := ebiten.RunGame(l); err != nil {
		panic(err)
	}
}
