package gpu

import (
	"cmp"
	"image"
	"math"
	"slices"
	"time"

	"github.com/egoist/mygo/internal/raster"
	"github.com/egoist/mygo/internal/scene"
)

// clipMaskSlot is a retained clip mask. Masks outlive the frames that
// don't use them, up to idleMaskPixels of them, so a transform that comes
// and goes finds its masks again instead of rasterizing them.
//
// A mask is of its clips' own shape, not of where they are in the frame:
// shapes are the clips as the mask has them, moved so that the mask's
// pixel (0, 0) is the frame's pixel origin, wherever that is (maskPlace).
// A clip that only moves by whole pixels, as a card scrolled with its
// list or a pane sliding in, finds the mask it had, which the shaders
// sample at its new place. Its stops are in stops, and w×h are the pixels
// it covers, of its texture.
type clipMaskSlot struct {
	shapes []scene.ClipShape
	stops  []scene.Stop
	w, h   int
	// tex is the texture of the mask that MaskTextures made, or, without
	// them, image holds its coverage in alpha, which the renderer uploads
	// as any image.
	tex   uintptr
	image *scene.Image
	// frame is the last frame that used the mask, and slot the clip's
	// place among that frame's masked clips.
	frame uint64
	slot  int
}

// MaskTextures makes the textures of clip masks from their coverage, one
// byte a pixel, for a renderer that keeps them in single-channel textures
// (A8): a quarter of the memory of an RGBA image. Without it the builder
// hands masks to the renderer as images, their coverage in alpha.
type MaskTextures interface {
	// MaskTexture returns a texture of w×h holding pix, rows of w bytes:
	// tex, which a mask of that size had, refilled, or a new one, which
	// the builder frees with FreeMask; 0 when it cannot.
	MaskTexture(tex uintptr, w, h int, pix []byte) uintptr
	// FreeMask frees a texture MaskTexture returned.
	FreeMask(tex uintptr)
}

// idleMaskPixels is how many pixels of the masks no clip of the frame
// uses are kept, the least recently used dropped first: 8 MB of
// single-channel textures.
const idleMaskPixels = 8 << 20

// maskScratchKeep is the most bytes of coverage the builder keeps between
// frames to rasterize masks into; a larger buffer goes after a frame that
// rasterized none.
const maskScratchKeep = 1 << 20

// maskSlack is how many pixels a mask may cover beyond twice its clip's
// scissor rectangle (maskPlace): its margin around a small clip.
const maskSlack = 64 * 64

// maskMargin is how many pixels a mask reaches beyond its clips' bounds:
// continuous corners antialias up to 3 out (raster.ClipMask), and one more
// is transparent all round, which sampling beyond the texture repeats.
const maskMargin = 4

