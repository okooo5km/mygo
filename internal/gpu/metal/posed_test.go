//go:build darwin

package metal

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/internal/gpu/gputest"
	"github.com/egoist/mygo/internal/raster"
	"github.com/egoist/mygo/internal/scene"
)

// region says where in the frame a pixel is, for a card with sides x0..x1,
// y0..y1 whose corners reach reach: on a curve of a corner, within a pixel
// of a side the card's clips share, or elsewhere.
func region(x, y int, x0, y0, x1, y1, reach float32) int {
	fx, fy := float32(x), float32(y)
	if (fx < x0+reach || fx > x1-reach-1) && (fy < y0+reach || fy > y1-reach-1) {
		return 0
	}
	near := func(a, b float32) bool { return a-b < 1.5 && b-a < 1.5 }
	if near(fx+0.5, x0) || near(fx+0.5, x1) || near(fy+0.5, y0) || near(fy+0.5, y1) {
		return 1
	}
	return 2
}

// TestPosedCardsDrawAsMasked draws a card under a pose, whose clips are
// cut in the shader, at the scales apps draw at, the card's scale and
// fractional moves, against what it was (its clips cut by masks of the
// transformed clips), and against the CPU renderer's card with its clips
// as the rectangles they come to in the frame (what a stationary card
// is).
//
// Where the mask's coverage differs, it differs from the exact one: a
// transformed clip is rasterized by the CPU's way, which is off by a
// couple of percent at a scaled edge and antialiases a continuous curve
// not as a stationary one is, by up to about 55 of 255 on a pixel or two
// of an arc. The shader's is the stationary card's, so a card does not
// change its look when it comes to rest. The sides the card's clips share
// are antialiased once, not twice as the mask has them. Against the CPU
// renderer's card, elsewhere, an image's interpolation differs a little
// (10 at most, as measured).
func TestPosedCardsDrawAsMasked(t *testing.T) {
	scene.FoldTranslations() // as package ui does
	r, err := newRenderer()
	if err != nil {
		unavailable(t, err)
	}
	defer r.Release()
	var worstMask, worstCPU [3]int
	for _, k := range []float32{1, 2, 2.5} {
		for _, scale := range []float32{1, 1.0136, 1.02} {
			for _, d := range [][2]float32{{0, 0}, {0.37, -1.62}, {0.61, 2.18}, {0.5, 0.5}, {-3.33, 0.07}} {
				name := fmt.Sprintf("k%v-scale%v-move%v,%v", k, scale, d[0], d[1])
				s := gputest.PosedCardScene(k, scale, d[0], d[1], false)
				r.b.NoSimilarClips = true
				want, err := r.renderOffscreen(s)
				if err != nil {
					t.Fatal(err)
				}
				if r.b.MasksDrawn == 0 && (d != [2]float32{} || scale != 1) {
					t.Errorf("%s: the reference drew no masks", name)
				}
				r.b.NoSimilarClips = false
				got, err := r.renderOffscreen(s)
				if err != nil {
					t.Fatal(err)
				}
				if r.b.MasksDrawn != 0 || r.b.MaskPixels != 0 {
					t.Errorf("%s: drew %d masks of %d pixels", name, r.b.MasksDrawn, r.b.MaskPixels)
				}
				cpu := raster.NewImage(s.Width, s.Height)
				raster.Render(cpu, gputest.PosedCardScene(k, scale, d[0], d[1], true))
				// The card's rectangle in the frame, and the corners' reach.
				cx, cy := (30+150)*k, (24+100)*k
				x0, y0 := cx+d[0]-scale*150*k, cy+d[1]-scale*100*k
				x1, y1 := x0+scale*300*k, y0+scale*200*k
				reach := 12*k*scale*1.53 + 3
				w := s.Width * 4
				for y := range s.Height {
					for x := range s.Width {
						a, b, c := got[y*w+4*x:][:4], want[y*w+4*x:][:4], cpu.Pix[y*cpu.Stride+4*x:][:4]
						rg := region(x, y, x0, y0, x1, y1, reach)
						for i := range 4 {
							worstMask[rg] = max(worstMask[rg], abs(int(a[i])-int(b[i])))
							worstCPU[rg] = max(worstCPU[rg], abs(int(a[i])-int(c[i])))
						}
					}
				}
			}
		}
	}
	t.Logf("max difference of the shader's card from the masked one: %d on the corner curves, %d on shared sides, %d elsewhere", worstMask[0], worstMask[1], worstMask[2])
	t.Logf("max difference of the shader's card from the CPU renderer's stationary one: %d on the corner curves, %d on shared sides, %d elsewhere", worstCPU[0], worstCPU[1], worstCPU[2])
	if worstMask[2] > 16 || worstMask[0] > 60 || worstMask[1] > 60 {
		t.Errorf("the card differs from the masked one by %v (curves, shared sides, elsewhere)", worstMask)
	}
	if worstCPU[2] > 12 { // an image's interpolation differs a little
		t.Errorf("away from the corners and the shared sides the card differs from the stationary one by %d", worstCPU[2])
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// A viewport edge tighter than a posed card's, in its pixel, cuts the
// card's row as the mask and the CPU renderer do (a black card on white,
// the row at the viewport's top: coverage about 26 of 255, not 230).
func TestTighterAncestorEdgeCutsTheCard(t *testing.T) {
	r, err := newRenderer()
	if err != nil {
		unavailable(t, err)
	}
	defer r.Release()
	s := tighterEdgeScene()
	r.b.NoSimilarClips = true
	want, _ := r.renderOffscreen(s)
	r.b.NoSimilarClips = false
	got, err := r.renderOffscreen(s)
	if err != nil {
		t.Fatal(err)
	}
	cpu := raster.NewImage(s.Width, s.Height)
	raster.Render(cpu, s)
	w := s.Width * 4
	worstMask, worstCPU := 0, 0
	for x := 45; x < 100; x++ {
		for _, y := range []int{100, 101} {
			g := int(got[y*w+4*x])
			worstMask = max(worstMask, abs(g-int(want[y*w+4*x])))
			worstCPU = max(worstCPU, abs(g-int(cpu.Pix[y*cpu.Stride+4*x])))
		}
	}
	t.Logf("row 100 and 101: %d from the masked output, %d from the CPU renderer's (pixel at row 100: %d)", worstMask, worstCPU, got[100*w+4*60])
	if worstMask > 4 || worstCPU > 4 { // the mask of a scaled clip is a couple of percent off
		t.Errorf("differs by %d from the mask, %d from the CPU", worstMask, worstCPU)
	}
}

func tighterEdgeScene() *scene.Scene {
	m := scene.Matrix(1.02, 0, 0, 1.02, 0, 100.1-1.02*110)
	card := scene.Rect{X: 20, Y: 110, W: 100, H: 100}
	return &scene.Scene{Width: 200, Height: 300, Clear: scene.Color{R: 255, G: 255, B: 255, A: 255}, Ops: []scene.Op{
		{Kind: scene.OpPushClip, Rect: scene.Rect{X: 0, Y: 100.9, W: 200, H: 150}},
		{Kind: scene.OpPushClip, Rect: card, Radii: scene.Uniform(10), Continuous: true, Transform: m},
		{Kind: scene.OpFill, Rect: card, Color: scene.Color{A: 255}, Transform: m},
		{Kind: scene.OpPopClip}, {Kind: scene.OpPopClip}}}
}

// A card scrolled by a fraction of a pixel (scale 1, a move) is cut in the
// shader too, its bottom edge row as the CPU renderer draws it; one on
// whole pixels draws the same bytes as before.
func TestScrolledCardCutsInTheShader(t *testing.T) {
	scene.FoldTranslations()
	r, err := newRenderer()
	if err != nil {
		unavailable(t, err)
	}
	defer r.Release()
	worst := 0
	for _, k := range []float32{1, 2, 2.5} {
		for _, d := range [][2]float32{{0, 0}, {0, 3}, {0.37, -1.62}, {0.61, 2.18}, {0, 0.5}, {-3.33, 0.07}} {
			s := gputest.PosedCardScene(k, 1, d[0], d[1], false)
			r.b.NoSimilarClips = true
			want, _ := r.renderOffscreen(s)
			r.b.NoSimilarClips = false
			got, err := r.renderOffscreen(s)
			if err != nil {
				t.Fatal(err)
			}
			if r.b.MasksDrawn != 0 {
				t.Errorf("k%v move %v: %d masks", k, d, r.b.MasksDrawn)
			}
			if d[0] == float32(int(d[0])) && d[1] == float32(int(d[1])) && string(got) != string(want) {
				t.Errorf("k%v move %v: whole pixels draw other bytes", k, d)
			}
			cpu := raster.NewImage(s.Width, s.Height)
			raster.Render(cpu, s)
			w := s.Width * 4
			y1 := int((24+200)*k + d[1]) // the card's bottom edge row
			for y := y1 - 1; y <= y1+1; y++ {
				for x := int((30 + 40) * k); x < int((30+260)*k); x++ { // between the corner curves
					for c := range 4 {
						worst = max(worst, abs(int(got[y*w+4*x+c])-int(cpu.Pix[y*cpu.Stride+4*x+c])))
					}
				}
			}
		}
	}
	t.Logf("bottom edge rows: at most %d of 255 from the CPU renderer's", worst)
	if worst > 24 {
		t.Errorf("bottom edge differs by %d from the CPU renderer's", worst)
	}
}
