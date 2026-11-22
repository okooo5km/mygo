//go:build darwin

package metal

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/gpu"
)

// frameDiag measures where a frame's present goes, for MYGO_FRAME_STATS:
// the wait for the slot's GPU frame, the drawable, the encoding, the commit,
// and how long the GPU took on the frame whose wait ended. It is nil, and
// costs nothing, while MYGO_FRAME_STATS is unset.
type frameDiag struct {
	total, wait, acquire, draw, commit time.Duration
	// src tells where the drawable came from: prefetch, fresh (a
	// nextDrawable of the main thread) or none; sync that the frame presented
	// with the transaction; stashed that its drawing waited for the presenter.
	src           string
	sync, stashed bool
	// gpu is the GPU's time on the frame waited for, gpuWait the wait for it.
	gpu, gpuWait time.Duration
	// build is gpu.Builder.Build (CPU), upload what build does after it
	// (buffers, backdrops), pipes the pipeline lookup, prepare all of
	// prepare, encode drawBuilt (canvases' too), canvases how many.
	build, upload, pipes, prepare, encode time.Duration
	canvases                              int
}

var diagOn = func() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MYGO_FRAME_STATS"))) {
	case "", "0", "false", "off", "no":
		return false
	}
	return true
}()

func newFrameDiag() *frameDiag {
	if !diagOn {
		return nil
	}
	gpu.Timing = true
	if msgDouble == nil {
		purego.RegisterFunc(&msgDouble, msgSend)
	}
	return &frameDiag{}
}

func (d *frameDiag) reset() {
	if d == nil {
		return
	}
	*d = frameDiag{}
}

func ms(t time.Duration) string { return fmt.Sprintf("%.2f", t.Seconds()*1000) }

func (d *frameDiag) String() string {
	if d == nil {
		return ""
	}
	return fmt.Sprintf("metal: render %s ms (waitSlot %s, drawFrame %s, drawable %s from %s, commit+present %s; sync %v, stashed %v; GPU %s ms on the frame waited for, waited %s ms for it; prepare %s: Build %s, upload %s, pipes %s, %d canvases; encode %s)",
		ms(d.total), ms(d.wait), ms(d.draw), ms(d.acquire), d.src, ms(d.commit), d.sync, d.stashed, ms(d.gpu), ms(d.gpuWait), ms(d.prepare), ms(d.build), ms(d.upload), ms(d.pipes), d.canvases, ms(d.encode))
}
