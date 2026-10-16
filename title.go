package main

import "github.com/hajimehoshi/ebiten/v2"

// App switches between scenes.
type App struct{ cur ebiten.Game }

func (a *App) Update() error {
	pad.update()
	return a.cur.Update()
}

func (a *App) Draw(s *ebiten.Image) {
	a.cur.Draw(s)
	pad.draw(s)
}

func (a *App) Layout(w, h int) (int, int) { return a.cur.Layout(w, h) }
