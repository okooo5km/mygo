package gpu

import (
	"fmt"
	"image"
	"testing"

	"github.com/egoist/mygo/internal/gpu/gputest"
	"github.com/egoist/mygo/internal/raster"
	"github.com/egoist/mygo/internal/scene"
)

// fakeMasks is a MaskTextures that keeps each texture's pixels.
type fakeMasks struct {
	next  uintptr
	live  map[uintptr][]byte
	sizes map[uintptr][2]int
	made  int
}

func (f *fakeMasks) MaskTexture(tex uintptr, w, h int, pix []byte) uintptr {
	if f.live == nil {
		f.live, f.sizes = map[uintptr][]byte{}, map[uintptr][2]int{}
	}
	if tex == 0 {
		f.next++
		tex = f.next
		f.made++
	}
	f.live[tex] = append([]byte(nil), pix...)
	f.sizes[tex] = [2]int{w, h}
	return tex
}

func (f *fakeMasks) FreeMask(tex uintptr) {
	if _, ok := f.live[tex]; !ok {
		panic(fmt.Sprintf("freed texture %d twice or never made", tex))
	}
	delete(f.live, tex)
}

// TestMovingClipMasksFollowTheirClips builds gputest.MovingClipScene
// scrolled by whole pixels, at the scales apps draw at: the masks are
// rasterized once, in one channel, and the texels the shaders read for
// each masked instance hold, within a level, the coverage of its clips
// where they are now; scrolled by a fraction of a pixel, the masks are
// rasterized again and hold it too. Release frees every texture.
func TestMovingClipMasksFollowTheirClips(t *testing.T) {
	for _, k := range gputest.MovingClipScales {
		t.Run(fmt.Sprint(k), func(t *testing.T) {
			var b Builder
			masks := &fakeMasks{}
			b.MaskTextures = masks
			for i, dy := range []float32{0, -7, -60, -61, -0.5, -0.5, -12.25} {
				s := gputest.MovingClipScene(0, dy, k)
				b.Build(s, func(*scene.Image) uintptr { t.Fatal("a mask went through the images"); return 0 })
				whole := i > 0 && dy == float32(int(dy)) || i == 5
				// The pressed card's clip scales about its middle, which
				// moves with it: its transform is another, and so its mask.
				if whole && (b.MasksDrawn > 1 || b.MasksReused == 0) {
					t.Errorf("scrolled %v: rasterized %d masks, reused %d", dy, b.MasksDrawn, b.MasksReused)
				}
				if !whole && i > 0 && b.MasksDrawn == 0 {
					t.Errorf("scrolled %v: rasterized no mask", dy)
				}
				checkMasks(t, &b, s, masks, fmt.Sprintf("scrolled %v", dy))
			}
			b.Release()
			if len(masks.live) != 0 {
				t.Errorf("%d mask textures left after Release", len(masks.live))
			}
		})
	}
}

// checkMasks checks the coverage the instances of s that sample a mask
// read against the coverage of their clips in the frame.
func checkMasks(t *testing.T, b *Builder, s *scene.Scene, masks *fakeMasks, name string) {
	t.Helper()
	var clips []scene.ClipShape
	for i, op := range s.Ops {
		switch op.Kind {
		case scene.OpPushClip:
			clips = append(clips, scene.ClipShapeOf(&s.Ops[i], s.Stops))
			continue
		case scene.OpPopClip:
			clips = clips[:len(clips)-1]
			continue
		}
		for j := b.OpStart[i]; j < b.OpStart[i+1]; j++ {
			in := b.Instances[j]
			if in.Transform1[3] != 1 {
				continue
			}
			var bt *Batch
			for n := range b.Batches {
				if x := &b.Batches[n]; x.Start <= int(j) && int(j) < x.Start+x.Count {
					bt = x
				}
			}
			pix, size := masks.live[bt.ClipMask], masks.sizes[bt.ClipMask]
			if pix == nil || size != [2]int{int(in.Clip[2]), int(in.Clip[3])} {
				t.Fatalf("%s: op %d samples a mask of %v, its texture is %v", name, i, in.Clip, size)
			}
			sc := bt.Scissor
			area := image.Rect(int(sc.Left), int(sc.Top), int(sc.Right), int(sc.Bottom))
			want := raster.ClipMask(nil, clips, area)
			worst, wx, wy, wg, ww := 0, 0, 0, byte(0), byte(0)
			for y := area.Min.Y; y < area.Max.Y; y++ {
				for x := area.Min.X; x < area.Max.X; x++ {
					u, v := x-int(in.Clip[0]), y-int(in.Clip[1])
					got := byte(0)
					if u >= 0 && v >= 0 && u < size[0] && v < size[1] {
						got = pix[v*size[0]+u]
					}
					w := want.Pix[4*((y-area.Min.Y)*area.Dx()+x-area.Min.X)+3]
					if d := abs(int(got) - int(w)); d > worst {
						worst, wx, wy, wg, ww = d, x, y, got, w
					}
				}
			}
			if worst > 1 {
				t.Errorf("%s: op %d: the mask differs from its clips' coverage by %d levels at %d,%d (%d, want %d; mask at %v, scissor %v)", name, i, worst, wx, wy, wg, ww, in.Clip, area)
			}
		}
	}
}

func TestMaskTexturesStayWithinTheirBudget(t *testing.T) {
	// Ten clips of their own shapes in a frame, then none: the idle masks
	// beyond idleMaskPixels are dropped, and their textures freed.
	var b Builder
	masks := &fakeMasks{}
	b.MaskTextures = masks
	var ops []scene.Op
	for i := range 10 {
		x := float32(i * 1010)
		ops = append(ops, scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: x, Y: 5, W: 1000, H: 1500}, Transform: scene.Rotation(0.01)},
			scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: x, W: 10, H: 10}, Color: scene.Color{A: 255}},
			scene.Op{Kind: scene.OpPopClip})
	}
	b.Build(&scene.Scene{Width: 10100, Height: 1600, Ops: ops}, nil)
	if masks.made != 10 || len(masks.live) != 10 {
		t.Fatalf("made %d masks, %d live, want 10", masks.made, len(masks.live))
	}
	b.Build(&scene.Scene{Width: 10100, Height: 1600}, nil)
	kept := 0
	for tex := range masks.live {
		kept += masks.sizes[tex][0] * masks.sizes[tex][1]
	}
	if kept > idleMaskPixels || len(masks.live) == 0 {
		t.Errorf("%d idle masks keep %d pixels, budget %d", len(masks.live), kept, idleMaskPixels)
	}
	b.Release()
	if len(masks.live) != 0 {
		t.Errorf("%d textures left after Release", len(masks.live))
	}
}
