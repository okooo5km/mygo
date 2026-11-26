// Author: okooo5km(十里)

package windows

import (
	"reflect"
	"testing"
)

func claimAll(string) bool  { return true }
func claimNone(string) bool { return false }

func comp(s string) imeComposed {
	return imeComposed{HasComp: true, Comp: s, Caret: len([]rune(s))}
}

func collect(t *imeIntent) []imeItem {
	var got []imeItem
	t.Flush(func(it imeItem) { got = append(got, it) })
	return got
}

// "ni", Space: the first composition is offered once and claimed; everything after it is queued in order and delivered
// when the box is active.
func TestClaimedCompositionIsQueuedInOrder(t *testing.T) {
	var it imeIntent
	var offered []string
	offer := func(s string) bool { offered = append(offered, s); return true }
	it.Start()
	if r, cancel := it.Compose(comp("n"), false, offer); r != imeQueued || cancel {
		t.Fatalf("first: route %v cancel %v", r, cancel)
	}
	if r, _ := it.Compose(comp("ni"), false, offer); r != imeQueued {
		t.Fatalf("second: route %v", r)
	}
	r, _ := it.Compose(imeComposed{HasResult: true, Result: "你", HasComp: true}, false, offer)
	if r != imeQueued {
		t.Fatalf("commit: route %v", r)
	}
	if r := it.End(); r != imeQueued {
		t.Fatalf("end: route %v", r)
	}
	if !reflect.DeepEqual(offered, []string{"n"}) {
		t.Errorf("offered %q, want one intent, n", offered)
	}
	if !it.Pending() {
		t.Error("not pending")
	}
	want := []imeItem{
		{Kind: imeComp, Text: "n", Caret: 1}, {Kind: imeComp, Text: "ni", Caret: 2},
		{Kind: imeResult, Text: "你"}, {Kind: imeComp}, {Kind: imeEnd},
	}
	if got := collect(&it); !reflect.DeepEqual(got, want) {
		t.Errorf("delivered %+v, want %+v", got, want)
	}
	if it.Pending() {
		t.Error("pending after the flush")
	}
}

// Not claimed: the composition is canceled, what it makes until it ends is ignored, and the next one is offered afresh.
func TestUnclaimedCompositionIsCanceledAndIgnored(t *testing.T) {
	var it imeIntent
	offers := 0
	offer := func(string) bool { offers++; return false }
	it.Start()
	if r, cancel := it.Compose(comp("n"), false, offer); r != imeIgnored || !cancel {
		t.Fatalf("route %v cancel %v, want ignored and canceled", r, cancel)
	}
	// the cancel's own messages
	if r, cancel := it.Compose(imeComposed{Empty: true}, false, offer); r != imeIgnored || cancel {
		t.Errorf("empty: route %v cancel %v", r, cancel)
	}
	if r := it.End(); r != imeIgnored {
		t.Errorf("end: route %v", r)
	}
	it.Start()
	it.Compose(comp("i"), false, offer)
	if offers != 2 || it.Pending() {
		t.Errorf("offers %d pending %v, want 2 and not pending", offers, it.Pending())
	}
}

// With a text input active the messages go straight to it, as always, and nothing is offered.
func TestActiveInputTakesMessagesDirectly(t *testing.T) {
	var it imeIntent
	it.Start()
	r, cancel := it.Compose(comp("n"), true, func(string) bool { t.Error("offered"); return true })
	if r != imeDirect || cancel {
		t.Errorf("route %v cancel %v", r, cancel)
	}
	if r := it.End(); r != imeDirect {
		t.Errorf("end %v", r)
	}
}

// A message that arrives after the box is active but before the queue was delivered goes behind it.
func TestMessageBeforeTheFlushGoesBehindTheQueue(t *testing.T) {
	var it imeIntent
	it.Compose(comp("n"), false, claimAll)
	if r, _ := it.Compose(comp("ni"), true, claimAll); r != imeQueued {
		t.Fatalf("route %v, want queued behind the queue", r)
	}
	got := collect(&it)
	if len(got) != 2 || got[1].Text != "ni" {
		t.Errorf("delivered %+v", got)
	}
	// after the flush the composition goes direct
	if r, _ := it.Compose(comp("nih"), true, claimAll); r != imeDirect {
		t.Errorf("after the flush: route %v", r)
	}
}

