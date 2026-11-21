package ui

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

func TestWheelGlides(t *testing.T) {
	var s ScrollState
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&s).Children(func() {
			coreBox(c).Height(1000).Shrink(0)
		})
	}, 200, 200)
	tt.Move(100, 100)
	// A notch of a wheel glides there, as browsers scroll it.
	tt.Wheel(100, 100, 0, 100)
	tt.Frame()
	if s.Y != 0 {
		t.Errorf("a notch scrolled at once, to %v", s.Y)
	}
	tt.Advance(50 * time.Millisecond)
	if s.Y <= 0 || s.Y >= 100 {
		t.Errorf("a notch on its way: at %v", s.Y)
	}
	// Notches on the way add up.
	tt.Wheel(100, 100, 0, 100)
	tt.Advance(time.Second)
	if s.Y != 200 || tt.FramePending() {
		t.Errorf("after two notches: at %v, frames pending %v", s.Y, tt.FramePending())
	}
	// Past the end, they stop there.
	for range 10 {
		tt.Wheel(100, 100, 0, 100)
	}
	tt.Advance(time.Second)
	if s.Y != 800 {
		t.Errorf("past the end: at %v", s.Y)
	}
	// The app setting the offset takes over.
	tt.Wheel(100, 100, 0, -100)
	tt.Advance(50 * time.Millisecond)
	s.Y = 10
	tt.Advance(time.Second)
	if s.Y != 10 {
		t.Errorf("set to 10 on the way: at %v", s.Y)
	}
	// A touchpad scrolls at once.
	tt.Scroll(100, 100, 0, 30)
	if s.Y != 40 {
		t.Errorf("a touchpad scrolled to %v", s.Y)
	}
}

func TestWheelMomentumReachesInput(t *testing.T) {
	var got InputEvent
	tt := coreNewTester(func(c *context) {
		coreBox(c).Fill().HandleInput(func(ev InputEvent) bool {
			got = ev
			return true
		})
	}, 200, 200)
	tt.Move(100, 100)
	tt.send(platform.SurfaceEvent{Kind: platform.PointerScroll, X: 100, Y: 100, DY: -40, Precise: true, Momentum: true})
	if got.Kind != InputScroll || !got.Precise || !got.Momentum {
		t.Errorf("input got kind %v, precise %v, momentum %v; want InputScroll, true, true", got.Kind, got.Precise, got.Momentum)
	}
}

func TestWheelReducedMotion(t *testing.T) {
	var s ScrollState
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&s).Children(func() {
			coreBox(c).Height(1000).Shrink(0)
		})
	}, 200, 200)
	tt.SetPreferences(Preferences{ReduceMotion: true, TextScale: 1})
	tt.Move(100, 100)
	tt.Wheel(100, 100, 0, 100)
	if s.Y != 100 {
		t.Errorf("with less motion, a notch scrolled to %v", s.Y)
	}
}

func TestWheelInnerAtEnd(t *testing.T) {
	var outer, inner ScrollState
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&outer).Children(func() {
			coreScroll(c).Height(100).Shrink(0).TrackScroll(&inner).Children(func() {
				coreBox(c).Height(150).Shrink(0)
			})
			coreBox(c).Height(1000).Shrink(0)
		})
	}, 200, 200)
	tt.Move(100, 50)
	// The inner container takes the notches it has room for, the outer
	// the next.
	tt.Wheel(100, 50, 0, 40)
	tt.Wheel(100, 50, 0, 40)
	tt.Advance(time.Second)
	if inner.Y != 50 || outer.Y != 0 {
		t.Errorf("two notches: inner at %v, outer at %v", inner.Y, outer.Y)
	}
	tt.Wheel(100, 50, 0, 40)
	tt.Advance(time.Second)
	if inner.Y != 50 || outer.Y != 40 {
		t.Errorf("three notches: inner at %v, outer at %v", inner.Y, outer.Y)
	}
}

// The app scrolling a List, or an element coming into view, takes over
// from the notches of a wheel on their way, as setting a ScrollState does.
func TestWheelListScrollTakesOver(t *testing.T) {
	var s ListState
	tt := coreNewTester(func(c *context) {
		coreList(c, &s, 200, func(i int) { coreBox(c).Height(20) }).Fill()
	}, 200, 200)
	offset := func() float64 { return tt.rt.states[tt.rt.c.root.first.id].scrollY }
	tt.Move(100, 100)
	tt.Wheel(100, 100, 0, 300)
	tt.Advance(30 * time.Millisecond)
	if offset() <= 0 {
		t.Fatalf("a notch on its way: at %v", offset())
	}
	s.ScrollTo(0, Start)
	tt.Advance(time.Second)
	if offset() != 0 {
		t.Errorf("ScrollTo(0) on the way: at %v", offset())
	}
	// A notch after the app's ScrollTo glides as usual.
	tt.Wheel(100, 100, 0, 100)
	tt.Advance(time.Second)
	if math.Abs(offset()-100) > 0.01 {
		t.Errorf("a notch after ScrollTo: at %v", offset())
	}
	tt.Wheel(100, 100, 0, -100)
	tt.Advance(30 * time.Millisecond)
	s.ScrollToEnd()
	tt.Advance(time.Second)
	if end := float64(200*20 - 200); math.Abs(offset()-end) > 0.01 {
		t.Errorf("ScrollToEnd on the way up: at %v, want %v", offset(), end)
	}
}

