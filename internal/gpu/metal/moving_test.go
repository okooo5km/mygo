//go:build darwin

package metal

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/gpu/gputest"
	"github.com/egoist/mygo/internal/scene"
)

// TestMovingClipsDrawShifted draws gputest.MovingClipScene at the scales
// apps draw at, scrolled by whole pixels and by fractions of one: each
// frame draws as the CPU renderer does; one scrolled by whole pixels draws
// the same bytes as the first, moved, inside the list, and rasterizes no
// mask again; and A8 masks draw the bytes the RGBA ones did.
func TestMovingClipsDrawShifted(t *testing.T) {
	r, err := newRenderer()
	if err != nil {
		unavailable(t, err)
	}
	defer r.Release()
	for _, k := range gputest.MovingClipScales {
		t.Run(fmt.Sprint(k), func(t *testing.T) {
			base := gputest.MovingClipScene(0, 0, k)
			first, err := r.renderOffscreen(base)
			if err != nil {
				t.Fatal(err)
			}
			gputest.Compare(t, fmt.Sprintf("metal-moving-%v", k), first, base.Width*4, base)
			if r.b.MasksDrawn == 0 {
				t.Fatal("the scene rasterized no clip mask")
			}
			for _, d := range [][2]float32{{0, -7}, {0, -60}, {3, -25}, {0, -0.5}, {0.25, -12.75}} {
				s := gputest.MovingClipScene(d[0], d[1], k)
				got, err := r.renderOffscreen(s)
				if err != nil {
					t.Fatal(err)
				}
				name := fmt.Sprintf("metal-moving-%v-%v,%v", k, d[0], d[1])
				gputest.Compare(t, name, got, s.Width*4, s)
				whole := d[0] == float32(int(d[0])) && d[1] == float32(int(d[1]))
				if !whole {
					continue
				}
				// All but the pressed card's: it scales about its middle,
				// which moves with it, a transform of its own.
				if r.b.MasksDrawn > 1 || r.b.MasksReused < 3 {
					t.Errorf("%s: rasterized %d masks, reused %d, want all but one reused", name, r.b.MasksDrawn, r.b.MasksReused)
				}
				// Inside the list in both frames, the same bytes moved.
				dx, dy := int(d[0]), int(d[1])
				x0, y0, x1, y1 := int(8*k), int(8*k), int(8*k)+int(344*k), int(8*k)+int(504*k)
				w := s.Width * 4
				diff := 0
				for y := max(y0, y0+dy); y < min(y1, y1+dy); y++ {
					for x := max(x0, x0+dx); x < min(x1, x1+dx); x++ {
						if !bytes.Equal(got[y*w+4*x:][:4], first[(y-dy)*w+4*(x-dx):][:4]) {
							diff++
						}
					}
				}
				if diff != 0 {
					t.Errorf("%s: %d pixels differ from the first frame's moved", name, diff)
				}
			}
		})
	}
}

// TestA8MasksDrawAsRGBA draws the masked scenes with clip masks in A8 and
// in RGBA images, and requires the same bytes.
func TestA8MasksDrawAsRGBA(t *testing.T) {
	draw := func(rgba bool) [][]byte {
		rgbaMasks = rgba
		defer func() { rgbaMasks = false }()
		r, err := newRenderer()
		if err != nil {
			unavailable(t, err)
		}
		defer r.Release()
		var out [][]byte
		for _, k := range gputest.MovingClipScales {
			for _, d := range []float32{0, -7, -0.5} {
				pix, err := r.renderOffscreen(gputest.MovingClipScene(0, d, k))
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, pix)
			}
		}
		for range 2 {
			pix, err := r.renderOffscreen(gputest.MaskedClipScene())
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, pix)
		}
		return out
	}
	a8, rgba := draw(false), draw(true)
	for i := range a8 {
		if n := differing(a8[i], rgba[i]); n != 0 {
			t.Errorf("frame %d: %d bytes differ between A8 and RGBA masks", i, n)
		}
	}
}

// TestSweepFreesUnseenTexturesByTime draws a picture, then frames without
// it: the sweep keeps its texture while it was drawn lately, and frees it
// once no frame drew it for unseenFor, however few frames went by.
func TestSweepFreesUnseenTexturesByTime(t *testing.T) {
	r, err := newRenderer()
	if err != nil {
		unavailable(t, err)
	}
	defer r.Release()
	img := scene.NewImageRGBA(4, 4, make([]byte, 64))
	with := &scene.Scene{Width: 32, Height: 32, Ops: []scene.Op{{Kind: scene.OpImage, Rect: scene.Rect{W: 16, H: 16}, Image: img, Src: scene.Rect{W: 4, H: 4}}}}
	without := &scene.Scene{Width: 32, Height: 32, Ops: []scene.Op{{Kind: scene.OpFill, Rect: scene.Rect{W: 8, H: 8}, Color: scene.Color{A: 255}}}}
	if _, err := r.renderOffscreen(with); err != nil {
		t.Fatal(err)
	}
	if r.images[img.ID()] == nil {
		t.Fatal("the picture has no texture")
	}
	if _, err := r.renderOffscreen(without); err != nil {
		t.Fatal(err)
	}
	r.sweep()
	if r.images[img.ID()] == nil {
		t.Fatal("the sweep freed a texture drawn a moment ago")
	}
	r.images[img.ID()].lastDrawn = r.now.Add(-unseenFor - time.Second)
	if _, err := r.renderOffscreen(without); err != nil {
		t.Fatal(err)
	}
	r.sweep()
	if r.images[img.ID()] != nil {
		t.Fatal("the sweep kept a texture no frame drew for longer than unseenFor, 3 frames later")
	}
}