func (b *Builder) prepare(in *Instance, m scene.Affine, c *clip) bool {
	b.preparedMask = 0
	if c.scissor.Empty() || in.Rect[2] <= 0 || in.Rect[3] <= 0 {
		return false
	}
	if _, ok := m.Inverse(); !ok {
		return false
	}
	in.Transform0, in.Transform1, in.Transform2 = [4]float32{1, 0, 0, 0}, [4]float32{0, 1, 0, 0}, [4]float32{}
	if m.Set {
		in.Transform0 = [4]float32{m.A, m.C, m.X, 1}
		in.Transform1 = [4]float32{m.B, m.D, m.Y, 0}
		in.Transform2 = [4]float32{m.G, m.H, 0, 0}
		if m.Behind {
			in.Transform2[2] = 1 // the divisor's constant term is -1
		}
	}
	in.Clip, in.ClipRadii = rect(c.rect), c.radii
	if !c.masked {
		return true
	}
	// Stationary clips can often reduce to one rounded rectangle, even
	// around scaled content or a fractional scrolling viewport. Evaluate
	// that geometry in the shader instead of rasterizing a window-sized mask.
	if !c.moved && !c.masks {
		if rounded, ok := b.analyticClip(); ok {
			in.Clip, in.ClipRadii = rect(rounded.rect), rounded.radii
			return true
		}
	}
	// A hard clip sharing the drawing's coordinates is cheap to evaluate in
	// the fragment shader. Keep the rounded ancestor's mask stationary even
	// when a text input or custom clipped drawing moves within it. Drawing
	// that a translation of the clip's moved shares them too, folded into
	// the frame's (scene.TranslationFolder): the clip's rectangle moves.
	// So does drawing that nothing transforms, or that a translation only
	// moves, in a clip in the frame's coordinates (as a clip only moved
	// is, scene.MovedClip), whose rectangle moves the other way: as a
	// pane reaching into the rounded corners of the frame around it
	// (markEnclosing), which then needs no mask of its own.
	shared, dx, dy := c.shape.Transform == m && m.Set, float32(0), float32(0)
	if tx, ty, moved := m.Translation(); !c.shape.Transform.Set && (!m.Set || moved) {
		dx, dy, shared = -tx, -ty, true
	}
	if c.shape.Radii == [4]float32{} && !c.shape.Masks() && shared && len(b.stack) > 1 {
		parent := &b.stack[len(b.stack)-2]
		if parent.masked {
			b.preparedMask = b.clipTexture(parent, b.clipShapes[:len(b.clipShapes)-1])
			if b.preparedMask == 0 {
				return false
			}
			in.Clip = rect(c.rect)
			in.Clip[0], in.Clip[1] = in.Clip[0]+dx, in.Clip[1]+dy
			in.ClipRadii = areaRect(parent.maskArea)
			in.Transform1[3] = 2
			return true
		}
	}
	b.preparedMask = b.clipTexture(c, b.clipShapes)
	if b.preparedMask == 0 {
		return false
	}
	in.Clip = areaRect(c.maskArea)
	in.ClipRadii = [4]float32{}
	in.Transform1[3] = 1
	return true
}

// analyticClip removes clips that cover every pixel of the scissor, then
// represents the remaining coverage with one rectangle and its corners.
// Overlapping antialiased edges still need their multiplied pixel mask.
func (b *Builder) analyticClip() (clip, bool) {
	var rounded, hard *clip
	var merged clip
	sc := b.stack[len(b.stack)-1].scissor
	area := scene.Rect{X: float32(sc.Left), Y: float32(sc.Top), W: float32(sc.Right - sc.Left), H: float32(sc.Bottom - sc.Top)}
	// On Metal, a card's clips have fractional edges, posed (clip.posed) or
	// scrolled by a fraction of a pixel, and the body in it shares the
	// card's sides: the mask multiplies their coverage, and the shader
	// takes it once. A side no tighter than the card's is the card's;
	// several hard clips are one rectangle where their sides agree.
	loose := b.contClips && !b.NoSimilarClips
	for i := range b.stack {
		c := &b.stack[i]
		// Only Metal's shader evaluates continuous corners (SplitCorners):
		// the other renderers, which never draw them in an app, keep the
		// mask. On Metal, a pane against the side of a rounded frame, as a
		// sidebar, cuts the frame's curves in the shader rather than with a
		// mask of the whole frame (#1511).
		if continuous(c.radii) && !b.contClips {
			return clip{}, false
		}
		if c.radii == [4]float32{} && integerClip(c.rect) {
			continue // the batch scissor implements these edges exactly
		}
		if clipOpaqueWithin(c, area) {
			continue
		}
		switch {
		case c.radii != [4]float32{}:
			if rounded != nil {
				return clip{}, false
			}
			rounded = c
		case hard == nil:
			hard = c
		case !loose:
			return clip{}, false
		default:
			// Hard clips are rectangles: the intersection of several is
			// one, where no two sides differ within a pixel.
			if !sidesAgree(hard.rect, c.rect) {
				return clip{}, false
			}
			merged = clip{rect: hard.rect.Intersect(c.rect)}
			if merged.rect.W <= 0 || merged.rect.H <= 0 {
				return clip{}, false
			}
			hard = &merged
		}
	}
	if rounded == nil {
		if hard != nil {
			return *hard, true
		}
		return clip{rect: area}, true
	}
	if hard != nil {
		return intersectAnalyticClip(*rounded, hard.rect, loose)
	}
	return *rounded, true
}

// sharedEdge is how far apart two edges may be to be the same one.
const sharedEdge = 1e-3

