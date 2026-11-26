package darwin

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// counter stands in for release: how many times each event was released.
type counter map[id]int

func (c counter) release(ev id) { c[ev]++ }

func (c counter) once(t *testing.T, evs ...id) {
	t.Helper()
	for _, ev := range evs {
		if c[ev] != 1 {
			t.Errorf("event %d released %d times, want once", ev, c[ev])
		}
	}
}

func TestKeyQueueKeepsOrderWhateverTheAge(t *testing.T) {
	var q keyQueue
	q.add(1, pendingKey{code: 45, sent: true})
	q.add(2, pendingKey{code: 34})
	q.add(3, pendingKey{code: 49}) // a Space, a key that types nothing, behind the letters
	for range maxPendingFrames {
		if q.frame() {
			t.Fatal("expired within the frames allowed")
		}
	}
	// the input became active: everything goes, in order, however long it took
	keys := q.take()
	if len(keys) != 3 || keys[0].ev != 1 || keys[1].ev != 2 || keys[2].ev != 3 {
		t.Fatalf("keys %+v", keys)
	}
	if len(q.keys) != 0 {
		t.Fatal("queue not emptied")
	}
}

func TestKeyQueueExpiresByFramesWithNoInput(t *testing.T) {
	var q keyQueue
	if q.frame() {
		t.Fatal("an empty queue expired")
	}
	q.add(1, pendingKey{sent: true})
	for i := 1; i <= maxPendingFrames; i++ {
		if q.frame() {
			t.Fatalf("expired at frame %d", i)
		}
	}
	if !q.frame() {
		t.Fatal("did not expire after the frames allowed")
	}
	// a new run starts counting again and has its own generation
	gen := q.gen
	q.take()
	q.add(2, pendingKey{sent: true})
	if q.frames != 0 || q.gen == gen {
		t.Fatalf("frames %d gen %d (was %d)", q.frames, q.gen, gen)
	}
}

func TestKeyQueueReleasesEveryEventOnEveryPath(t *testing.T) {
	// dropped (expiry, click, blur, destroy): all the keys, the fresh and the old
	var q keyQueue
	c := counter{}
	q.add(1, pendingKey{sent: true})
	q.add(2, pendingKey{})
	q.add(3, pendingKey{})
	for range maxPendingFrames + 1 {
		q.frame()
	}
	releaseAll(q.take(), c.release)
	c.once(t, 1, 2, 3)
	// replayed: each released after its replay
	var r keyQueue
	rc := counter{}
	r.add(4, pendingKey{sent: true})
	r.add(5, pendingKey{})
	for _, k := range r.take() {
		rc.release(k.ev)
	}
	rc.once(t, 4, 5)
	// nothing left to release twice
	releaseAll(r.take(), rc.release)
	rc.once(t, 4, 5)
}

func TestKeyQueueHoldsTheReleaseOfAKeyWhosePressWaits(t *testing.T) {
	var q keyQueue
	up := platform.SurfaceEvent{Kind: platform.KeyReleased}
	q.add(1, pendingKey{code: 45, sent: true}) // pressed live: its release goes live too
	q.add(2, pendingKey{code: 34})
	if q.holdUp(45, up) {
		t.Error("held the release of a key the content saw pressed")
	}
	if !q.holdUp(34, up) {
		t.Error("did not hold the release of a key whose press waits")
	}
	if q.holdUp(34, up) {
		t.Error("held a second release for one press")
	}
	// a release while Command is held (the commandKeyUps monitor) is held the same way: Command+F after a letter
	cmdUp := platform.SurfaceEvent{Kind: platform.KeyReleased, Mods: platform.ModSuper}
	q.add(3, pendingKey{code: 3})
	if !q.holdUp(3, cmdUp) {
		t.Error("did not hold the release of a key typed with Command whose press waits")
	}
	if q.holdUp(99, up) {
		t.Error("held the release of a key that is not held")
	}
	if k := q.take(); k[1].up == nil || k[0].up != nil {
		t.Errorf("releases %+v", k)
	}
}

func TestIntentText(t *testing.T) {
	const (
		shift = platform.ModShift
		alt   = platform.ModAlt
		cmd   = platform.ModSuper
		ctrl  = platform.ModCtrl
	)
	cases := []struct {
		name       string
		chars, ign string
		mods       platform.Modifiers
		want       string
	}{
		{"n", "n", "n", 0, "n"},
		{"Shift+n", "N", "n", shift, "N"},
		{"digit", "1", "1", 0, "1"},
		{"Option+a", "å", "a", alt, "å"},
		{"Option+e dead key falls back", "", "e", alt, "e"},
		{"dead key with no Option has nothing", "", "e", 0, ""},
		{"Space", " ", " ", 0, ""},
		{"Shift+Space", " ", " ", shift, ""},
		{"Option+Space", " ", " ", alt, ""},
		{"Return", "\r", "\r", 0, ""},
		{"keypad Enter", "\x03", "\x03", 0, ""},
		{"Escape", "\x1b", "\x1b", 0, ""},
		{"Tab", "\t", "\t", 0, ""},
		{"Shift+Tab", "\x19", "\x19", shift, ""},
		{"Delete", "\x7f", "\x7f", 0, ""},
		{"Forward Delete", "", "", 0, ""},
		{"Up", "", "", 0, ""},
		{"Option+Left", "", "", alt, ""},
		{"F5", "", "", 0, ""},
		{"Clear", "", "", 0, ""},
		{"Command+n", "n", "n", cmd, ""},
		{"Control+n", "\x0e", "n", ctrl, ""},
		{"Command+Option+n", "n", "n", cmd | alt, ""},
	}
	for _, c := range cases {
		if got := intentText(c.chars, c.ign, c.mods); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