func TestWheelRevealTakesOver(t *testing.T) {
	reveal := false
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().Children(func() {
			for i := range 50 {
				e := coreBox(c).Height(40).Shrink(0)
				if i == 0 && reveal {
					e.ScrollIntoView()
				}
			}
		})
	}, 200, 200)
	offset := func() float64 { return tt.rt.states[tt.rt.c.root.first.id].scrollY }
	tt.Move(100, 100)
	tt.Wheel(100, 100, 0, 400)
	tt.Advance(30 * time.Millisecond)
	if offset() <= 0 {
		t.Fatalf("a notch on its way: at %v", offset())
	}
	reveal = true
	tt.Frame()
	reveal = false
	tt.Advance(time.Second)
	if offset() != 0 {
		t.Errorf("the first box brought into view on the way: at %v", offset())
	}
}

// TestWheelReverseAtEnd reproduces a wheel gliding to the end of the
// content, and the user turning it the other way while the notches are
// still on their way: the remaining travel must not absorb the turn, or
// the wheel seems to not respond until the old notches run out.
func TestWheelReverseAtEnd(t *testing.T) {
	var s ScrollState
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&s).Children(func() {
			coreBox(c).Height(1000).Shrink(0)
		})
	}, 200, 200)
	tt.Move(100, 100)
	// The notches of a fast wheel add up on their way.
	for range 10 {
		tt.Wheel(100, 100, 0, 100)
	}
	tt.Advance(50 * time.Millisecond)
	if s.Y <= 0 || s.Y >= 800 {
		t.Fatalf("setup: gliding at %v, want between 0 and 800", s.Y)
	}
	// Keep scrolling to the end, then turn the wheel the other way at once.
	for range 10 {
		tt.Wheel(100, 100, 0, 100)
	}
	// Advance frame by frame until the content reaches the end, rather than
	// relying on a fixed time that may land it a hair short: arm64 fuses
	// float multiply-add into FMA, so the notches have not run out at 50 ms
	// and the content is still on its way (e.g. at 770). Reaching the end
	// is what matters, not how long it took.
	for i := 0; s.Y < 800 && i < 100; i++ {
		tt.Advance(16 * time.Millisecond)
	}
	if s.Y != 800 {
		t.Fatalf("setup: at the end %v, want 800", s.Y)
	}
	tt.Wheel(100, 100, 0, -100)
	tt.Advance(50 * time.Millisecond)
	if s.Y >= 800 {
		t.Fatalf("the wheel did not turn back at the end: still at %v, want < 800", s.Y)
	}
}

// wheelSettle bounds how long a lone notch takes to rest.
const wheelSettle = 500 * time.Millisecond

// wheelDeltas turns a mouse wheel of notches of 40 DIPs every every,
// for turning, on a tall scroll container at frames of frame, and returns
// the distance the content went in each frame, until after it rests.
func wheelDeltas(t *testing.T, every, frame, turning time.Duration, notch float32) (deltas []float64, s *ScrollState) {
	t.Helper()
	s = new(ScrollState)
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(s).Children(func() {
			coreBox(c).Height(100000).Shrink(0)
		})
	}, 200, 200)
	tt.h.hz = float32(float64(time.Second) / float64(frame))
	tt.Move(100, 100)
	tt.Frame()
	last := float64(s.Y)
	next := time.Duration(0)
	for at := time.Duration(0); at < turning+time.Second; at += frame {
		if at >= next && at < turning {
			tt.Wheel(100, 100, 0, notch)
			next += every
		}
		tt.Advance(frame)
		deltas = append(deltas, float64(s.Y)-last)
		last = float64(s.Y)
	}
	return deltas, s
}

