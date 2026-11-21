package gpu

import (
	"testing"

	"github.com/egoist/mygo/internal/gpu/gputest"
	"github.com/egoist/mygo/internal/scene"
)

func buildPosed(b *Builder, s *scene.Scene) {
	b.Build(s, func(img *scene.Image) uintptr { return uintptr(img.ID()) })
}

// A card under a pose, whose drawing is scaled and moved by a fraction of a
// pixel in every frame, cut its clips with masks of the whole card, three a
// frame, each rasterized again as its translation changed. A renderer that
// cuts continuous corners in the shader cuts them there: none.
func TestPosedCardClipsAreCutInTheShader(t *testing.T) {
	for _, k := range []float32{1, 2, 2.5} {
		for _, scale := range []float32{1, 1.0136, 1.02} {
			b := Builder{NoCull: true, NoFold: true}
			b.SplitCorners()
			for i, d := range [][2]float32{{0, 0}, {0.37, -1.62}, {0.61, 2.18}, {-3.33, 0.07}, {0.5, 0.5}} {
				s := gputest.PosedCardScene(k, scale, d[0], d[1], false)
				buildPosed(&b, s)
				if b.MasksDrawn != 0 || b.MaskPixels != 0 || len(b.maskSlots) != 0 || len(b.Instances) == 0 {
					t.Fatalf("k %v scale %v frame %d: %d masks of %d pixels, %d kept, %d instances", k, scale, i, b.MasksDrawn, b.MaskPixels, len(b.maskSlots), len(b.Instances))
				}
				for _, in := range b.Instances {
					if in.Transform1[3] != 0 {
						t.Fatalf("k %v scale %v frame %d: a clip mode %v, want 0 (the shader's)", k, scale, i, in.Transform1[3])
					}
				}
				for _, batch := range b.Batches {
					if batch.ClipMask != 0 {
						t.Fatalf("k %v scale %v frame %d: a batch with a clip mask", k, scale, i)
					}
				}
			}
			b.Release()
			// What it was: the same frames with the clips transformed.
			old := Builder{NoCull: true, NoFold: true, NoSimilarClips: true}
			old.SplitCorners()
			buildPosed(&old, gputest.PosedCardScene(k, scale, 0.37, -1.62, false))
			if old.MasksDrawn == 0 {
				t.Errorf("k %v scale %v: the reference drew no masks", k, scale)
			}
			t.Logf("k %v scale %v: masks before %d (%d pixels), after 0", k, scale, old.MasksDrawn, old.MaskPixels)
			old.Release()
		}
	}
}

// The mask stays for clips that are not a uniform scale and a move: turned,
// skewed, or scaled by axis, and in a renderer that does not cut continuous
// corners, which keeps masks for a posed card as it did.
func TestPosedCardKeepsMasksOtherwise(t *testing.T) {
	for _, name := range []string{"rotation", "skew", "axes", "mirror", "no-continuous-corners"} {
		t.Run(name, func(t *testing.T) {
			k := float32(2)
			m := scene.Translation(300, 250).Mul(scene.Rotation(3)).Mul(scene.Translation(-300, -250))
			switch name {
			case "skew":
				m = scene.Matrix(1.01, 0.02, 0.03, 1.01, 3.2, 1.4)
			case "axes":
				m = scene.Matrix(1.02, 0, 0, 1.01, 3.2, 1.4)
			case "mirror":
				m = scene.Matrix(-1.01, 0, 0, 1.01, 700.5, 1.4)
			case "no-continuous-corners":
				m = scene.Matrix(1.02, 0, 0, 1.02, 3.2, 1.4)
			}
			s := gputest.PosedScene(&scene.Scene{Width: int(360 * k), Height: int(300 * k)}, m, false, k)
			b := Builder{NoCull: true, NoFold: true}
			if name != "no-continuous-corners" {
				b.SplitCorners()
			}
			defer b.Release()
			buildPosed(&b, s)
			if b.MasksDrawn == 0 || b.MaskPixels == 0 {
				t.Fatalf("no clip mask drawn")
			}
		})
	}
}

// A posed card's clip is the rounded rectangle it is in the frame: its
// rectangle scaled and moved, its radii scaled, its corners continuous.
func TestPosedCardClipIsRoundedRectInFrame(t *testing.T) {
	k, scale := float32(2), float32(1.0136)
	s := gputest.PosedCardScene(k, scale, 0.37, -1.62, false)
	b := Builder{NoCull: true, NoFold: true}
	b.SplitCorners()
	defer b.Release()
	buildPosed(&b, s)
	op := s.Ops[2] // the card's clip, under the list's
	conv := scene.SimilarClipOp(&op)
	if conv.Transform.Set || conv.Rect.W != op.Rect.W*scale || conv.Radii[0] != op.Radii[0]*scale || !conv.Continuous {
		t.Fatalf("converted clip %+v from %+v", conv, op)
	}
	found := false
	for _, in := range b.Instances {
		if in.Clip == rect(conv.Rect) && in.ClipRadii == scene.Corners(conv.Rect, conv.Radii, true) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no instance is cut by the card's clip in the frame %v", conv.Rect)
	}
}

// A fractional edge of a clip around a posed card that is tighter than the
// card's, in its pixel, must still cut it: the shader takes one edge, so
// the mask does.
func TestTighterAncestorEdgeKeepsMask(t *testing.T) {
	s := TighterEdgeScene()
	b := Builder{NoCull: true, NoFold: true}
	b.SplitCorners()
	defer b.Release()
	buildPosed(&b, s)
	if b.MasksDrawn == 0 {
		t.Fatal("the viewport's tighter edge was dropped, no mask")
	}
}

// TighterEdgeScene is a viewport whose top is at y = 100.9 holding a card
// posed at 1.02 whose top is at 100.1, filled black.
func TighterEdgeScene() *scene.Scene {
	m := scene.Matrix(1.02, 0, 0, 1.02, 0, 100.1-1.02*110)
	card := scene.Rect{X: 20, Y: 110, W: 100, H: 100}
	return &scene.Scene{Width: 200, Height: 300, Clear: scene.Color{R: 255, G: 255, B: 255, A: 255}, Ops: []scene.Op{
		{Kind: scene.OpPushClip, Rect: scene.Rect{X: 0, Y: 100.9, W: 200, H: 150}},
		{Kind: scene.OpPushClip, Rect: card, Radii: scene.Uniform(10), Continuous: true, Transform: m},
		{Kind: scene.OpFill, Rect: card, Color: scene.Color{A: 255}, Transform: m},
		{Kind: scene.OpPopClip}, {Kind: scene.OpPopClip}}}
}
