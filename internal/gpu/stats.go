package gpu

import "fmt"

// Summary describes the last scene built, for logs (MYGO_FRAME_STATS): how
// many instances in how many batches, how many ops were left out as covered,
// how many fills drew as opaque cores, and the batches of each class.
func (b *Builder) Summary() string {
	var classes [NumClasses]int
	opaque := 0
	for i := range b.Batches {
		classes[b.Batches[i].Class]++
		if b.Batches[i].Opaque {
			opaque++
		}
	}
	s := fmt.Sprintf("%d instances in %d batches (%d opaque; by class general %d, plain %d, fill %d, shadow %d, glyph %d, subpixel %d, image %d), %d ops culled, %d opaque cores",
		len(b.Instances), len(b.Batches), opaque, classes[ClassGeneral], classes[ClassPlain], classes[ClassFill], classes[ClassShadow], classes[ClassGlyph],
		classes[ClassSubpixel], classes[ClassImage], b.Culled, b.OpaqueCores)
	if b.MasksDrawn > 0 || b.MasksReused > 0 {
		s += fmt.Sprintf(", clip masks %d rasterized (%d px), %d reused", b.MasksDrawn, b.MaskPixels, b.MasksReused)
	}
	if Timing {
		s += fmt.Sprintf(", masks took %.2f ms, of it raster %.2f", b.MaskTime.Seconds()*1000, b.MaskRaster.Seconds()*1000)
	}
	return s
}