// A wheel turning steadily scrolls steadily: the speed carries over from
// notch to notch, at 60 and at 120 Hz, instead of starting over at each.
func TestWheelSteadySpeed(t *testing.T) {
	for _, tc := range []struct{ every, frame time.Duration }{
		{50 * time.Millisecond, 16667 * time.Microsecond},
		{50 * time.Millisecond, 8333 * time.Microsecond},
		{100 * time.Millisecond, 16667 * time.Microsecond},
		{100 * time.Millisecond, 8333 * time.Microsecond},
	} {
		d, s := wheelDeltas(t, tc.every, tc.frame, time.Second, 40)
		// The steady stretch, after the start and before the last notch.
		lo, hi := int(400*time.Millisecond/tc.frame), int(time.Second/tc.frame)
		w := d[lo:hi]
		var sum, sq float64
		for _, v := range w {
			sum += v
			sq += v * v
		}
		mean := sum / float64(len(w))
		sd := math.Sqrt(max(0, sq/float64(len(w))-mean*mean))
		want := 40 * float64(tc.frame) / float64(tc.every)
		if math.Abs(mean-want) > want*0.15 {
			t.Errorf("%v notches at %v frames: %.2f px a frame, want about %.2f", tc.every, tc.frame, mean, want)
		}
		// A curve restarted at each notch has sd 2.9 (60 Hz, 50 ms) and
		// 1.5 (120 Hz); the spring 0.67 and 0.43.
		limit := 0.1 * mean
		if tc.every > 50*time.Millisecond {
			limit = 0.3 * mean // the gaps outlast the spring: some ripple stays
		}
		if sd > limit {
			t.Errorf("%v notches at %v frames: sd %.2f px a frame, want <= %.2f", tc.every, tc.frame, sd, limit)
		}
		for i, v := range w {
			if v <= 0 {
				t.Errorf("%v notches at %v frames: frame %d did not move (%v)", tc.every, tc.frame, lo+i, v)
				break
			}
		}
		if n := float64(time.Second/tc.every) * 40; s.Y != float32(n) {
			t.Errorf("%v notches at %v frames: rests at %v, want %v", tc.every, tc.frame, s.Y, n)
		}
	}
}

// A lone notch has no jump at its start, does not overshoot, rests
// exactly where it was sent, and does so about as fast as it used to.
func TestWheelLoneNotch(t *testing.T) {
	for _, frame := range []time.Duration{16667 * time.Microsecond, 8333 * time.Microsecond} {
		var s ScrollState
		tt := coreNewTester(func(c *context) {
			coreScroll(c).Fill().TrackScroll(&s).Children(func() {
				coreBox(c).Height(1000).Shrink(0)
			})
		}, 200, 200)
		tt.h.hz = float32(float64(time.Second) / float64(frame))
		tt.Move(100, 100)
		tt.Wheel(100, 100, 0, 100)
		var at, rest time.Duration
		var ahead, prev, firstStep, maxStep float64
		for at < wheelSettle {
			tt.Advance(frame)
			at += frame
			y := float64(s.Y)
			if y > 100 {
				t.Fatalf("frame %v: overshot to %v", frame, y)
			}
			if y < prev {
				t.Fatalf("frame %v: went back from %v to %v", frame, prev, y)
			}
			if firstStep == 0 {
				firstStep = y
			}
			maxStep = max(maxStep, y-prev)
			ahead = y
			if rest == 0 && y == 100 {
				rest = at
			}
			prev = y
		}
		if ahead != 100 || rest == 0 || rest > 400*time.Millisecond {
			t.Errorf("frame %v: at %v after %v, rested at %v; want 100 within 400 ms", frame, ahead, wheelSettle, rest)
		}
		// It starts gently: the first frame is far from the fastest.
		if firstStep > 0.6*maxStep {
			t.Errorf("frame %v: first step %.2f of a fastest %.2f: a jump at the start", frame, firstStep, maxStep)
		}
		if tt.FramePending() {
			t.Errorf("frame %v: frames still pending after it rested", frame)
		}
	}
}

// Notches past the end of the content never move it past the end, and
// the content rests exactly at it, with the same spring.
func TestWheelSpringBounds(t *testing.T) {
	var s ScrollState
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&s).Children(func() {
			coreBox(c).Height(1000).Shrink(0)
		})
	}, 200, 200)
	tt.Move(100, 100)
	for i := range 40 {
		tt.Wheel(100, 100, 0, 100)
		tt.Advance(8 * time.Millisecond)
		if s.Y < 0 || s.Y > 800 {
			t.Fatalf("notch %d: at %v, outside 0..800", i, s.Y)
		}
	}
	tt.Advance(time.Second)
	if s.Y != 800 {
		t.Errorf("at %v, want 800", s.Y)
	}
	// And the same upward, to the start.
	for range 40 {
		tt.Wheel(100, 100, 0, -100)
		tt.Advance(8 * time.Millisecond)
		if s.Y < 0 || s.Y > 800 {
			t.Fatalf("at %v, outside 0..800", s.Y)
		}
	}
	tt.Advance(time.Second)
	if s.Y != 0 {
		t.Errorf("at %v, want 0", s.Y)
	}
}
