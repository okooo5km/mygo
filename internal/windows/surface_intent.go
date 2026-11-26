//go:build windows && (amd64 || arm64)

// Author: okooo5km(十里)

package windows

import (
	"unicode/utf16"

	"github.com/egoist/mygo/internal/platform"
)

// Type-to-search with an input method (ime_intent.go decides; this file does the
// Win32 part). While an element that opens a text input from typed text has the
// focus (TextInputState.Intent), the window keeps its input context, so the first
// key of a composition reaches the input method, which composes it: Microsoft
// Pinyin takes "n" and shows 你 for "ni" and Space. The first text of that
// composition is offered to the content as an intent; when the content claims it
// (InputEvent.Claim: it opens its text input), what the input method makes is
// queued until the text input is active, which is a frame later, and then
// delivered to it as the engine's composition and text events.

const (
	// wmAppIntent is posted to the surface to act out of an input method's
	// message: wp 0 delivers the queue, 1 cancels the composition.
	wmAppIntent = wmApp + 11
	// timerIntent gives a claimed composition up when no text input became
	// active (the surface's timer).
	timerIntent = 6
	intentWait  = 2000 // ms
)

// intentRead reads a WM_IME_COMPOSITION of the input context himc.
func intentRead(himc, lp uintptr) imeComposed {
	c := imeComposed{HasResult: lp&gcsResultStr != 0, HasComp: lp&gcsCompStr != 0}
	if c.HasResult {
		c.Result = imeString(himc, gcsResultStr)
	}
	if c.HasComp {
		c.Comp = imeString(himc, gcsCompStr)
		pos, _, _ := procImmGetCompositionStrW.Call(himc, gcsCursorPos, 0, 0)
		units := utf16.Encode([]rune(c.Comp))
		c.Caret = len(utf16.Decode(units[:min(int(pos), len(units))]))
	}
	c.Empty = lp&(gcsResultStr|gcsCompStr) == 0 && imeString(himc, gcsCompStr) == ""
	return c
}

// offerIntent shows the content the first text of a composition, as an intent,
// and reports whether it claimed it.
func (s *surface) offerIntent(text string) bool {
	return s.send(imeIntentEvent(text, mods()))
}

// intentComposed routes a WM_IME_COMPOSITION through the decision. It reports
// whether the message was the intent's: false is the active text input's, as
// always.
func (s *surface) intentComposed(c imeComposed) bool {
	route, cancel := s.intent.Compose(c, s.input.Active, s.offerIntent)
	if cancel {
		postMessage(s.hwnd, wmAppIntent, 1, 0)
	}
	switch route {
	case imeQueued:
		if s.input.Active {
			postMessage(s.hwnd, wmAppIntent, 0, 0)
		} else if s.intent.Pending() {
			procSetTimer.Call(s.hwnd, timerIntent, intentWait, 0)
		}
	case imeDirect:
		return false
	}
	return true
}

// intentEnded routes a WM_IME_ENDCOMPOSITION, as intentComposed does.
func (s *surface) intentEnded() bool {
	switch s.intent.End() {
	case imeQueued:
		if s.input.Active {
			postMessage(s.hwnd, wmAppIntent, 0, 0)
		}
		return true
	case imeIgnored:
		return true
	}
	return false
}

// intentMessage runs wmAppIntent.
func (s *surface) intentMessage(wp uintptr) {
	switch wp {
	case 0:
		if !s.input.Active || !s.intent.Pending() {
			return // the timer gives up the queue if no input comes
		}
		procKillTimer.Call(s.hwnd, timerIntent)
		s.placeIME()
		s.intent.Flush(s.deliverIntent)
	case 1:
		s.cancelComposition()
	}
}

// deliverIntent hands what the input method made to the text input that is
// active now, as the messages would have.
func (s *surface) deliverIntent(it imeItem) {
	switch it.Kind {
	case imeResult:
		s.composed(platform.SurfaceEvent{Kind: platform.TextInput, Text: it.Text})
	case imeComp:
		s.composed(platform.SurfaceEvent{Kind: platform.TextComposition, Text: it.Text, Caret: it.Caret})
	case imeCancel:
		if c := s.input.Client; c != nil {
			s.clientComposition.Cancel(c)
		} else {
			s.send(platform.SurfaceEvent{Kind: platform.TextComposition})
		}
	case imeEnd:
		s.reconvert = nil
		if c := s.input.Client; c != nil {
			s.clientComposition.End(c)
		} else {
			s.send(platform.SurfaceEvent{Kind: platform.TextComposition})
		}
	}
}

// dropIntent gives a claimed or open composition up, and cancels it.
func (s *surface) dropIntent() {
	procKillTimer.Call(s.hwnd, timerIntent)
	if s.intent.Drop() {
		postMessage(s.hwnd, wmAppIntent, 1, 0)
	}
}

// cancelComposition has the input method clear its composition string
// (ImmNotifyIME, NI_COMPOSITIONSTR, CPS_CANCEL).
func (s *surface) cancelComposition() {
	if himc, _, _ := procImmGetContext.Call(s.hwnd); himc != 0 {
		procImmNotifyIME.Call(himc, niCompositionStr, cpsCancel, 0)
		procImmReleaseContext.Call(s.hwnd, himc)
	}
}

// commandThroughIME reports whether a WM_KEYDOWN the input method took with
// VK_PROCESSKEY is a key the content acts on (imeCommandKey) while no text
// input is active and no composition is open: a digit that pastes, Space that
// previews, as with no input context.
func (s *surface) commandThroughIME(wp, lp uintptr) bool {
	return wp == vkProcessKey && s.ime && !s.input.Active && !s.intent.Open() && !s.intent.Pending() &&
		imeCommandKey(uint8(lp>>16&0xFF), lp&(1<<24) != 0)
}

// commandKey sends the key of a VK_PROCESSKEY key down by its position, and
// drops what the input method makes of it.
func (s *surface) commandKey(lp uintptr, modifiers platform.Modifiers) bool {
	sc := uint8(lp >> 16 & 0xFF)
	k := platform.KeyForScancode(uint16(sc))
	if k == platform.KeyUnknown {
		return false
	}
	s.intent.Command()
	return s.send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: k, PhysicalKey: k, Code: platform.KeyCodeForXT(sc, lp&(1<<24) != 0), Mods: modifiers, Repeat: lp&(1<<30) != 0})
}
