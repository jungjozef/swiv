//go:build wasm

package main

import (
	"math"
	"sync"
	"syscall/js"
)

// The browser's touches, read directly from the page. Ebitengine takes the
// clientX/clientY of a touch and converts it as if the canvas covered the whole
// page; in the framed layout (and with the letterbox of a screen of another
// shape) that puts every finger in the wrong place. Here the position is
// mapped through the canvas's own rectangle and the game screen's placement in
// it.
var (
	jsMu      sync.Mutex
	jsTouches []touch
)

func init() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	touchSource = func() []touch {
		jsMu.Lock()
		defer jsMu.Unlock()
		return append([]touch(nil), jsTouches...)
	}
	read := js.FuncOf(func(this js.Value, args []js.Value) any {
		e := args[0]
		e.Call("preventDefault")
		list := e.Get("touches")
		ts := make([]touch, 0, list.Length())
		for i := 0; i < list.Length(); i++ {
			t := list.Call("item", i)
			x, y := logicalPosition(doc, t.Get("clientX").Float(), t.Get("clientY").Float())
			ts = append(ts, touch{t.Get("identifier").Int(), x, y})
		}
		jsMu.Lock()
		jsTouches = ts
		jsMu.Unlock()
		return nil
	})
	opts := js.ValueOf(map[string]any{"passive": false})
	for _, name := range []string{"touchstart", "touchmove", "touchend", "touchcancel"} {
		doc.Call("addEventListener", name, read, opts)
	}
}

// logicalPosition maps a point of the page to the 320x256 game screen, which
// the engine scales to fit the canvas and centres in it.
func logicalPosition(doc js.Value, cx, cy float64) (float64, float64) {
	canvas := doc.Call("querySelector", "canvas")
	if !canvas.Truthy() {
		return cx, cy
	}
	r := canvas.Call("getBoundingClientRect")
	left, top := r.Get("left").Float(), r.Get("top").Float()
	w, h := r.Get("width").Float(), r.Get("height").Float()
	scale := math.Min(w/ScreenW, h/ScreenH)
	if scale <= 0 {
		return cx, cy
	}
	ox := left + (w-ScreenW*scale)/2
	oy := top + (h-ScreenH*scale)/2
	return (cx - ox) / scale, (cy - oy) / scale
}
