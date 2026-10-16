module github.com/jungjozef/swiv

go 1.27.2

require (
	github.com/hajimehoshi/ebiten/v2 v2.10.5
	protracker-go v0.0.0
	protracker-player v0.0.0
)

require (
	github.com/ebitengine/gomobile v0.0.0-20260820040257-d11f821a26a6 // indirect
	github.com/ebitengine/hideconsole v1.0.0 // indirect
	github.com/ebitengine/oto/v3 v3.5.0 // indirect
	github.com/ebitengine/purego v0.11.0 // indirect
	github.com/jfreymuth/pulse v0.1.3 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

replace protracker-go => ../protracker-go

replace protracker-player => ../protracker-go/player
