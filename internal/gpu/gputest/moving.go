package gputest

import (
	"image"
	"sync"

	"github.com/egoist/mygo/internal/raster"
	"github.com/egoist/mygo/internal/scene"
)

// movingPicture is the coverage of a rounded rectangle, as package ui
// makes a path clip's mask (Painter.ClipPath): an image whose alpha masks
// a clip of its size, one per scale.
var movingPicture = struct {
	sync.Mutex
	m map[float32]*scene.Image
}{m: map[float32]*scene.Image{}}

func pictureMask(scale float32) *scene.Image {
	movingPicture.Lock()
	defer movingPicture.Unlock()
	if img := movingPicture.m[scale]; img != nil {
		return img
	}
	w, h := int(150*scale)+2, int(100*scale)+2
	shape := scene.ClipShape{Rect: scene.Rect{X: 1, Y: 1, W: float32(w - 2), H: float32(h - 2)}, Radii: scene.Uniform(12 * scale), Continuous: true}
	img := raster.ClipMask(nil, []scene.ClipShape{shape}, image.Rect(0, 0, w, h))
	movingPicture.m[scale] = img
	return img
}

// MovingClipScene is a list of cards that clip as an app's do, scrolled by
// (dx, dy) device pixels at scale: inside a list's rectangle on whole
// pixels, a picture cut by a path's mask, a card whose text fades out at
// the bottom (a gradient's mask), and a card held at a pressed scale, its
// rounded clip transformed; the last card reaches past the list's bottom.
// The same scene moved by whole pixels draws the same pixels moved; the
// clips' masks follow it.
func MovingClipScene(dx, dy, scale float32) *scene.Scene {
	scene.FoldTranslations() // as package ui does
	k := scale
	s := &scene.Scene{Width: int(360 * k), Height: int(520 * k), Clear: scene.Color{R: 246, G: 247, B: 249, A: 255}}
	add := func(ops ...scene.Op) { s.Ops = append(s.Ops, ops...) }
	r := func(x, y, w, h float32) scene.Rect { return scene.Rect{X: x*k + dx, Y: y*k + dy, W: w * k, H: h * k} }
	red, blue, green := scene.Color{R: 220, G: 40, B: 40, A: 255}, scene.Color{R: 40, G: 60, B: 220, A: 255}, scene.Color{G: 160, B: 90, A: 255}
	black, clear := scene.Color{A: 255}, scene.Color{}
	// The list, on whole pixels, which the scissor rectangles cut.
	list := scene.Rect{X: 8 * k, Y: 8 * k, W: 344 * k, H: 504 * k}
	list.X, list.Y, list.W, list.H = float32(int(list.X)), float32(int(list.Y)), float32(int(list.W)), float32(int(list.H))
	add(scene.Op{Kind: scene.OpPushClip, Rect: list})
	// A picture cut by a path's mask, its rectangle on whole pixels in its
	// own coordinates, as ui's ClipPath makes it, moved by the scroll.
	pic := pictureMask(k)
	at := r(20, 20, 0, 0)
	add(scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: float32(int(20*k)) + dx, Y: float32(int(20*k)) + dy, W: float32(pic.W), H: float32(pic.H)},
		Image: pic, Src: scene.Rect{W: float32(pic.W), H: float32(pic.H)}})
	add(scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: at.X, Y: at.Y, W: 160 * k, H: 110 * k}, Color: red},
		scene.Op{Kind: scene.OpFill, Rect: r(60, 40, 80, 50), Radii: scene.Uniform(8 * k), Color: blue})
	add(scene.Op{Kind: scene.OpPopClip})
	// Text fading out at the bottom of a rounded card.
	a := int32(len(s.Stops))
	s.Stops = append(s.Stops, scene.Stop{Color: black, At: 0}, scene.Stop{Color: black, At: 0.7}, scene.Stop{Color: clear, At: 1})
	card := r(190, 20, 150, 120)
	add(scene.Op{Kind: scene.OpPushClip, Rect: card, Paint: scene.PaintLinearStops, Gradient: [4]float32{card.X, card.Y, card.X, card.Y + card.H},
		Stops: [2]int32{a, int32(len(s.Stops))}})
	for i := range 6 {
		add(scene.Op{Kind: scene.OpFill, Rect: r(200, 28+float32(i)*18, 120-float32(i%3)*20, 10), Color: black})
	}
	add(scene.Op{Kind: scene.OpPopClip})
	// A card pressed down, its rounded clip scaled about its middle.
	press := r(20, 160, 320, 140)
	cx, cy := press.X+press.W/2, press.Y+press.H/2
	m := scene.Translation(cx, cy).Mul(scene.Scaling(0.97, 0.97)).Mul(scene.Translation(-cx, -cy))
	add(scene.Op{Kind: scene.OpPushClip, Rect: press, Radii: scene.Uniform(10 * k), Continuous: true, Transform: m})
	add(scene.Op{Kind: scene.OpFill, Rect: press, Color: green, Transform: m},
		scene.Op{Kind: scene.OpFill, Rect: r(30, 170, 140, 120), Color: blue, Transform: m})
	add(scene.Op{Kind: scene.OpPopClip})
	// A faded card reaching past the list's bottom.
	last := r(20, 320, 320, 240)
	add(scene.Op{Kind: scene.OpPushClip, Rect: last, Radii: scene.Uniform(10 * k), Paint: scene.PaintLinearStops,
		Gradient: [4]float32{last.X, last.Y, last.X, last.Y + last.H}, Stops: [2]int32{a, int32(len(s.Stops))}})
	add(scene.Op{Kind: scene.OpFill, Rect: last, Color: red},
		scene.Op{Kind: scene.OpFill, Rect: r(40, 340, 200, 60), Color: blue})
	add(scene.Op{Kind: scene.OpPopClip})
	add(scene.Op{Kind: scene.OpPopClip})
	return s
}

// MovingClipScales are the scales MovingClipScene is drawn at in tests.
var MovingClipScales = []float32{1, 1.5, 2, 2.5}
