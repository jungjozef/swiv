//go:build !wasm

package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/jungjozef/swiv/internal/adf"
	"github.com/jungjozef/swiv/internal/level"
	"github.com/jungjozef/swiv/internal/raw"
)

func main() {
	modPath := flag.String("mod", "", "path to a ProTracker .mod file to play as music")
	filter := flag.Bool("filter", false, "enable Amiga low-pass filter")
	adfPath := flag.String("adf", "SWIV.adf", "path to the SWIV disk image")
	view := flag.Bool("view", false, "browse the disk's full-screen .RAW images")
	dump := flag.String("dump", "", "write every .RAW image as a PNG into this directory and exit")
	dumpLin := flag.String("dump-lin", "", "write a contact-sheet PNG for every .LIN sprite bank into this directory and exit")
	mute := flag.Bool("mute", false, "disable music")
	lvl := flag.Int("level", -1, "start directly in this level (0 town, 1 desert, 2 grass, 3 river, 4 ice, 5 scifi, 6 final)")
	top := flag.Int("top", level.StartTop, "initial scroll value of the screen top (use with -level)")
	snap := flag.String("snapshot", "", "with -level: write one frame at -top to this PNG file and exit")
	holdFire := flag.Int("hold-fire", 0, "with -snapshot: hold fire for this many ticks first")
	flag.BoolVar(&unlimitedLives, "unlimited-lives", false, "the helicopter never runs out of lives")
	weaponLvl := flag.Int("weapon", 0, "start with this weapon level (1-6)")
	interleaved := flag.Bool("interleaved", false, "treat .RAW bitplanes as row-interleaved")
	tps := flag.Int("tps", 50, "TPS for the game speed")
	flag.Parse()

	// the audio context can be made once: music and effects share it
	var music *ModMusic
	if !*mute {
		music = NewModMusic(*filter)
	}

	layout := raw.Sequential
	if *interleaved {
		layout = raw.Interleaved
	}

	var game ebiten.Game
	switch {
	case *lvl >= 0:
		disk, err := adf.Open(*adfPath)
		if err != nil {
			log.Fatal(err)
		}
		if music != nil {
			if err := startSounds(music.ctx, disk); err != nil {
				log.Printf("sound effects: %v", err)
			}
		}
		lv, err := level.Chain(disk, *lvl, 7)
		if err != nil {
			log.Fatal(err)
		}
		sc, err := NewLevelScene(disk, lv, *top)
		if err != nil {
			log.Fatal(err)
		}
		sc.player.Unlimited = unlimitedLives
		if *weaponLvl > 0 {
			sc.shots.SetLevel(*weaponLvl)
		}
		if *snap != "" {
			sc.Advance(*holdFire, 0, 0, true)
			if err := snapshot(sc, *snap); err != nil {
				log.Fatal(err)
			}
			return
		}
		game = sc
	case *dump != "" || *dumpLin != "" || *view:
		disk, err := adf.Open(*adfPath)
		if err != nil {
			log.Fatal(err)
		}
		if *dumpLin != "" {
			if err := dumpLIN(disk, *dumpLin); err != nil {
				log.Fatal(err)
			}
			return
		}
		if *dump != "" {
			if err := dumpRaw(disk, *dump, layout); err != nil {
				log.Fatal(err)
			}
			return
		}
		v, err := NewViewer(disk)
		if err != nil {
			log.Fatal(err)
		}
		v.layout = layout
		v.render()
		game = v
	default:
		var m Music = NopMusic{}
		if music != nil {
			m = music
		}
		game = start(m, *adfPath, *modPath)
	}

	ebiten.SetWindowSize(ScreenW*Scale, ScreenH*Scale)
	ebiten.SetWindowTitle(fmt.Sprintf("SWIV at %d", *tps))
	ebiten.SetTPS(*tps)
	if sounds != nil {
		sounds.SetTPS(*tps)
	}
	if err := ebiten.RunGame(game); err != nil {
		log.Fatal(err)
	}
}

// start builds the opening scene: the title screen when the disk image is
// available, otherwise the placeholder game (optionally with -mod music).
func start(music Music, adfPath, modPath string) ebiten.Game {
	if modPath != "" {
		if err := music.Play(modPath); err != nil {
			log.Fatal(err)
		}
		return &App{cur: NewGame(music)}
	}
	disk, err := adf.Open(adfPath)
	if err != nil {
		log.Printf("no disk image (%v); starting placeholder game", err)
		return &App{cur: NewGame(music)}
	}
	return startDisk(music, disk)
}
