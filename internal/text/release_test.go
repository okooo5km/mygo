package text

import "testing"

// ReleaseMemory gives the glyph atlases back; drawing afterwards makes the same glyph masks again.
func TestReleaseMemoryDrawsAgain(t *testing.T) {
	s := New()
	masks := func() [][]byte {
		l := s.Layout(Params{Text: "Agé 停", Style: Style{Size: 32}})
		var out [][]byte
		for _, g := range l.Lines[0].Glyphs {
			img := s.Glyph(g.Font, g.ID, 1, 0, 0, false)
			if !img.OK { // the space has no mask
				out = append(out, nil)
				continue
			}
			var m []byte
			for y := 0; y < int(img.H); y++ {
				row := (int(img.Y)+y)*s.MaskAtlas.W + int(img.X)
				m = append(m, s.MaskAtlas.Pix[row:row+int(img.W)]...)
			}
			out = append(out, m)
		}
		return out
	}
	before := masks()
	if len(before) < 4 {
		t.Fatalf("%d glyphs", len(before))
	}
	for s.MaskAtlas.Grow() {
	}
	gen, mgen := s.Generation(), s.MaskAtlas.Generation()

	s.ReleaseMemory()
	if s.MaskAtlas.W != 256 || s.MaskAtlas.H != 256 || s.ColorAtlas.W != 1 || s.ColorAtlas.H != 1 {
		t.Errorf("atlases %dx%d and %dx%d", s.MaskAtlas.W, s.MaskAtlas.H, s.ColorAtlas.W, s.ColorAtlas.H)
	}
	if s.Generation() == gen || s.MaskAtlas.Generation() == mgen {
		t.Error("layouts and atlas generations did not change: windows would keep drawing stale glyphs")
	}
	after := masks()
	if len(after) != len(before) {
		t.Fatalf("%d glyphs after, %d before", len(after), len(before))
	}
	for i := range before {
		if string(before[i]) != string(after[i]) {
			t.Errorf("glyph %d differs after a release", i)
		}
	}
}
