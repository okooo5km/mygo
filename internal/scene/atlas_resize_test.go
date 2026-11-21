package scene

import "testing"

func TestAtlasResize(t *testing.T) {
	a := NewAtlas(1, 64, 64)
	x, y, ok := a.Alloc(8, 8)
	if !ok {
		t.Fatal("full")
	}
	a.Put(x, y, 8, 8, make([]byte, 64), 8)
	a.Pix[0] = 9
	gen, ver := a.Generation(), a.Version()

	a.Resize(16, 16)
	if a.W != 16 || a.H != 16 || len(a.Pix) != 16*16 {
		t.Fatalf("size %dx%d, %d bytes", a.W, a.H, len(a.Pix))
	}
	for i, b := range a.Pix {
		if b != 0 {
			t.Fatalf("byte %d is %d, want a zeroed bitmap", i, b)
		}
	}
	if a.Generation() == gen || a.Version() == ver {
		t.Errorf("generation %d, version %d did not change from %d, %d", a.Generation(), a.Version(), gen, ver)
	}
	// a renderer that uploaded the old atlas uploads all of the new one
	if _, full := a.Changes(gen, ver); !full {
		t.Error("a renderer of the old generation is not told to upload everything")
	}
	// the shelves are reset: the whole new atlas can be filled again
	if _, _, ok := a.Alloc(15, 15); !ok { // a pixel of padding each way
		t.Error("the resized atlas cannot allocate its full size")
	}
	a.Resize(1, 1)
	if len(a.Pix) != 1 || a.H != 1 {
		t.Errorf("1x1: %dx%d, %d bytes", a.W, a.H, len(a.Pix))
	}
}
