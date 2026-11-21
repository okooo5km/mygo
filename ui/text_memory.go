package ui

import "github.com/egoist/mygo/internal/text"

// ReleaseTextMemory lets go of what the process-wide text system holds and can make again: its layout cache and its
// glyph atlases, which go back to their first size. The atlases and the layout cache are shared by every window and
// only a drawn frame evicts from them, which a hidden window never reaches, so an app that recycles or hides its
// windows calls this when none is drawing. Main thread only (the atlases are). The windows that remain draw their next
// frame with fresh layouts and glyphs, and their renderers upload the new atlases whole.
func ReleaseTextMemory() { text.Shared().ReleaseMemory() }
