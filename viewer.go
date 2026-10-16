package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/jungjozef/swiv/internal/adf"
	"github.com/jungjozef/swiv/internal/lin"
	"github.com/jungjozef/swiv/internal/raw"
)

// Viewer pages through the disk's full-screen .RAW images.
type Viewer struct {
	names  []string
	imgs   []*ebiten.Image
	cur    int
	layout raw.Layout
	disk   *adf.Disk
	data   map[string][]byte
}

// NewViewer loads every .RAW file on the disk.
func NewViewer(d *adf.Disk) (*Viewer, error) {
	v := &Viewer{disk: d, data: map[string][]byte{}}
	for i, e := range d.Entries {
		if !strings.HasSuffix(strings.ToUpper(e.Name), ".RAW") {
			continue
		}
		b, err := d.Read(i)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name, err)
		}
		v.names = append(v.names, e.Name)
		v.data[e.Name] = b
	}
	if len(v.names) == 0 {
		return nil, fmt.Errorf("no .RAW files on disk")
	}
	v.render()
	return v, nil
}

func (v *Viewer) render() {
	v.imgs = make([]*ebiten.Image, len(v.names))
	for i, n := range v.names {
		img, err := raw.Decode(v.data[n], v.layout)
		if err != nil {
			continue
		}
		v.imgs[i] = ebiten.NewImageFromImage(img)
	}
}

func (v *Viewer) Update() error {
	n := len(v.names)
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowRight) {
		v.cur = (v.cur + 1) % n
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowLeft) {
		v.cur = (v.cur + n - 1) % n
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyL) {
		v.layout = 1 - v.layout
		v.render()
	}
	return nil
}

func (v *Viewer) Draw(screen *ebiten.Image) {
	if img := v.imgs[v.cur]; img != nil {
		screen.DrawImage(img, nil)
	}
	l := "sequential"
	if v.layout == raw.Interleaved {
		l = "interleaved"
	}
	ebitenutil.DebugPrint(screen, fmt.Sprintf("%s (%d/%d) %s  [←/→ page, L layout]",
		v.names[v.cur], v.cur+1, len(v.names), l))
}

func (v *Viewer) Layout(_, _ int) (int, int) { return raw.Width, raw.Height }

// dumpRaw writes every .RAW file as a PNG into dir.
func dumpRaw(d *adf.Disk, dir string, l raw.Layout) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i, e := range d.Entries {
		if !strings.HasSuffix(strings.ToUpper(e.Name), ".RAW") {
			continue
		}
		b, err := d.Read(i)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}
		img, err := raw.Decode(b, l)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}
		f, err := os.Create(filepath.Join(dir, strings.TrimSuffix(e.Name, filepath.Ext(e.Name))+".png"))
		if err != nil {
			return err
		}
		err = png.Encode(f, img)
		f.Close()
		if err != nil {
			return err
		}
		fmt.Println("wrote", f.Name())
	}
	return nil
}

// sheetPalette is a fixed, high-contrast 16-colour palette used to inspect
// sprite banks, whose real palettes are chosen per level by the game.
var sheetPalette = [16]color.RGBA{
	{0x00, 0x00, 0x00, 0xff}, {0xff, 0xff, 0xff, 0xff}, {0xe0, 0x20, 0x20, 0xff}, {0x20, 0xc0, 0x20, 0xff},
	{0x30, 0x50, 0xff, 0xff}, {0xff, 0xd0, 0x20, 0xff}, {0xff, 0x80, 0x20, 0xff}, {0xa0, 0x40, 0xd0, 0xff},
	{0x20, 0xc0, 0xc0, 0xff}, {0x90, 0x90, 0x90, 0xff}, {0x60, 0x30, 0x10, 0xff}, {0xff, 0x90, 0xb0, 0xff},
	{0x00, 0x60, 0x00, 0xff}, {0x00, 0x00, 0x80, 0xff}, {0x60, 0x60, 0x60, 0xff}, {0xc0, 0xc0, 0x80, 0xff},
}

// dumpLIN writes a contact sheet PNG per .LIN file, one cell per image (the
// strips of a chained image are composed with their hotspots aligned).
func dumpLIN(d *adf.Disk, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i, e := range d.Entries {
		if !strings.HasSuffix(strings.ToUpper(e.Name), ".LIN") {
			continue
		}
		b, err := d.Read(i)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}
		bank, err := lin.Parse(b)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}
		const cellW, cellH, perRow = 112, 112, 8
		rows := (len(bank.Images) + perRow - 1) / perRow
		sheet := image.NewRGBA(image.Rect(0, 0, cellW*perRow, cellH*rows))
		draw.Draw(sheet, sheet.Bounds(), &image.Uniform{color.RGBA{0x28, 0x28, 0x28, 0xff}}, image.Point{}, draw.Src)
		for n := range bank.Images {
			ox, oy := n%perRow*cellW, n/perRow*cellH
			r := bank.ImageRect(n)
			// centre the image's bounding box in the cell
			cx := ox + (cellW-r.Dx())/2 - r.Min.X
			cy := oy + (cellH-r.Dy())/2 - r.Min.Y
			cell := sheet.SubImage(image.Rect(ox, oy, ox+cellW-1, oy+cellH-1)).(*image.RGBA)
			bank.DrawImage(cell, n, cx, cy, &sheetPalette)
		}
		out := filepath.Join(dir, strings.TrimSuffix(e.Name, filepath.Ext(e.Name))+".png")
		fh, err := os.Create(out)
		if err != nil {
			return err
		}
		err = png.Encode(fh, sheet)
		fh.Close()
		if err != nil {
			return err
		}
		fmt.Printf("%-14s %3d frames, %3d images -> %s\n", e.Name, len(bank.Frames), len(bank.Images), out)
	}
	return nil
}

// snapshot renders one frame of a level scene to a PNG file.
func snapshot(sc *LevelScene, path string) error {
	sc.Compose()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, sc.buf)
}
