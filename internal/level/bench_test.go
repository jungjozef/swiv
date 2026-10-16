package level

import (
	"image"
	"testing"

	"github.com/jungjozef/swiv/internal/adf"
)

func BenchmarkRender(b *testing.B) {
	d, err := adf.Open("../../SWIV.adf")
	if err != nil {
		b.Skip(err)
	}
	l, err := Load(d, 0)
	if err != nil {
		b.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, ScreenW, ScreenH))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Render(0xE600-i%0x400, img)
	}
}
