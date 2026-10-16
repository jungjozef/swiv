package main

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	playerSpeed   = 2.0 // pixels per tick
	scrollSpeed   = 1.0 // pixels per tick
	bulletSpeed   = 6.0
	fireInterval  = 6 // ticks between shots
	playerW       = 16
	playerH       = 16
	bulletW       = 2
	bulletH       = 6
	backgroundTil = 32
)

type bullet struct{ x, y float64 }

type Game struct {
	music Music

	px, py   float64
	cooldown int
	bullets  []bullet
	scrollY  float64
}

func NewGame(m Music) *Game {
	return &Game{
		music: m,
		px:    ScreenW/2 - playerW/2,
		py:    ScreenH - 48,
	}
}

func (g *Game) Update() error {
	g.scrollY += scrollSpeed
	if g.scrollY >= backgroundTil*2 {
		g.scrollY -= backgroundTil * 2
	}

	var dx, dy float64
	if ebiten.IsKeyPressed(ebiten.KeyArrowLeft) {
		dx--
	}
	if ebiten.IsKeyPressed(ebiten.KeyArrowRight) {
		dx++
	}
	if ebiten.IsKeyPressed(ebiten.KeyArrowUp) {
		dy--
	}
	if ebiten.IsKeyPressed(ebiten.KeyArrowDown) {
		dy++
	}
	g.px = clamp(g.px+dx*playerSpeed, 0, ScreenW-playerW)
	g.py = clamp(g.py+dy*playerSpeed, 0, ScreenH-playerH)

	if g.cooldown > 0 {
		g.cooldown--
	}
	if ebiten.IsKeyPressed(ebiten.KeySpace) && g.cooldown == 0 {
		g.bullets = append(g.bullets, bullet{g.px + playerW/2 - bulletW/2, g.py})
		g.cooldown = fireInterval
	}

	live := g.bullets[:0]
	for _, b := range g.bullets {
		b.y -= bulletSpeed
		if b.y > -bulletH {
			live = append(live, b)
		}
	}
	g.bullets = live
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{0x10, 0x30, 0x18, 0xff})

	// Placeholder scrolling ground: checkerboard tiles.
	dark := color.RGBA{0x14, 0x3c, 0x1e, 0xff}
	for row := -1; row <= ScreenH/backgroundTil+1; row++ {
		for col := range ScreenW / backgroundTil {
			if (row+col)&1 == 0 {
				continue
			}
			y := float32(row*backgroundTil) + float32(g.scrollY)
			vector.FillRect(screen, float32(col*backgroundTil), y, backgroundTil, backgroundTil, dark, false)
		}
	}

	for _, b := range g.bullets {
		vector.FillRect(screen, float32(b.x), float32(b.y), bulletW, bulletH, color.RGBA{0xff, 0xe0, 0x40, 0xff}, false)
	}
	vector.FillRect(screen, float32(g.px), float32(g.py), playerW, playerH, color.RGBA{0xc0, 0xc8, 0xd8, 0xff}, false)
}

func (g *Game) Layout(_, _ int) (int, int) { return ScreenW, ScreenH }

func clamp(v, lo, hi float64) float64 {
	return min(max(v, lo), hi)
}