// sidesAgree reports whether no side of a differs from b's by more than
// sharedEdge within one pixel: those are antialiased by both, and a mask
// multiplies their coverage.
func sidesAgree(a, b scene.Rect) bool {
	ae, be := [4]float32{a.X, a.Y, a.X + a.W, a.Y + a.H}, [4]float32{b.X, b.Y, b.X + b.W, b.Y + b.H}
	for i := range ae {
		if d := ae[i] - be[i]; d <= sharedEdge && d >= -sharedEdge {
			continue
		}
		if i < 2 && math.Floor(float64(ae[i])) == math.Floor(float64(be[i])) || i >= 2 && math.Ceil(float64(ae[i])) == math.Ceil(float64(be[i])) {
			return false
		}
	}
	return true
}

func integerClip(r scene.Rect) bool {
	for _, v := range [4]float32{r.X, r.Y, r.X + r.W, r.Y + r.H} {
		if v != float32(math.Floor(float64(v))) {
			return false
		}
	}
	return true
}

// clipCornerBounds conservatively encloses each curve and its
// antialiasing: a continuous one (negative radius) reaches contExtent
// times its radius from its corner, where the shader stops evaluating it
// (contValue).
func clipCornerBounds(c *clip) [4]scene.Rect {
	r := c.rect
	var boxes [4]scene.Rect
	for i, radius := range c.radii {
		if radius == 0 {
			continue
		}
		reach := radius + 1
		if radius < 0 {
			reach = -radius*contExtent + 1
		}
		box := scene.Rect{X: r.X, Y: r.Y, W: reach, H: reach}
		if i == 1 || i == 2 {
			box.X = r.X + r.W - reach
		}
		if i == 2 || i == 3 {
			box.Y = r.Y + r.H - reach
		}
		boxes[i] = box
	}
	return boxes
}

// clipOpaqueWithin proves that a clip contributes coverage 1 to every
// integer pixel box in area; merely containing the pixel centers is unsafe.
func clipOpaqueWithin(c *clip, area scene.Rect) bool {
	r := c.rect
	if area.X < r.X || area.Y < r.Y || area.X+area.W > r.X+r.W || area.Y+area.H > r.Y+r.H {
		return false
	}
	for _, box := range clipCornerBounds(c) {
		if !box.Intersect(area).Empty() {
			return false
		}
	}
	return true
}

// intersectAnalyticClip trims away whole corners, as a fractional lyrics
// viewport trims its player card. Cutting a curve or multiplying partial
// coverages cannot be represented by a single rounded rectangle.
//
// Loose is for renderers that cut continuous corners (Metal): a side of the
// hard clip that shares its pixel with the card's and is no tighter than
// it (the body's sides are the card's), is the card's alone, which the
// shader antialiases once where the mask multiplies the two. A tighter
// one in that pixel still needs the mask.
func intersectAnalyticClip(c clip, hard scene.Rect, loose bool) (clip, bool) {
	before := [4]float32{c.rect.X, c.rect.Y, c.rect.X + c.rect.W, c.rect.Y + c.rect.H}
	edges := [4]float32{hard.X, hard.Y, hard.X + hard.W, hard.Y + hard.H}
	after := before
	for i, edge := range edges {
		old := float64(before[i])
		var shared bool
		if i < 2 {
			shared = edge <= before[i] && float64(edge) > math.Floor(old) || edge > before[i] && math.Floor(float64(edge)) < math.Ceil(old)
		} else {
			shared = edge >= before[i] && float64(edge) < math.Ceil(old) || edge < before[i] && math.Ceil(float64(edge)) > math.Floor(old)
		}
		switch {
		case shared && loose && (i < 2 && edge <= before[i]+sharedEdge || i >= 2 && edge >= before[i]-sharedEdge):
			// the hard edge is as loose as the card's, or the same:
			// the card's edge stands
		case shared:
			// a tighter edge in the pixel of the card's, which only
			// the product of the two coverages gets right
			return clip{}, false
		case i < 2:
			after[i] = max(before[i], edge)
		default:
			after[i] = min(before[i], edge)
		}
	}
	r := scene.Rect{X: after[0], Y: after[1], W: after[2] - after[0], H: after[3] - after[1]}
	if r.W < 1 || r.H < 1 {
		return clip{}, false
	}
	pixels := scene.Rect{X: float32(math.Floor(float64(r.X))), Y: float32(math.Floor(float64(r.Y)))}
	pixels.W, pixels.H = float32(math.Ceil(float64(r.X+r.W)))-pixels.X, float32(math.Ceil(float64(r.Y+r.H)))-pixels.Y
	for i, box := range clipCornerBounds(&c) {
		sides := [4][2]int{{0, 1}, {2, 1}, {2, 3}, {0, 3}}[i]
		if after[sides[0]] != before[sides[0]] || after[sides[1]] != before[sides[1]] {
			if !box.Intersect(pixels).Empty() {
				return clip{}, false
			}
			c.radii[i] = 0
		} else if box.W > r.W/2 || box.H > r.H/2 {
			// Moving a surviving curve across the new midpoint would change
			// which corner the shader evaluates there. A continuous curve
			// keeps its shape too, which depends on the sides and its
			// neighbors' radii only where they are shorter than
			// contExtent times the radii they join (the shader's
			// contCorner): a side the cut leaves is as long as before, with
			// the same neighbor, and one it shortens is still twice this
			// box long, its neighbor cut away beyond it.
			return clip{}, false
		}
	}
	c.rect = r
	return c, true
}

