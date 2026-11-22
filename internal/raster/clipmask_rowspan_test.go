package raster

import (
	"bytes"
	"image"
	"math/rand"
	"testing"

	"github.com/egoist/mygo/internal/scene"
)

// fadeStops fade a mask from opaque to transparent, as a card's body does
// at its bottom.
var fadeStops = []scene.Stop{{Color: scene.Color{A: 255}, At: 0}, {Color: scene.Color{A: 255}, At: 0.6}, {At: 1}}

// A masked or transformed clip fills the pixels it covers entirely in a
// row at once (rowSpan); the mask is the same as pixel by pixel.
func TestClipMaskRowSpansMatchPixels(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	spans := 0
	for i := 0; i < 300; i++ {
		w, h := 40+rng.Intn(200), 40+rng.Intn(200)
		var shapes []scene.ClipShape
		// A card's clip, then a faded one inside it, maybe transformed.
		r := float32(rng.Intn(20))
		shapes = append(shapes, scene.ClipShape{Rect: scene.Rect{X: rng.Float32() * 10, Y: rng.Float32()*10 - 5, W: float32(w) - 10, H: float32(h) - 5},
			Radii: [4]float32{r, r, r, r}, Continuous: rng.Intn(2) == 0})
		c := scene.ClipShape{Rect: scene.Rect{X: rng.Float32() * 20, Y: rng.Float32() * 20, W: float32(w) - 30, H: float32(h) - 30}, Paint: scene.PaintLinearStops, Stops: fadeStops}
		if rng.Intn(3) > 0 {
			rr := float32(rng.Intn(8))
			c.Radii = [4]float32{rr, rr, rr, rr}
		}
		cx := c.Rect.X + c.Rect.W/2
		c.Gradient = [4]float32{cx, c.Rect.Y, cx, c.Rect.Y + c.Rect.H}
		switch rng.Intn(3) {
		case 1:
			s := 0.9 + rng.Float32()*0.2
			c.Transform = scene.Matrix(s, 0, 0, s, rng.Float32()*10-5, rng.Float32()*10-5)
		case 2: // turned: no spans
			c.Transform = scene.Matrix(0.99, 0.1, -0.1, 0.99, 3, 2)
		}
		shapes = append(shapes, c)
		area := image.Rect(0, 0, w, h)
		got, want := make([]byte, w*h), make([]byte, w*h)
		ClipMaskAlpha(got, shapes, area)
		noRowSpans = true
		ClipMaskAlpha(want, shapes, area)
		noRowSpans = false
		if !bytes.Equal(got, want) {
			n := 0
			for j := range got {
				if got[j] != want[j] {
					n++
				}
			}
			t.Fatalf("case %d (%dx%d, transform %v): %d of %d pixels differ", i, w, h, c.Transform.Set, n, len(got))
		}
		spans++
	}
	if spans == 0 {
		t.Fatal("no cases")
	}
}

func BenchmarkClipMaskFade(b *testing.B) {
	c := scene.ClipShape{Rect: scene.Rect{X: 1588, Y: 104, W: 496, H: 436}, Paint: scene.PaintLinearStops, Gradient: [4]float32{1836, 104, 1836, 540},
		Stops: fadeStops, Transform: scene.Matrix(1.0157907, 0, 0, 1.0157907, -1632.9917, -101.52673)}
	pix := make([]byte, 464*452)
	for _, off := range []bool{false, true} {
		b.Run(map[bool]string{false: "spans", true: "pixels"}[off], func(b *testing.B) {
			noRowSpans = off
			defer func() { noRowSpans = false }()
			for range b.N {
				ClipMaskAlpha(pix, []scene.ClipShape{c}, image.Rect(0, 0, 464, 452))
			}
		})
	}
}
