package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// tabJumpEntry ist ein Entry, dessen Tab-Taste direkt zu einem Ziel-Widget
// springt statt die Standard-Traversierung zu durchlaufen (überspringt z. B.
// die Formatierungsleiste zwischen Betreff und Nachrichtentext).
// Shift+Tab behält die normale Rückwärts-Traversierung. Shift wird wie in
// focusZone über desktop.Keyable verfolgt (fyne.KeyEvent trägt keine Modifier).
type tabJumpEntry struct {
	widget.Entry
	next   fyne.Focusable
	canvas fyne.Canvas
	shift  bool
}

var (
	_ fyne.Tabbable   = (*tabJumpEntry)(nil)
	_ desktop.Keyable = (*tabJumpEntry)(nil)
)

func newTabJumpEntry() *tabJumpEntry {
	e := &tabJumpEntry{}
	e.ExtendBaseWidget(e)
	return e
}

// AcceptsTab sorgt dafür, dass Tab als TypedKey ankommt.
func (e *tabJumpEntry) AcceptsTab() bool { return true }

func (e *tabJumpEntry) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyTab && e.canvas != nil {
		if e.shift {
			e.canvas.FocusPrevious()
		} else if e.next != nil {
			e.canvas.Focus(e.next)
		}
		return
	}
	e.Entry.TypedKey(ev)
}

func (e *tabJumpEntry) KeyDown(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyShiftLeft || ev.Name == desktop.KeyShiftRight {
		e.shift = true
	}
	e.Entry.KeyDown(ev)
}

func (e *tabJumpEntry) KeyUp(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyShiftLeft || ev.Name == desktop.KeyShiftRight {
		e.shift = false
	}
	e.Entry.KeyUp(ev)
}

func (e *tabJumpEntry) FocusLost() {
	e.shift = false // KeyUp kann bei Fokusverlust verloren gehen
	e.Entry.FocusLost()
}

// tabBackEntry ist ein mehrzeiliges Entry, aus dem Shift+Tab rückwärts zu
// einem Ziel-Widget springt; normales Tab fügt weiterhin einen Tabulator ein
// (natives MultiLine-Verhalten).
type tabBackEntry struct {
	widget.Entry
	prev   fyne.Focusable
	canvas fyne.Canvas
	shift  bool
}

var (
	_ fyne.Tabbable   = (*tabBackEntry)(nil)
	_ desktop.Keyable = (*tabBackEntry)(nil)
)

func newTabBackEntry() *tabBackEntry {
	e := &tabBackEntry{}
	e.MultiLine = true
	e.ExtendBaseWidget(e)
	return e
}

func (e *tabBackEntry) AcceptsTab() bool { return true }

func (e *tabBackEntry) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyTab && e.shift && e.canvas != nil && e.prev != nil {
		e.canvas.Focus(e.prev)
		return
	}
	// Workaround fyne-io/fyne#6649: Endet die Zeile über dem Cursor mit einem
	// Leerzeichen exakt an der Umbruchkante, setzt Fyne den Cursor nach
	// Backspace/Delete ans Ende der NÄCHSTEN logischen Zeile. Ohne Selektion
	// ist die Sollposition trivial (Backspace: eins zurück, Delete: bleibt) –
	// abweichende Positionen werden korrigiert. (Rückbau, sobald Upstream fixt.)
	if (ev.Name == fyne.KeyBackspace || ev.Name == fyne.KeyDelete) && e.SelectedText() == "" {
		before := e.Entry.CursorTextOffset() // Runen-Offset
		e.Entry.TypedKey(ev)
		want := before
		if ev.Name == fyne.KeyBackspace && before > 0 {
			want = before - 1
		}
		runes := []rune(e.Entry.Text)
		if want > len(runes) {
			want = len(runes)
		}
		if want >= 0 && e.Entry.CursorTextOffset() != want {
			moveCursor(&e.Entry, len(string(runes[:want])))
		}
		if e.Entry.CursorTextOffset() != want && want > 0 {
			// Liegt 'want' direkt hinter einem Leerzeichen an der Umbruch-
			// kante, hat die Position in Fyne keine (Row,Col)-Darstellung
			// (auch Pfeiltasten überspringen sie). Nächstbester Punkt ist
			// eine Rune davor: gleiche Zeile, optisch dieselbe Stelle.
			moveCursor(&e.Entry, len(string(runes[:want-1])))
		}
		return
	}
	e.Entry.TypedKey(ev)
}

func (e *tabBackEntry) KeyDown(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyShiftLeft || ev.Name == desktop.KeyShiftRight {
		e.shift = true
	}
	e.Entry.KeyDown(ev)
}

func (e *tabBackEntry) KeyUp(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyShiftLeft || ev.Name == desktop.KeyShiftRight {
		e.shift = false
	}
	e.Entry.KeyUp(ev)
}

func (e *tabBackEntry) FocusLost() {
	e.shift = false
	e.Entry.FocusLost()
}