func (b *Builder) clipTexture(c *clip, shapes []scene.ClipShape) uintptr {
	if c.maskTexture != 0 {
		return c.maskTexture
	}
	area := image.Rect(int(c.scissor.Left), int(c.scissor.Top), int(c.scissor.Right), int(c.scissor.Bottom))
	place, kept := b.maskPlace(area, shapes)
	w, h := place.Dx(), place.Dy()
	// The mask of the same shapes, of this frame or an earlier one, however
	// they moved by whole pixels; else the one the clip in its place had
	// last frame, which it most likely is, changed; else a new one.
	found, reuse := -1, -1
	for i := range b.maskSlots {
		m := &b.maskSlots[i]
		// A mask of more than the clip needs holds the same coverage where
		// it does: each pixel's is of the shapes alone.
		if w <= m.w && h <= m.h && slices.EqualFunc(m.shapes, kept, func(a, b scene.ClipShape) bool { return a.Same(&b) }) {
			found = i
			break
		}
		if reuse < 0 && m.frame == b.frame-1 && m.slot == c.maskSlot {
			reuse = i
		}
	}
	if found >= 0 {
		b.MasksReused++
	} else {
		found = reuse
		if found < 0 {
			found = len(b.maskSlots)
			b.maskSlots = append(b.maskSlots, clipMaskSlot{})
		}
		m := &b.maskSlots[found]
		frame := b.stack[0].scissor
		mw, mh := maskSize(w, h, m.w, m.h, int(frame.Right-frame.Left), int(frame.Bottom-frame.Top))
		m.keep(kept)
		if !b.rasterize(m, mw, mh) {
			b.dropSlot(found)
			return 0
		}
	}
	m := &b.maskSlots[found]
	m.frame, m.slot = b.frame, c.maskSlot
	c.maskArea = image.Rect(place.Min.X, place.Min.Y, place.Min.X+m.w, place.Min.Y+m.h)
	if m.tex != 0 {
		c.maskTexture = m.tex
	} else {
		c.maskTexture = b.imageTexture(m.image)
	}
	return c.maskTexture
}

