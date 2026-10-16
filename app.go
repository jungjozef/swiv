package main

import (
	"log"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/jungjozef/swiv/internal/adf"
	"github.com/jungjozef/swiv/internal/level"
)

// The Amiga PAL lowres screen SWIV ran on, ticking at the 50Hz vblank rate.
const (
	ScreenW = 320
	ScreenH = 256
	TPS     = 500
	Scale   = 3
)

// unlimitedLives is the -unlimited-lives option.
var unlimitedLives bool

// debugKeys enables the keys and the text line that help with development
// (T drops tokens, 1-6 set the weapon, PageUp/PageDown scroll, Home restarts,
// H hides the line at the bottom of the screen). The browser build turns it off.
var debugKeys = true

// startDisk builds the game from an opened disk: the title screen with the
// attract loop, which starts the first level on fire.
func startDisk(music Music, disk *adf.Disk) ebiten.Game {
	app := &App{}
	if mm, ok := music.(*ModMusic); ok {
		if err := startSounds(mm.ctx, disk); err != nil {
			log.Printf("sound effects: %v", err)
		}
	}
	scores := NewHighScores(disk)
	var title func()
	begin := func() {
		music.Stop()
		lv, err := level.Chain(disk, 0, 7)
		if err != nil {
			log.Printf("level 0: %v; starting placeholder game", err)
			app.cur = NewGame(music)
			return
		}
		sc, err := NewLevelScene(disk, lv, level.StartTop)
		if err != nil {
			log.Printf("level scene: %v; starting placeholder game", err)
			app.cur = NewGame(music)
			return
		}
		sc.scores = scores
		sc.player.Unlimited = unlimitedLives
		sc.onExit = title
		app.cur = sc
	}
	title = func() {
		at, err := NewAttract(disk, scores, begin)
		if err != nil {
			log.Fatal(err)
		}
		if tune, err := disk.ReadName("AMTITUNE.MOD"); err == nil {
			if err := music.PlayBytes(tune); err != nil {
				log.Printf("AMTITUNE.MOD: %v", err)
			}
		}
		app.cur = at
	}
	title()
	return app
}
