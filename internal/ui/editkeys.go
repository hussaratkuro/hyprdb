package ui

import (
	"unicode"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/widget"
)

// Gio's editor only moves/deletes by word with its "shortcut alt" modifier,
// which does not fire for Ctrl on Linux in practice, so every editor in the
// app routes Ctrl(+Shift)+Left/Right/Backspace/Delete through these.

func wordEdge(runes []rune, pos, dir int) int {
	pos = min(max(pos, 0), len(runes))
	if dir < 0 {
		for pos > 0 && unicode.IsSpace(runes[pos-1]) {
			pos--
		}
		for pos > 0 && !unicode.IsSpace(runes[pos-1]) {
			pos--
		}
	} else {
		for pos < len(runes) && unicode.IsSpace(runes[pos]) {
			pos++
		}
		for pos < len(runes) && !unicode.IsSpace(runes[pos]) {
			pos++
		}
	}
	return pos
}

func moveWordIn(ed *widget.Editor, dir int, extend bool) {
	start, end := ed.Selection()
	to := wordEdge([]rune(ed.Text()), start, dir)
	if !extend {
		end = to
	}
	ed.SetCaret(to, end)
}

func deleteWordIn(ed *widget.Editor, dir int) {
	start, end := ed.Selection()
	if start != end {
		ed.Delete(1)
		return
	}
	to := wordEdge([]rune(ed.Text()), start, dir)
	if to == start {
		return
	}
	ed.SetCaret(to, start)
	ed.Delete(1)
}

// editKeys must run before the editor's Layout/Update so it gets the keys first.
func editKeys(gtx C, ed *widget.Editor) {
	filters := []event.Filter{
		key.Filter{Focus: ed, Name: key.NameLeftArrow, Required: key.ModCtrl, Optional: key.ModShift},
		key.Filter{Focus: ed, Name: key.NameRightArrow, Required: key.ModCtrl, Optional: key.ModShift},
		key.Filter{Focus: ed, Name: key.NameDeleteBackward, Required: key.ModCtrl},
		key.Filter{Focus: ed, Name: key.NameDeleteForward, Required: key.ModCtrl},
	}
	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			return
		}
		k, ok := ev.(key.Event)
		if !ok || k.State != key.Press {
			continue
		}
		switch k.Name {
		case key.NameLeftArrow:
			moveWordIn(ed, -1, k.Modifiers.Contain(key.ModShift))
		case key.NameRightArrow:
			moveWordIn(ed, 1, k.Modifiers.Contain(key.ModShift))
		case key.NameDeleteBackward:
			if !ed.ReadOnly {
				deleteWordIn(ed, -1)
			}
		case key.NameDeleteForward:
			if !ed.ReadOnly {
				deleteWordIn(ed, 1)
			}
		}
	}
}
