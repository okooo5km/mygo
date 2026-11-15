//go:build windows

package d3d11

import (
	"fmt"
	"time"
	"unsafe"
)

// Uploads go through staging textures the renderer owns, not
// UpdateSubresource or a texture's initial data. Given those, the driver
// copies the pixels into upload heaps of its own: NVIDIA's reserves them
// 32 MB at a time as uploads come in bursts (every thumbnail of a list at
// a show, a mask in every frame a clip moved) and keeps them while the
// device lives, 500 MB after a few show / scroll / hide cycles of an app's
// panel (MYGO-UPSTREAM 8 in Clips). This is how ANGLE uploads to Direct3D
// 11 (Image11's staging texture, mapped, then CopySubresourceRegion into
// the texture drawn from), and Microsoft's guidance for data that is not
// written once at creation ("How to: Use dynamic resources", the remarks
// of ID3D11DeviceContext::UpdateSubresource about the copy it makes when
// the resource is in use).
//
// A staging texture is stageSide pixels square, mapped with
// D3D11_MAP_FLAG_DO_NOT_WAIT: one the GPU still copies from is passed for
// the next, and a new one made, up to maxStages of a format; beyond them
// the oldest is waited for. They go once unused for stageIdle, and all of
// them when the window goes idle (trim).
const (
	stageSide = 512
	maxStages = 24
	stageIdle = 2 * time.Second
)

const (
	usageStaging       = 3
	mapWrite           = 2
	mapFlagDoNotWait   = 0x100000
	dxgiErrorStillDraw = 0x887A000A
	formatA8Unorm      = 65
)

// stage is a staging texture of a format, and when it was last written.
type stage struct {
	tex  uintptr
	used time.Time
}

// stages are the staging textures of each format, and next the one to
// try first.
type stages struct {
	list []stage
	next int
}

// upload copies w×h pixels of bpp bytes, rows of stride bytes from pix,
// into dst, a texture of format, at (x, y), through staging textures.
func (r *Renderer) upload(dst uintptr, format uint32, bpp, x, y, w, h int, pix []byte, stride int) error {
	if w <= 0 || h <= 0 {
		return nil
	}
	if r.stages == nil {
		r.stages = map[uint32]*stages{}
	}
	ss := r.stages[format]
	if ss == nil {
		ss = &stages{}
		r.stages[format] = ss
	}
	now := time.Now()
	for cy := 0; cy < h; cy += stageSide {
		ch := min(stageSide, h-cy)
		for cx := 0; cx < w; cx += stageSide {
			cw := min(stageSide, w-cx)
			st, m, err := r.mapStage(ss, format)
			if err != nil {
				return err
			}
			for row := range ch {
				src := pix[(cy+row)*stride+cx*bpp:][:cw*bpp]
				copy(unsafe.Slice((*byte)(unsafe.Add(ptr(m.Data), row*int(m.RowPitch))), cw*bpp), src)
			}
			call(r.ctx, ctxUnmap, st.tex, 0)
			st.used = now
			b := box{Right: uint32(cw), Bottom: uint32(ch), Back: 1}
			call(r.ctx, ctxCopySubresourceRegion, dst, 0, uintptr(x+cx), uintptr(y+cy), 0, st.tex, 0, uintptr(unsafe.Pointer(&b)))
		}
	}
	return nil
}

// mapStage maps a staging texture of format the GPU is done with for
// writing, making one if none is and there are fewer than maxStages, or
// waiting for the next.
func (r *Renderer) mapStage(ss *stages, format uint32) (*stage, mapped, error) {
	var m mapped
	for i := range ss.list {
		st := &ss.list[(ss.next+i)%len(ss.list)]
		hr := call(r.ctx, ctxMap, st.tex, 0, mapWrite, mapFlagDoNotWait, uintptr(unsafe.Pointer(&m)))
		if uint32(hr) == dxgiErrorStillDraw {
			continue
		}
		if failed(hr) {
			return nil, m, fmt.Errorf("d3d11: cannot map a staging texture: %#x", uint32(hr))
		}
		ss.next = (ss.next + i + 1) % len(ss.list)
		return st, m, nil
	}
	if len(ss.list) < maxStages {
		desc := texture2DDesc{Width: stageSide, Height: stageSide, MipLevels: 1, ArraySize: 1, Format: format, SampleCount: 1,
			Usage: usageStaging, CPUAccessFlags: cpuAccessWrite}
		var tex uintptr
		if hr := call(r.device, devCreateTexture2D, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&tex))); failed(hr) {
			return nil, m, fmt.Errorf("d3d11: cannot create a staging texture: %#x", uint32(hr))
		}
		ss.list = append(ss.list, stage{tex: tex})
		st := &ss.list[len(ss.list)-1]
		if hr := call(r.ctx, ctxMap, st.tex, 0, mapWrite, 0, uintptr(unsafe.Pointer(&m))); failed(hr) {
			return nil, m, fmt.Errorf("d3d11: cannot map a staging texture: %#x", uint32(hr))
		}
		ss.next = 0
		return st, m, nil
	}
	// All of them are being copied from: the oldest copy is done first.
	st := &ss.list[ss.next]
	if hr := call(r.ctx, ctxMap, st.tex, 0, mapWrite, 0, uintptr(unsafe.Pointer(&m))); failed(hr) {
		return nil, m, fmt.Errorf("d3d11: cannot map a staging texture: %#x", uint32(hr))
	}
	ss.next = (ss.next + 1) % len(ss.list)
	return st, m, nil
}

// dropStages frees the staging textures unused since before, or all of
// them when before is zero.
func (r *Renderer) dropStages(before time.Time) {
	for _, ss := range r.stages {
		kept := ss.list[:0]
		for _, st := range ss.list {
			if before.IsZero() || st.used.Before(before) {
				free(&st.tex)
				continue
			}
			kept = append(kept, st)
		}
		clear(ss.list[len(kept):])
		ss.list, ss.next = kept, 0
	}
}

// stagingBytes is the memory of the staging textures, for tests.
func (r *Renderer) stagingBytes() int {
	n := 0
	for format, ss := range r.stages {
		bpp := 4
		if format == formatR8Unorm || format == formatA8Unorm {
			bpp = 1
		}
		n += len(ss.list) * stageSide * stageSide * bpp
	}
	return n
}
