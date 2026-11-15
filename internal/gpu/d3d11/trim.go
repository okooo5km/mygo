//go:build windows

package d3d11

import (
	"syscall"
	"time"
	"unsafe"
)

// A window that draws nothing for trimAfter gives back what its frames
// leave: the textures of images the last frame did not draw, the staging
// textures, and the driver's own memory of the device (IDXGIDevice3::Trim,
// after ClearState, as its documentation asks: the upload heaps the driver
// grew with the uploads, among others). A window hidden for hiddenAfter
// gives back every texture its frames use, as Metal's renderer does after
// the same idle time; the next frame makes them again and draws whole, as
// the first frame of a new window does. The window's swap chain stays as
// it is: DWM shows its last frame.
const (
	trimAfter   = 2 * time.Second
	hiddenAfter = 2 * time.Second
	trimTimer   = 0x6d7a // the timer's ID on the window
)

var (
	procIsWindowVisible = user32.NewProc("IsWindowVisible")
	iidIDXGIDevice3     = guid{0x6007896c, 0x3244, 0x4afd, [8]byte{0xbf, 0x18, 0xa6, 0xd3, 0xbe, 0xda, 0x50, 0x23}}
)

const dxgiDev3Trim = 17 // IDXGIDevice3::Trim

// trimming holds the renderers whose trim timer is armed, by window;
// trimProc is the timers' callback, made once.
var (
	trimming = map[uintptr]*Renderer{}
	trimProc uintptr
)

// armTrim has the window's timer trim the renderer once it draws nothing
// for trimAfter. A frame drawn meanwhile puts it off (trimIfIdle).
func (r *Renderer) armTrim() { r.armTrimIn(trimAfter) }

// armTrimIn has the window's timer call trimIfIdle after d.
func (r *Renderer) armTrimIn(d time.Duration) {
	if r.trimArmed || r.hwnd == 0 {
		return
	}
	if trimProc == 0 {
		trimProc = syscall.NewCallback(func(hwnd, msg, id, ms uintptr) uintptr {
			procKillTimer.Call(hwnd, id)
			if r := trimming[hwnd]; r != nil {
				r.trimArmed = false
				delete(trimming, hwnd)
				r.trimIfIdle()
			}
			return 0
		})
	}
	trimming[r.hwnd] = r
	r.trimArmed = true
	procSetTimer.Call(r.hwnd, trimTimer, uintptr(d/time.Millisecond), trimProc)
}

// disarmTrim stops the trim timer, for a renderer going away.
func (r *Renderer) disarmTrim() {
	if r.trimArmed {
		procKillTimer.Call(r.hwnd, trimTimer)
		if trimming[r.hwnd] == r {
			delete(trimming, r.hwnd)
		}
		r.trimArmed = false
	}
}

// trimIfIdle trims once the window drew nothing for trimAfter, and waits
// for that time otherwise; a frame waiting to present, or a call to the
// swap chain on another goroutine (uncapped.go), puts it off too.
func (r *Renderer) trimIfIdle() {
	if r.device == 0 || r.trimmed && r.trimmedAll {
		return
	}
	idle := time.Since(r.lastRender)
	if idle < trimAfter || r.pending || r.aside != "" {
		r.armTrim()
		return
	}
	hidden := !windowVisible(r.hwnd)
	if !r.trimmed || hidden && idle >= hiddenAfter {
		r.trim(hidden && idle >= hiddenAfter)
	}
	if hidden && !r.trimmedAll {
		// Hidden still at hiddenAfter: every texture goes then.
		r.armTrimIn(max(hiddenAfter-idle, 100*time.Millisecond))
	}
}

// windowVisible reports whether the window, and every window it is in,
// is visible.
func windowVisible(hwnd uintptr) bool {
	v, _, _ := procIsWindowVisible.Call(hwnd)
	return v != 0
}

// trim gives back what the frames leave, and, hidden, every texture
// they use.
func (r *Renderer) trim(hidden bool) {
	r.trimmed, r.trimmedAll = true, hidden
	r.now = time.Now()
	for id, t := range r.images {
		if t.lastFrame != r.frame || hidden {
			freeTexture(t)
			delete(r.images, id)
		}
	}
	r.dropStages(time.Time{})
	if hidden {
		r.releaseFrameTextures()
	}
	call(r.ctx, ctxClearState)
	call(r.ctx, ctxFlush)
	trimDevice(r.device)
}

// releaseFrameTextures frees the textures the frames draw with, which the
// next frame makes again, drawing whole: canvases, clip masks, atlases,
// backdrops, blurred images and stops.
func (r *Renderer) releaseFrameTextures() {
	r.releaseShared()
	r.sweepShared(true)
	for id, t := range r.canvases {
		freeCanvas(t)
		delete(r.canvases, id)
	}
	classes, noClasses := r.b.Classes, r.b.NoClasses
	r.b.Release()
	r.b.Classes, r.b.NoClasses = classes, noClasses
	for _, t := range r.masks {
		freeTexture(t)
	}
	clear(r.masks)
	for i := range r.blurred {
		t := &r.blurred[i]
		free(&t.rtv)
		free(&t.srv)
		free(&t.tex)
	}
	r.blurred, r.used = nil, 0
	for _, t := range []*texture{&r.mask, &r.color, &r.grab, &r.backdrop[0], &r.backdrop[1], &r.blurTmp[0], &r.blurTmp[1], &r.stops} {
		free(&t.rtv)
		free(&t.srv)
		free(&t.tex)
		*t = texture{}
	}
	r.held = false
	r.diff.Invalidate()
}

// trimDevice has DXGI give back the memory the driver allocated for the
// device on the app's behalf (IDXGIDevice3::Trim, Windows 8.1), where it
// can.
func trimDevice(device uintptr) {
	var dev3 uintptr
	if failed(call(device, 0, uintptr(unsafe.Pointer(&iidIDXGIDevice3)), uintptr(unsafe.Pointer(&dev3)))) {
		return
	}
	call(dev3, dxgiDev3Trim)
	free(&dev3)
}