// A committed string with no composition (a full-width comma) is offered, as its text.
func TestResultOnlyIsOffered(t *testing.T) {
	var it imeIntent
	var offered string
	it.Compose(imeComposed{HasResult: true, Result: "，"}, false, func(s string) bool { offered = s; return true })
	if offered != "，" {
		t.Errorf("offered %q", offered)
	}
	if got := collect(&it); !reflect.DeepEqual(got, []imeItem{{Kind: imeResult, Text: "，"}}) {
		t.Errorf("delivered %+v", got)
	}
}

// A key the content acts on (Space, a digit) that the input method took: its output is dropped, nothing is offered,
// until the next key.
func TestCommandKeyOutputIsDropped(t *testing.T) {
	var it imeIntent
	it.NewKey()
	it.Command()
	r, cancel := it.Compose(comp("1"), false, func(string) bool { t.Error("offered"); return true })
	if r != imeIgnored || !cancel {
		t.Errorf("route %v cancel %v", r, cancel)
	}
	if r := it.End(); r != imeIgnored {
		t.Errorf("end %v", r)
	}
	it.NewKey()
	offered := false
	it.Compose(comp("n"), false, func(string) bool { offered = true; return true })
	if !offered {
		t.Error("the next key's composition was not offered")
	}
}

// Drop forgets what waits and ignores an open composition until it ends.
func TestDropForgetsTheQueue(t *testing.T) {
	var it imeIntent
	it.Start()
	it.Compose(comp("n"), false, claimAll)
	if !it.Drop() {
		t.Error("nothing to cancel")
	}
	if it.Pending() || len(it.queue) != 0 {
		t.Error("the queue survived")
	}
	if r, _ := it.Compose(comp("ni"), false, claimAll); r != imeIgnored {
		t.Errorf("an open composition after the drop: route %v", r)
	}
	it.End()
	if r, _ := it.Compose(comp("a"), true, claimAll); r != imeDirect {
		t.Errorf("after its end: route %v", r)
	}
	var idle imeIntent
	if idle.Drop() {
		t.Error("an idle intent has something to cancel")
	}
}

// The context is let go of with a composition claimed and open (the panel hid before the box was active): no
// WM_IME_ENDCOMPOSITION comes, so Reset leaves nothing behind: the next composition is offered, none is open.
func TestResetAfterTheContextIsLetGo(t *testing.T) {
	var it imeIntent
	it.Start()
	it.Compose(comp("n"), false, claimAll)
	it.Drop()  // the surface ignores what the completion makes ...
	it.Reset() // ... and starts over once the context is gone
	if it.Open() || it.Pending() || len(it.queue) != 0 || it.phase != imeIdle || it.command {
		t.Fatalf("state left behind: %+v", it)
	}
	offered := false
	r, cancel := it.Compose(comp("n"), false, func(string) bool { offered = true; return true })
	if !offered || r != imeQueued || cancel {
		t.Errorf("first composition of the next show: offered %v route %v cancel %v", offered, r, cancel)
	}
}

func TestCommandKeys(t *testing.T) {
	for _, c := range []struct {
		scan uint8
		ext  bool
		want bool
	}{
		{0x02, false, true}, {0x0B, false, true}, {0x39, false, true}, {0x1C, false, true}, {0x1C, true, false},
		{0x01, false, true}, {0x10, false, false}, {0x1E, false, false}, {0x0C, false, false}, {0x4B, true, false},
	} {
		if got := imeCommandKey(c.scan, c.ext); got != c.want {
			t.Errorf("scan %#x ext %v: %v, want %v", c.scan, c.ext, got, c.want)
		}
	}
}