// maskPlace returns the frame pixels the mask of shapes covers, for a
// clip whose scissor rectangle is area, and the shapes the mask is of,
// moved so that the mask starts at the frame's origin. Shapes that the
// scissor rectangle cuts exactly (a rectangle on whole pixels, nothing
// transforming or masking it) and those letting everything through
// within area are left out: they change nothing where the mask is read.
// The mask covers what the other shapes can let through, with a margin,
// rather than area alone, so that it holds as a clip moves within the
// scissor rectangles; unless that is more than twice area (and
// maskSlack), as for a clip partly scrolled out of view or around a long
// page, when it covers area alone.
func (b *Builder) maskPlace(area image.Rectangle, shapes []scene.ClipShape) (image.Rectangle, []scene.ClipShape) {
	kept := b.maskShapes[:0]
	sa := scene.Rect{X: float32(area.Min.X), Y: float32(area.Min.Y), W: float32(area.Dx()), H: float32(area.Dy())}
	for i := range shapes {
		sh := &shapes[i]
		if !sh.Transform.Set && !sh.Masks() {
			if sh.Radii == [4]float32{} && integerClip(sh.Rect) {
				continue
			}
			if c := (clip{rect: sh.Rect, radii: scene.Corners(sh.Rect, sh.Radii, sh.Continuous)}); clipOpaqueWithin(&c, sa) {
				continue
			}
		}
		kept = append(kept, *sh)
	}
	if len(kept) == 0 {
		// Every pixel of area is let through: the innermost clip's mask
		// holds that.
		kept = append(kept, shapes[len(shapes)-1])
	}
	place := area
	bounds := scene.Rect{X: -1e9, Y: -1e9, W: 2e9, H: 2e9}
	for i := range kept {
		bounds = bounds.Intersect(kept[i].Transform.Bounds(kept[i].Rect))
	}
	if bounds.W > 0 && bounds.H > 0 && bounds.W < 1e6 && bounds.H < 1e6 {
		own := image.Rect(int(math.Floor(float64(bounds.X)))-maskMargin, int(math.Floor(float64(bounds.Y)))-maskMargin,
			int(math.Ceil(float64(bounds.X+bounds.W)))+maskMargin, int(math.Ceil(float64(bounds.Y+bounds.H)))+maskMargin)
		// Not much more than area: a clip that only peeks into view (a
		// card at the list's edge) would rasterize all of itself in every
		// frame it moves by a fraction of a pixel, for the part that shows.
		if own.Dx()*own.Dy() <= 2*area.Dx()*area.Dy()+maskSlack {
			place = own
		}
	}
	for i := range kept {
		kept[i] = maskLocal(kept[i], place.Min.X, place.Min.Y)
	}
	b.maskShapes = kept
	return place, kept
}

// maskLocal returns c as the mask starting at the frame's pixel (x, y) has
// it: moved by (-x, -y). A clip without a transform has its places
// snapped to 1/maskGrid of a pixel, so that the same shape moved by whole
// pixels is the same mask though float32 rounded the sums that placed it
// differently: what is rasterized differs from the exact places by
// 1/(2*maskGrid) of a pixel at most, far under what a level of coverage
// takes. A transformed clip is moved by its transform, exactly (its
// coverage depends on where its rectangle lies under the transform, by a
// few levels at continuous corners, so it is not put in another form).
func maskLocal(c scene.ClipShape, x, y int) scene.ClipShape {
	if c.Transform.Set {
		return c.Moved(float32(-x), float32(-y))
	}
	snap := func(v float64) float32 { return float32(math.Round(v*maskGrid) / maskGrid) }
	g := scene.MoveGradient(c.Paint, c.Gradient, float32(-x), float32(-y))
	for i, v := range g {
		if g[i] != c.Gradient[i] {
			g[i] = snap(float64(v))
		}
	}
	c.Gradient = g
	c.Rect.X, c.Rect.Y = snap(float64(c.Rect.X)-float64(x)), snap(float64(c.Rect.Y)-float64(y))
	return c
}

// maskGrid is how many places a pixel has for a mask's shapes (maskLocal).
const maskGrid = 4096

// maskSize returns the size of the mask of w×h pixels that a slot holding
// one of had×hadH takes: a new mask its own; otherwise the size it had,
// so that a clip that changes reuses its mask's memory and texture instead
// of allocating both again on every frame of its animation. A card growing
// out of a cover to fill the window allocated 190 MB in 40 frames
// (egoist/mygo-music#55). A side that outgrows it doubles, up to the
// frame's size, and a clip that shrinks to less than a quarter of it
// takes its own size.
func maskSize(w, h, had, hadH, fw, fh int) (int, int) {
	if had <= 0 || hadH <= 0 || 4*w*h < had*hadH {
		return w, h
	}
	grow := func(n, was, most int) int {
		if n <= was {
			return was
		}
		return max(n, min(2*was, most))
	}
	return grow(w, had, fw), grow(h, hadH, fh)
}

