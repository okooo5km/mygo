package gputest

import "github.com/egoist/mygo/internal/scene"

var posedImage = func() *scene.Image {
	pix := make([]byte, 0, 8*8*4)
	for y := range 8 {
		for x := range 8 {
			if (x+y)%2 == 0 {
				pix = append(pix, 250, 190, 40, 255)
			} else {
				pix = append(pix, 40, 120, 230, 255)
			}
		}
	}
	return scene.NewImageRGBA(8, 8, pix)
}()

// PosedCardScene is a card under a pose, as an app draws a hovered or
// lifted one: everything in it drawn under one transform, a uniform scale
// of scale about its middle and a move of (tx, ty) device pixels, at
// device scale k. Its clips are those of a clipboard card: a continuous
// rounded one, a body (ClipY) and a meta line (ClipX) that share its sides,
// and a picture block in its corner with square corners; an image
// and fills inside.
//
// With converted, the clips are the rectangles they come to in the frame
// (scene.SimilarClipOp), without a transform, and the drawing keeps it.
func PosedCardScene(k, scale, tx, ty float32, converted bool) *scene.Scene {
	s := &scene.Scene{Width: int(360 * k), Height: int(300 * k), Clear: scene.Color{R: 246, G: 247, B: 249, A: 255}}
	r := func(x, y, w, h float32) scene.Rect { return scene.Rect{X: x * k, Y: y * k, W: w * k, H: h * k} }
	card := r(30, 24, 300, 200)
	cx, cy := card.X+card.W/2, card.Y+card.H/2
	m := scene.Translation(cx+tx, cy+ty).Mul(scene.Scaling(scale, scale)).Mul(scene.Translation(-cx, -cy))
	return PosedScene(s, m, converted, k)
}

// PosedScene fills s with the card of PosedCardScene under m.
func PosedScene(s *scene.Scene, m scene.Affine, converted bool, k float32) *scene.Scene {
	r := func(x, y, w, h float32) scene.Rect { return scene.Rect{X: x * k, Y: y * k, W: w * k, H: h * k} }
	card := r(30, 24, 300, 200)
	add := func(ops ...scene.Op) { s.Ops = append(s.Ops, ops...) }
	clip := func(rect scene.Rect, radius float32) {
		op := scene.Op{Kind: scene.OpPushClip, Rect: rect, Transform: m}
		if radius > 0 {
			op.Radii, op.Continuous = scene.Uniform(radius*k), true
		}
		if converted {
			op = scene.SimilarClipOp(&op)
		}
		add(op)
	}
	fill := func(rect scene.Rect, c scene.Color) {
		add(scene.Op{Kind: scene.OpFill, Rect: rect, Color: c, Transform: m})
	}
	pop := scene.Op{Kind: scene.OpPopClip}
	add(scene.Op{Kind: scene.OpPushClip, Rect: r(4, 4, 352, 292)})
	fill(card, scene.Color{R: 255, G: 255, B: 255, A: 255})
	clip(card, 12)
	fill(r(30, 24, 300, 90), scene.Color{R: 90, G: 150, B: 100, A: 255})
	const far = 1e6
	clip(scene.Rect{X: -far, Y: card.Y, W: 2 * far, H: card.H}, 0) // the body, ClipY: the card's top and bottom
	fill(r(40, 130, 220, 10), scene.Color{A: 255})
	fill(r(40, 150, 180, 10), scene.Color{R: 80, G: 80, B: 80, A: 255})
	clip(scene.Rect{X: card.X, Y: -far, W: card.W, H: 2 * far}, 0) // the meta line, ClipX: the card's left and right
	fill(r(30, 196, 300, 28), scene.Color{R: 220, G: 40, B: 40, A: 200})
	add(pop)
	clip(r(30, 24, 110, 80), 0) // a block in the corner
	add(scene.Op{Kind: scene.OpImage, Rect: r(30, 24, 110, 80), Image: posedImage, Src: scene.Rect{W: 8, H: 8}, Transform: m})
	add(pop, pop, pop, pop)
	return s
}
