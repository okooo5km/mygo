// Author: okooo5km(十里)

package windows

import "github.com/egoist/mygo/internal/platform"

// No Win32 call and no build constraint in this file: the decisions of the
// surface about a composition that starts while no text input has the
// keyboard, which can be tested on any platform.
//
// A window keeps its input context while an element that opens a text input
// from typed text has the focus (platform.TextInputState.Intent), so the
// first key of "ni" reaches the input method, which composes it. That
// composition has nowhere to go until the text input is active, a frame
// later. imeIntent decides what becomes of it:
//
//   - the first text of a composition (its string, or a result string) is
//     offered to the content as an intent (platform.SurfaceEvent.Intent);
//   - claimed: what the input method makes is queued, in order, until a
//     text input is active, then delivered to it (flush);
//   - not claimed: the composition is canceled, and what it makes until it
//     ends is dropped.

// imeKind is what an imeItem delivers.
type imeKind uint8

const (
	imeResult imeKind = iota // committed text (GCS_RESULTSTR)
	imeComp                  // the composition string and its caret
	imeCancel                // a composition given up, with no string
	imeEnd                   // WM_IME_ENDCOMPOSITION
)

// imeItem is one thing an input method made, as the surface delivers it.
type imeItem struct {
	Kind  imeKind
	Text  string
	Caret int // runes, of imeComp
}

// imeComposed is a WM_IME_COMPOSITION, read.
type imeComposed struct {
	// Result is the committed string, Comp the composition string and Caret
	// its cursor, in runes; HasResult and HasComp tell which the message
	// carries (GCS_RESULTSTR, GCS_COMPSTR). Empty is the message of a
	// composition given up, which carries neither and leaves none.
	Result, Comp       string
	Caret              int
	HasResult, HasComp bool
	Empty              bool
}

// items returns what the message makes, in the order a surface delivers it:
// the result first, then the composition.
func (c imeComposed) items() []imeItem {
	var out []imeItem
	if c.HasResult && c.Result != "" {
		out = append(out, imeItem{Kind: imeResult, Text: c.Result})
	}
	if c.HasComp {
		out = append(out, imeItem{Kind: imeComp, Text: c.Comp, Caret: c.Caret})
	}
	if c.Empty {
		out = append(out, imeItem{Kind: imeCancel})
	}
	return out
}

// first is the text that tells which key began a composition.
func (c imeComposed) first() string {
	if c.HasResult && c.Result != "" {
		return c.Result
	}
	return c.Comp
}

type imePhase uint8

const (
	imeIdle    imePhase = iota // nothing waits
	imeClaimed                 // the content took the intent: items are queued
	imeDropped                 // it did not: the composition is canceled and ignored until it ends
)

// imeRoute is where a message of an input method goes.
type imeRoute uint8

const (
	imeDirect  imeRoute = iota // to the active text input, as always
	imeQueued                  // withheld until a text input is active
	imeIgnored                 // dropped; cancel says whether to cancel the composition
)

// imeIntent is the state of the decision above.
type imeIntent struct {
	phase imePhase
	queue []imeItem
	// open is whether a composition is open (WM_IME_STARTCOMPOSITION seen,
	// WM_IME_ENDCOMPOSITION not yet).
	open bool
	// command is set by a key the content takes as a command whatever the
	// input method made of it (Space, a digit, Return): what that key
	// composes is dropped, until the next key.
	command bool
}

// Start notes WM_IME_STARTCOMPOSITION.
func (t *imeIntent) Start() { t.open = true }

// Open reports whether a composition is open.
func (t *imeIntent) Open() bool { return t.open }

// Pending reports whether a claimed composition waits for a text input.
func (t *imeIntent) Pending() bool { return t.phase == imeClaimed }

// NewKey notes a key down: what the previous key made is over.
func (t *imeIntent) NewKey() { t.command = false }

// Command notes that the key just pressed is the content's: the input
// method's output for it is dropped.
func (t *imeIntent) Command() { t.command = true }

// Compose routes a WM_IME_COMPOSITION. active is whether a text input has
// the keyboard; offer shows the first text of a new composition to the
// content as an intent, and reports whether it claimed it. cancel is true
// when the composition is to be canceled (ImmNotifyIME, deferred by the
// caller out of the message).
func (t *imeIntent) Compose(c imeComposed, active bool, offer func(text string) bool) (route imeRoute, cancel bool) {
	switch {
	case t.command:
		return imeIgnored, !c.Empty && c.Comp != ""
	case t.phase == imeDropped:
		return imeIgnored, c.Comp != ""
	case t.phase == imeClaimed:
		t.queue = append(t.queue, c.items()...)
		return imeQueued, false
	case active:
		return imeDirect, false
	}
	text := c.first()
	if text == "" {
		return imeIgnored, false // a composition given up, with none left
	}
	if offer(text) {
		t.phase = imeClaimed
		t.queue = append(t.queue[:0], c.items()...)
		return imeQueued, false
	}
	t.phase = imeDropped
	return imeIgnored, c.Comp != ""
}

// End routes a WM_IME_ENDCOMPOSITION: imeDirect is for the active text
// input, as always.
func (t *imeIntent) End() imeRoute {
	t.open = false
	switch t.phase {
	case imeClaimed:
		t.queue = append(t.queue, imeItem{Kind: imeEnd})
		return imeQueued
	case imeDropped:
		t.phase = imeIdle
		t.command = false
		return imeIgnored
	}
	if t.command {
		return imeIgnored
	}
	return imeDirect
}

// Flush hands the queue, in order, to deliver, and is idle again.
func (t *imeIntent) Flush(deliver func(imeItem)) {
	q := t.queue
	t.queue, t.phase = nil, imeIdle
	for _, it := range q {
		deliver(it)
	}
}

// Drop gives the intent up: whatever waits is forgotten, and a composition
// still open is ignored until it ends. It reports whether there was one to
// cancel.
func (t *imeIntent) Drop() (cancel bool) {
	waiting := t.phase == imeClaimed
	t.queue = nil
	t.phase = imeIdle
	if t.open {
		t.phase = imeDropped
		return true
	}
	return waiting
}

// Reset forgets everything: for a context that is let go of, which sends nothing more (not even
// WM_IME_ENDCOMPOSITION), so no composition is open and none is waited for.
func (t *imeIntent) Reset() { *t = imeIntent{} }

// imeCommandKey reports whether the key at this XT scancode is one the
// content acts on while the timeline has the focus (a digit pastes, Space
// previews, Return pastes, Escape and Tab, Backspace), which an input method
// in a mode that composes them (kana digits, full-width Space) takes with
// VK_PROCESSKEY: the surface then sends the key by position all the same.
// Letters are not here: they are what opens the search.
func imeCommandKey(scan uint8, extended bool) bool {
	switch scan {
	case 0x01, 0x0E, 0x0F, 0x39: // Escape, Backspace, Tab, Space
		return !extended
	case 0x1C: // Return; the keypad's (extended) is not here: its release is the input method's
		return !extended
	}
	return !extended && scan >= 0x02 && scan <= 0x0B // 1 to 9 and 0
}

// imeIntentEvent is the intent the content is offered.
func imeIntentEvent(text string, mods platform.Modifiers) platform.SurfaceEvent {
	return platform.SurfaceEvent{Kind: platform.TextInput, Text: text, Intent: true, Mods: mods}
}