// rasterize makes m's mask of mw×mh pixels from its shapes, into its
// texture or its image, and reports whether it could.
func (b *Builder) rasterize(m *clipMaskSlot, mw, mh int) bool {
	n := mw * mh
	b.MasksDrawn++
	b.MaskPixels += n
	b.maskDrawn = true
	if Timing {
		t0 := time.Now()
		defer func() { b.MaskTime += time.Since(t0) }()
	}
	if b.MaskTextures == nil {
		if m.image == nil {
			m.image = scene.NewImageRGBA(mw, mh, make([]byte, 4*n))
		} else if m.image.W != mw || m.image.H != mh {
			m.image.Resize(mw, mh)
		}
		m.image = raster.ClipMask(m.image, m.shapes, image.Rect(0, 0, mw, mh))
		m.w, m.h = mw, mh
		return true
	}
	if cap(b.maskPix) < n {
		b.maskPix = make([]byte, n)
	}
	pix := b.maskPix[:n]
	var r0 time.Time
	if Timing {
		r0 = time.Now()
	}
	raster.ClipMaskAlpha(pix, m.shapes, image.Rect(0, 0, mw, mh))
	if Timing {
		b.MaskRaster += time.Since(r0)
	}
	had := m.tex
	if m.w != mw || m.h != mh {
		had = 0
	}
	tex := b.MaskTextures.MaskTexture(had, mw, mh, pix)
	if m.tex != 0 && m.tex != tex {
		b.MaskTextures.FreeMask(m.tex)
	}
	m.tex, m.w, m.h = tex, mw, mh
	return tex != 0
}

// dropSlot frees mask slot i and removes it.
func (b *Builder) dropSlot(i int) {
	b.freeSlot(&b.maskSlots[i])
	b.maskSlots = slices.Delete(b.maskSlots, i, i+1)
}

// freeSlot frees the texture of m that MaskTextures made.
func (b *Builder) freeSlot(m *clipMaskSlot) {
	if m.tex != 0 && b.MaskTextures != nil {
		b.MaskTextures.FreeMask(m.tex)
	}
	m.tex = 0
}

// areaRect returns area, a mask's frame pixels, as the shaders take it:
// its origin and size, which map frame pixels to the mask's texels.
func areaRect(area image.Rectangle) [4]float32 {
	return [4]float32{float32(area.Min.X), float32(area.Min.Y), float32(area.Dx()), float32(area.Dy())}
}

// keep keeps shapes as the mask's, with copies of their stops, which
// the scene's are not from frame to frame.
func (m *clipMaskSlot) keep(shapes []scene.ClipShape) {
	m.shapes = append(m.shapes[:0], shapes...)
	m.stops = m.stops[:0]
	for _, c := range shapes {
		m.stops = append(m.stops, c.Stops...)
	}
	at := 0
	for i := range m.shapes {
		if st := m.shapes[i].Stops; st != nil {
			m.shapes[i].Stops = m.stops[at : at+len(st) : at+len(st)]
			at += len(st)
		}
	}
}

// dropIdleMasks drops the least recently used masks this frame didn't use
// beyond idle pixels of them, and the scratch the frame's masks were
// rasterized in once it is large and no mask was.
func (b *Builder) dropIdleMasks(idle int) {
	if !b.maskDrawn && cap(b.maskPix) > maskScratchKeep {
		b.maskPix = nil
	}
	b.maskDrawn = false
	slices.SortFunc(b.maskSlots, func(x, y clipMaskSlot) int { return cmp.Compare(y.frame, x.frame) })
	for i := range b.maskSlots {
		m := &b.maskSlots[i]
		if m.frame == b.frame {
			continue
		}
		// The memory a mask keeps, which may be more than its pixels
		// take up: Resize reuses it for smaller ones.
		kept := m.w * m.h
		if m.image != nil {
			kept = cap(m.image.Pix) / 4
		}
		if idle -= kept; idle < 0 {
			for j := i; j < len(b.maskSlots); j++ {
				b.freeSlot(&b.maskSlots[j])
			}
			clear(b.maskSlots[i:])
			b.maskSlots = b.maskSlots[:i]
			return
		}
	}
}

// releaseMasks frees every mask's texture.
func (b *Builder) releaseMasks() {
	for i := range b.maskSlots {
		b.freeSlot(&b.maskSlots[i])
	}
}
