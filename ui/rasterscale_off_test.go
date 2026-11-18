package ui

import (
	"testing"
	"time"
)

// poseLayerDraws animates a kept card's pose 1 -> 1.02 -> 1 and returns how
// many times its layer was drawn again after its first frame, and the scale it ended at.
func poseLayerDraws(t *testing.T) (draws int, scale float32) {
	t.Helper()
	var peak bool
	var card *node
	tt := newScaleTester(func(c *context) {
		p := Pose{}
		if peak {
			p = Pose{Scale: 1.02}
		}
		card = scaleCard(c, 1).Cache().Absolute().Left(20).Top(20).
			AnimatePose(p, Animation{Duration: 120 * time.Millisecond, Ease: Linear})
	}, 600, 260, 1)
	layer := card.st.effectLayer.img
	v := layer.Version()
	run := func() {
		tt.rt.requestFrame()
		tt.frame(0)
		for i, more := 0, true; more; i++ {
			if i > 60 {
				t.Fatal("the animation does not end")
			}
			if layer.Version() != v {
				draws++
				v = layer.Version()
			}
			more = tt.frame(8 * time.Millisecond)
		}
		if layer.Version() != v {
			draws++
			v = layer.Version()
		}
	}
	peak = true
	run()
	peak = false
	run()
	return draws, layer.Scene.Scale
}

// TestSetRasterAtShownScale: with SetRasterAtShownScale(false), a Cache layer
// whose pose scales 1 -> 1.02 -> 1 is drawn once (in its first frame, never
// again), at the window's scale, as before v0.10.0; on, it is drawn again at the buckets and at the exact scale
// of each rest.
func TestSetRasterAtShownScale(t *testing.T) {
	t.Cleanup(func() { SetRasterAtShownScale(true) })
	SetRasterAtShownScale(false)
	off, offScale := poseLayerDraws(t)
	SetRasterAtShownScale(true)
	on, _ := poseLayerDraws(t)
	t.Logf("layer redrawn %d times off, %d on", off, on)
	if off != 0 || offScale != 1 {
		t.Errorf("off: layer redrawn %d times, at scale %v: want no redraw, at the window's scale", off, offScale)
	}
	if on <= off {
		t.Errorf("on: layer redrawn %d times, off %d: want more", on, off)
	}
}

// TestSetRasterAtShownScaleKeepsTheOldTextPath: off, text under a scaling
// pose rasterizes at the window's scale (no screen-space glyphs, no scale
// watch kept).
func TestSetRasterAtShownScaleKeepsTheOldTextPath(t *testing.T) {
	SetRasterAtShownScale(false)
	t.Cleanup(func() { SetRasterAtShownScale(true) })
	var text *node
	tt := newScaleTester(func(c *context) {
		coreBox(c).Absolute().Left(40).Top(40).TransformOrigin(0, 0).Scale(1.6, 1.6).Children(func() {
			text = coreText(c, "Word by word").FontSize(18)
		})
	}, 300, 140, 1)
	if text.st.scales != nil {
		t.Error("off: the text kept a scale watch")
	}
	if q, screen := rasterScaleOf(t, &tt.rt.scene, 1.6); q != 1 || screen {
		t.Errorf("off: text rasterized at %v (frame's pixels %v), want the window's scale", q, screen)
	}
}
