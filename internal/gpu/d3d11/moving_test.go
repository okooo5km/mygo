//go:build windows

package d3d11

import (
	"bytes"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/gpu/gputest"
	"github.com/egoist/mygo/internal/scene"
)

// TestMovingClipsDrawShifted draws gputest.MovingClipScene at the scales
// apps draw at, scrolled by whole pixels and by fractions of one: each
// frame draws as the CPU renderer does; one scrolled by whole pixels draws
// the same bytes as the first, moved, inside the list, and rasterizes no
// mask again but the pressed card's (its transform scales about its
// middle, which moves with it). Its uploads leave the staging textures
// within their bound.
func TestMovingClipsDrawShifted(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, k := range gputest.MovingClipScales {
		t.Run(fmt.Sprint(k), func(t *testing.T) {
			base := gputest.MovingClipScene(0, 0, k)
			r, err := New(hiddenWindow(t, base.Width, base.Height))
			if err != nil {
				t.Skip("no Direct3D 11:", err)
			}
			defer r.Release()
			if err := r.draw(base); err != nil {
				t.Fatal(err)
			}
			first, stride := r.readBack(t)
			first = append([]byte(nil), first...)
			gputest.Compare(t, fmt.Sprintf("d3d11-moving-%v", k), first, stride, base)
			if r.b.MasksDrawn == 0 || len(r.masks) == 0 {
				t.Fatalf("the scene rasterized %d clip masks, %d A8 textures", r.b.MasksDrawn, len(r.masks))
			}
			for _, d := range [][2]float32{{0, -7}, {0, -60}, {3, -25}, {0, -0.5}, {0.25, -12.75}} {
				s := gputest.MovingClipScene(d[0], d[1], k)
				if err := r.draw(s); err != nil {
					t.Fatal(err)
				}
				got, stride := r.readBack(t)
				name := fmt.Sprintf("d3d11-moving-%v-%v,%v", k, d[0], d[1])
				gputest.Compare(t, name, got, stride, s)
				if d[0] != float32(int(d[0])) || d[1] != float32(int(d[1])) {
					continue
				}
				if r.b.MasksDrawn > 1 || r.b.MasksReused < 3 {
					t.Errorf("%s: rasterized %d masks, reused %d, want all but one reused", name, r.b.MasksDrawn, r.b.MasksReused)
				}
				dx, dy := int(d[0]), int(d[1])
				x0, y0, x1, y1 := int(8*k), int(8*k), int(8*k)+int(344*k), int(8*k)+int(504*k)
				// The same bytes, but for a level here and there: the
				// shader's float arithmetic at other places rounds
				// differently (Metal's draws them identical).
				diff, worst := 0, 0
				for y := max(y0, y0+dy); y < min(y1, y1+dy); y++ {
					for x := max(x0, x0+dx); x < min(x1, x1+dx); x++ {
						a, b := got[y*stride+4*x:][:4], first[(y-dy)*stride+4*(x-dx):][:4]
						if !bytes.Equal(a, b) {
							diff++
							for c := range 4 {
								worst = max(worst, int(a[c])-int(b[c]), int(b[c])-int(a[c]))
							}
						}
					}
				}
				if diff > 0 {
					t.Logf("%s: %d pixels differ from the first frame's moved, by %d levels at most", name, diff, worst)
				}
				if worst > 1 || diff > (x1-x0)*(y1-y0)/10000 {
					t.Errorf("%s: %d pixels differ from the first frame's moved, by %d levels at most", name, diff, worst)
				}
			}
			if n := r.stagingBytes(); n > maxStages*stageSide*stageSide*4 {
				t.Errorf("staging textures take %d bytes", n)
			}
		})
	}
}

// TestTrimGivesBackTextures draws pictures, then has the window go idle:
// visible, it keeps the textures of the last frame; hidden, it frees every
// texture, and the next frame draws as before.
func TestTrimGivesBackTextures(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	s := gputest.MovingClipScene(0, 0, 1)
	img := scene.NewImageRGBA(600, 300, make([]byte, 600*300*4))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	other := scene.NewImageRGBA(64, 64, make([]byte, 64*64*4))
	s.Ops = append(s.Ops, scene.Op{Kind: scene.OpImage, Rect: scene.Rect{X: 20, Y: 20, W: 300, H: 150}, Image: img, Src: scene.Rect{W: 600, H: 300}})
	r, err := New(hiddenWindow(t, s.Width, s.Height))
	if err != nil {
		t.Skip("no Direct3D 11:", err)
	}
	defer r.Release()
	withOther := *s
	withOther.Ops = append(append([]scene.Op(nil), s.Ops...), scene.Op{Kind: scene.OpImage, Rect: scene.Rect{W: 32, H: 32}, Image: other, Src: scene.Rect{W: 64, H: 64}})
	if err := r.draw(&withOther); err != nil {
		t.Fatal(err)
	}
	if err := r.draw(s); err != nil {
		t.Fatal(err)
	}
	want, _ := r.readBack(t)
	want = append([]byte(nil), want...)
	if r.stagingBytes() == 0 || r.images[img.ID()] == nil || r.images[other.ID()] == nil {
		t.Fatal("no staging texture, or no picture's texture")
	}
	r.trim(false)
	if r.images[img.ID()] == nil || r.images[other.ID()] != nil || r.stagingBytes() != 0 {
		t.Fatalf("trimmed while visible: picture drawn kept %v, picture not drawn kept %v, staging %d bytes",
			r.images[img.ID()] != nil, r.images[other.ID()] != nil, r.stagingBytes())
	}
	r.trim(true)
	if len(r.images) != 0 || len(r.masks) != 0 || r.mask.tex != 0 || len(r.b.Instances) != 0 {
		t.Fatalf("trimmed while hidden: %d image textures, %d masks, mask atlas %v", len(r.images), len(r.masks), r.mask.tex)
	}
	if err := r.draw(s); err != nil {
		t.Fatal(err)
	}
	got, _ := r.readBack(t)
	if !bytes.Equal(got, want) {
		t.Error("the frame after the trim draws other pixels")
	}
	r.lastRender = time.Now()
	r.trimmed = false
	r.trimIfIdle() // not idle yet: armed again
	if r.trimmed || !r.trimArmed {
		t.Errorf("trimmed %v, armed %v, right after a frame", r.trimmed, r.trimArmed)
	}
}
