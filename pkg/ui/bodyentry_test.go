package ui

import (
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/compose"
)

// quoteBody ähnelt dem Nutzer-Szenario: Antwort mit Zitatblock – lange,
// im Editor umbrechende Zeilen, Leerzeilen dazwischen.
const quoteBody = "Hallo,\n\n> Dies ist eine ziemlich lange zitierte Zeile, die im Editor sicher mehrfach umbrochen wird, weil sie viel Text enthält.\n>\n> Zweite zitierte Zeile mitÜmläuten und ebenfalls genug Text für einen Umbruch im schmalen Fenster.\n\nGruß\n"

// newBodyUnderTest baut ein gerendertes Entry mit schmalem Fenster, damit
// TextWrapWord wirklich umbricht (wie im Compose-Fenster).
func newBodyUnderTest(t *testing.T, wrapped bool, useWrapper bool) (fyne.Window, *widget.Entry) {
	t.Helper()
	var entry *widget.Entry
	if useWrapper {
		e := newTabBackEntry()
		entry = &e.Entry
	} else {
		e := widget.NewMultiLineEntry()
		entry = e
	}
	if wrapped {
		entry.Wrapping = fyne.TextWrapWord
	} else {
		entry.Wrapping = fyne.TextWrapOff
	}
	entry.TextStyle.Monospace = true
	w := test.NewWindow(entry)
	t.Cleanup(w.Close)
	w.Resize(fyne.NewSize(300, 400)) // schmal → Umbrüche
	w.Canvas().Focus(entry)
	return w, entry
}

func pressKey(e *widget.Entry, name fyne.KeyName) {
	e.TypedKey(&fyne.KeyEvent{Name: name})
}

// TestCursorRoundtripConsistency fährt den Cursor per KeyRight durch den
// gesamten Text und prüft nach jedem Schritt, ob CursorTextOffset exakt
// um 1 wächst. Jede Abweichung heißt: (CursorRow,CursorColumn) und der
// interne Textoffset sind auseinandergelaufen – genau der Zustand, aus
// dem Backspace an der falschen Stelle löscht.
func TestCursorRoundtripConsistency(t *testing.T) {
	for _, tc := range []struct {
		name            string
		wrapped, custom bool
	}{
		{"pure/wrap", true, false},
		{"pure/nowrap", false, false},
		{"tabBackEntry/wrap", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, e := newBodyUnderTest(t, tc.wrapped, tc.custom)
			e.SetText(quoteBody)
			e.CursorRow, e.CursorColumn = 0, 0
			e.Refresh()
			runes := []rune(quoteBody)
			for i := 1; i <= len(runes); i++ {
				pressKey(e, fyne.KeyRight)
				if got := e.CursorTextOffset(); got != i {
					t.Fatalf("nach %d×KeyRight: CursorTextOffset=%d, erwartet %d (Kontext %q)",
						i, got, i, string(runes[max(0, i-6):min(len(runes), i+6)]))
				}
			}
		})
	}
}

// TestBackspaceModel prüft Backspace gegen ein Referenzmodell: von jeder
// per Cursortasten erreichten Position p muss Backspace genau die Rune
// p-1 entfernen und den Cursor auf p-1 setzen – auch mehrfach hintereinander
// (das Nutzer-Szenario „Backspace am Zeilenanfang gedrückt halten").
func TestBackspaceModel(t *testing.T) {
	for _, tc := range []struct {
		name            string
		wrapped, custom bool
	}{
		{"pure/wrap", true, false},
		{"pure/nowrap", false, false},
		{"tabBackEntry/wrap", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runes := []rune(quoteBody)
			// Startpositionen: alle Zeilenanfänge (dort drückt man Backspace,
			// um Zeilen zu verbinden) plus die Positionen rund um Leerzeilen.
			var starts []int
			for i, r := range runes {
				if r == '\n' && i+1 < len(runes) {
					starts = append(starts, i+1)
				}
			}
			for _, start := range starts {
				t.Run(fmt.Sprintf("start=%d", start), func(t *testing.T) {
					_, e := newBodyUnderTest(t, tc.wrapped, tc.custom)
					e.SetText(quoteBody)
					e.CursorRow, e.CursorColumn = 0, 0
					e.Refresh()
					for i := 0; i < start; i++ {
						pressKey(e, fyne.KeyRight)
					}
					if got := e.CursorTextOffset(); got != start {
						t.Fatalf("Positionierung: CursorTextOffset=%d, erwartet %d", got, start)
					}
					model := append([]rune(nil), runes...)
					pos := start
					for presses := 0; presses < 5 && pos > 0; presses++ {
						pressKey(e, fyne.KeyBackspace)
						model = append(model[:pos-1], model[pos:]...)
						pos--
						if e.Text != string(model) {
							t.Fatalf("nach Backspace #%d: Text weicht ab\n got: %q\nwant: %q",
								presses+1, e.Text, string(model))
						}
						if got := e.CursorTextOffset(); got != pos {
							t.Fatalf("nach Backspace #%d: CursorTextOffset=%d, erwartet %d",
								presses+1, got, pos)
						}
					}
				})
			}
		})
	}
}

// canonicalRows fährt per KeyRight durch den Text und liefert für jede
// Position die kanonische visuelle Zeile (CursorRow) – das Orakel für
// alle visuellen Navigationstests.
func canonicalRows(e *widget.Entry, text string) []int {
	e.SetText(text)
	e.CursorRow, e.CursorColumn = 0, 0
	e.Refresh()
	rows := make([]int, len([]rune(text))+1)
	rows[0] = e.CursorRow
	for i := 1; i < len(rows); i++ {
		pressKey(e, fyne.KeyRight)
		rows[i] = e.CursorRow
	}
	return rows
}

// TestVisualNavigationConsistency prüft KeyDown/KeyUp/Home/End gegen die
// kanonische Karte: Nach jeder Navigation müssen (CursorRow, Offset)
// zueinander passen – Abweichungen sind genau die Zustände, aus denen
// Backspace „falsche" Zeichen löscht.
func TestVisualNavigationConsistency(t *testing.T) {
	_, e := newBodyUnderTest(t, true, false)
	rows := canonicalRows(e, quoteBody)
	total := rows[len(rows)-1] + 1

	// Zeilenstart-Offsets je visueller Zeile aus der Karte ableiten.
	rowStart := make([]int, total)
	for pos := len(rows) - 1; pos >= 0; pos-- {
		rowStart[rows[pos]] = pos
	}

	// Von 0 aus mit KeyDown nach unten: nach jedem Down (von Spalte 0)
	// muss der Offset exakt der Zeilenstart der nächsten Zeile sein.
	e.CursorRow, e.CursorColumn = 0, 0
	e.Refresh()
	for want := 1; want < total; want++ {
		pressKey(e, fyne.KeyDown)
		pressKey(e, fyne.KeyHome)
		got := e.CursorTextOffset()
		if got != rowStart[want] {
			t.Errorf("KeyDown+Home auf Zeile %d: Offset=%d, erwartet Zeilenstart %d", want, got, rowStart[want])
		}
		if e.CursorRow != want {
			t.Errorf("KeyDown auf Zeile %d: CursorRow=%d", want, e.CursorRow)
		}
		// Konsistenz Offset↔Row (gezeichneter Cursor vs. interner Offset).
		if canonical := rows[got]; e.CursorRow != canonical {
			t.Errorf("Zeile %d: CursorRow=%d, kanonisch für Offset %d wäre %d", want, e.CursorRow, got, canonical)
		}
	}

	// Und wieder hoch: Up+Home muss jeweils den Zeilenstart treffen.
	for want := total - 2; want >= 0; want-- {
		pressKey(e, fyne.KeyUp)
		pressKey(e, fyne.KeyHome)
		if got := e.CursorTextOffset(); got != rowStart[want] {
			t.Errorf("KeyUp+Home auf Zeile %d: Offset=%d, erwartet %d", want, got, rowStart[want])
		}
	}
}

// TestClickPlacesCursorAtLineStart klickt (MouseDown/Up) an den Anfang
// jeder visuellen Zeile und prüft, ob der interne Offset dem Zeilenstart
// der kanonischen Karte entspricht – deckt Fehlzuordnungen Pixel→Zeile ab.
func TestClickPlacesCursorAtLineStart(t *testing.T) {
	_, e := newBodyUnderTest(t, true, false)
	rows := canonicalRows(e, quoteBody)
	total := rows[len(rows)-1] + 1
	rowStart := make([]int, total)
	for pos := len(rows) - 1; pos >= 0; pos-- {
		rowStart[rows[pos]] = pos
	}

	th := e.Theme()
	pad := th.Size(theme.SizeNameInnerPadding)
	textSize := th.Size(theme.SizeNameText)
	lineH := fyne.MeasureText("M", textSize, e.TextStyle).Height

	for row := 0; row < total; row++ {
		x := pad + 1
		y := pad + float32(row)*lineH + lineH/2
		ev := &desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(x, y)}, Button: desktop.MouseButtonPrimary}
		e.MouseDown(ev)
		e.MouseUp(ev)
		if got := e.CursorTextOffset(); got != rowStart[row] {
			t.Errorf("Klick auf Zeile %d: Offset=%d, erwartet Zeilenstart %d (CursorRow=%d)", row, got, rowStart[row], e.CursorRow)
		}
	}
}

// TestCursorStableOnRewrap: Ändert sich die effektive Breite (Fenster-
// Resize oder Scrollbalken erscheint/verschwindet), brechen die Zeilen neu
// um. Der Cursor muss dabei auf derselben TEXTposition bleiben – behält
// Fyne stattdessen Row/Col bei (Issue #3272), springt der Offset und
// nachfolgende Backspaces löschen an der falschen Stelle.
func TestCursorStableOnRewrap(t *testing.T) {
	w, e := newBodyUnderTest(t, true, false)
	e.SetText(quoteBody)
	e.CursorRow, e.CursorColumn = 0, 0
	e.Refresh()
	// Cursor auf den Anfang der Zeile „Gruß" (nach der zweiten Leerzeile).
	runes := []rune(quoteBody)
	target := 0
	for i := len(runes) - 2; i > 0; i-- { // letzte Zeile „Gruß\n"
		if runes[i] == '\n' {
			target = i + 1
			break
		}
	}
	for i := 0; i < target; i++ {
		pressKey(e, fyne.KeyRight)
	}
	if got := e.CursorTextOffset(); got != target {
		t.Fatalf("Vorbereitung: Offset=%d, erwartet %d", got, target)
	}
	// Breite ändern → Re-Wrap. Der Textoffset darf sich NICHT ändern.
	for _, width := range []float32{500, 220, 300} {
		w.Resize(fyne.NewSize(width, 400))
		if got := e.CursorTextOffset(); got != target {
			t.Errorf("nach Resize auf Breite %.0f: CursorTextOffset=%d, erwartet %d (CursorRow=%d, CursorColumn=%d)",
				width, got, target, e.CursorRow, e.CursorColumn)
		}
	}
}

// TestSnippetInsertWrappedUmlauts prüft den Formatierungsknopf-Pfad
// (insertAtCursor → SetText → moveCursor) mit Umlauten und umbrochenen
// Zeilen: Einfügepunkt und End-Cursor müssen exakt stimmen.
func TestSnippetInsertWrappedUmlauts(t *testing.T) {
	_, e := newBodyUnderTest(t, true, false)
	text := "Grüße an die Bären!\n\nDies ist eine ziemlich lange Zeile mit Ümläuten, die im schmalen Fenster ganz sicher mehrfach umbrochen wird, södass visuelle und logische Zeilen auseinanderfallen.\n"
	e.SetText(text)
	e.CursorRow, e.CursorColumn = 0, 0
	e.Refresh()

	// Cursor mitten in die lange (umbrochene) Zeile fahren: direkt vor "södass".
	runes := []rune(text)
	targetRune := 0
	for i := range runes {
		if string(runes[i:min(len(runes), i+6)]) == "södass" {
			targetRune = i
			break
		}
	}
	if targetRune == 0 {
		t.Fatal("Marker nicht gefunden")
	}
	for i := 0; i < targetRune; i++ {
		pressKey(e, fyne.KeyRight)
	}

	bold := compose.MarkdownSnippet{Prefix: "**", Suffix: "**"}
	newText, cursor := insertAtCursor(e, bold)
	e.SetText(newText)
	moveCursor(e, cursor)

	wantText := string(runes[:targetRune]) + "****" + string(runes[targetRune:])
	if e.Text != wantText {
		t.Fatalf("Einfügepunkt falsch:\n got: %q\nwant: %q", e.Text, wantText)
	}
	// Cursor muss zwischen den Sternchen stehen: tippt man jetzt "fett",
	// steht es zwischen ** und **.
	for _, r := range "fett" {
		e.TypedRune(r)
	}
	if want := string(runes[:targetRune]) + "**fett**" + string(runes[targetRune:]); e.Text != want {
		t.Fatalf("Cursor nach moveCursor falsch:\n got: %q\nwant: %q", e.Text, want)
	}
}

// TestEditingPass deckt die restlichen Eingabepfade des Nachrichtenfelds
// ab: Auswahl per Shift+Pfeile (+Löschen/Überschreiben), Entf-Taste,
// Select-All, Undo/Redo, mehrzeiliges Einfügen, Emoji.
func TestEditingPass(t *testing.T) {
	newBody := func(t *testing.T, text string) *widget.Entry {
		_, e := newBodyUnderTest(t, true, false)
		e.SetText(text)
		e.CursorRow, e.CursorColumn = 0, 0
		e.Refresh()
		return e
	}
	shiftPress := func(e *widget.Entry, names ...fyne.KeyName) {
		e.KeyDown(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})
		for _, n := range names {
			pressKey(e, n)
		}
		e.KeyUp(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})
	}

	t.Run("Entf-Taste am Zeilenende verbindet mit Folgezeile", func(t *testing.T) {
		e := newBody(t, "ab\ncd\n")
		pressKey(e, fyne.KeyRight)
		pressKey(e, fyne.KeyRight) // hinter "ab"
		pressKey(e, fyne.KeyDelete)
		if e.Text != "abcd\n" || e.CursorTextOffset() != 2 {
			t.Fatalf("got %q, Offset %d", e.Text, e.CursorTextOffset())
		}
	})

	t.Run("Shift-Auswahl über Zeilengrenze + Backspace", func(t *testing.T) {
		e := newBody(t, "eins\nzwei\ndrei\n")
		shiftPress(e, fyne.KeyDown) // selektiert "eins\n"... bis Zeile 2 Spalte 0
		if got := e.SelectedText(); got != "eins\n" {
			t.Fatalf("SelectedText=%q", got)
		}
		pressKey(e, fyne.KeyBackspace)
		if e.Text != "zwei\ndrei\n" || e.CursorTextOffset() != 0 {
			t.Fatalf("got %q, Offset %d", e.Text, e.CursorTextOffset())
		}
	})

	t.Run("Auswahl überschreiben durch Tippen", func(t *testing.T) {
		e := newBody(t, "alt neu\n")
		for i := 0; i < 3; i++ {
			shiftPress(e, fyne.KeyRight) // "alt" selektieren
		}
		e.TypedRune('X')
		if e.Text != "X neu\n" {
			t.Fatalf("got %q", e.Text)
		}
	})

	t.Run("Undo/Redo nach Tippen", func(t *testing.T) {
		e := newBody(t, "")
		for _, r := range "Grüße" {
			e.TypedRune(r)
		}
		e.Undo()
		if e.Text == "Grüße" {
			t.Fatalf("Undo ohne Wirkung: %q", e.Text)
		}
		e.Redo()
		if e.Text != "Grüße" {
			t.Fatalf("Redo stellt nicht wieder her: %q", e.Text)
		}
	})

	t.Run("Mehrzeiliges Einfügen an Cursorposition", func(t *testing.T) {
		e := newBody(t, "kopf\nfuß\n")
		for i := 0; i < 5; i++ {
			pressKey(e, fyne.KeyRight) // Anfang Zeile 2
		}
		cb := test.NewClipboard()
		cb.SetContent("mitte1\nmitte2\n")
		e.TypedShortcut(&fyne.ShortcutPaste{Clipboard: cb})
		if e.Text != "kopf\nmitte1\nmitte2\nfuß\n" {
			t.Fatalf("got %q", e.Text)
		}
		if got := e.CursorTextOffset(); got != len([]rune("kopf\nmitte1\nmitte2\n")) {
			t.Fatalf("Offset nach Einfügen: %d", got)
		}
	})

	t.Run("Emoji: Backspace löscht genau eine Rune", func(t *testing.T) {
		e := newBody(t, "")
		for _, r := range "ok👍!" {
			e.TypedRune(r)
		}
		pressKey(e, fyne.KeyBackspace)
		pressKey(e, fyne.KeyBackspace)
		if e.Text != "ok" {
			t.Fatalf("got %q", e.Text)
		}
	})

	t.Run("Select-All + Tippen ersetzt alles", func(t *testing.T) {
		e := newBody(t, quoteBody)
		e.TypedShortcut(&fyne.ShortcutSelectAll{})
		e.TypedRune('x')
		if e.Text != "x" {
			t.Fatalf("got %q", e.Text)
		}
	})
}

// TestBackspaceWrapBoundaryWorkaround sichert den Workaround für
// fyne-io/fyne#6649: Endet die Zeile über dem Cursor mit einem Leerzeichen
// exakt an der Umbruchkante, setzte Fyne den Cursor nach Backspace ans Ende
// der nächsten logischen Zeile (statt an den Join-Punkt). Der Sweep schiebt
// das Zeilenende themenunabhängig über die Umbruchgrenze; ohne den Workaround
// in tabBackEntry.TypedKey schlägt er bei einzelnen Breiten fehl.
func TestBackspaceWrapBoundaryWorkaround(t *testing.T) {
	for pad := 0; pad <= 60; pad++ {
		t.Run(fmt.Sprintf("pad%02d", pad), func(t *testing.T) {
			e := newTabBackEntry()
			e.Wrapping = fyne.TextWrapWord
			e.TextStyle.Monospace = true
			w := test.NewWindow(e)
			t.Cleanup(w.Close)
			w.Resize(fyne.NewSize(600, 400))
			w.Canvas().Focus(e)

			line1 := "dies ist ein Test in dem ich so lange zeilen " +
				strings.Repeat("y ", pad/2) + "eingebe sonst " // Leerzeichen am Ende!
			e.SetText(line1 + "\n\nOn 02.11.2026 20:28, Christoph wrote:\n> Hallo Christoph")

			// Cursor an den Anfang der Leerzeile navigieren (öffentliche Pfade).
			target := len([]rune(line1)) + 1
			e.CursorRow, e.CursorColumn = 0, 0
			for i := 0; i < len(e.Text)+10 && e.CursorTextOffset() != target; i++ {
				name := fyne.KeyRight
				if e.CursorTextOffset() > target {
					name = fyne.KeyLeft
				}
				e.TypedKey(&fyne.KeyEvent{Name: name})
			}
			if e.CursorTextOffset() != target {
				t.Fatalf("Setup: Offset %d, want %d", e.CursorTextOffset(), target)
			}

			e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})

			want := line1 + "\nOn 02.11.2026 20:28, Christoph wrote:\n> Hallo Christoph"
			if e.Text != want {
				t.Fatalf("Text nach Backspace: %q", e.Text)
			}
			// Ideal ist der Join-Punkt (Ende line1); endet line1 mit einem
			// Leerzeichen exakt an der Umbruchkante, ist diese Position in
			// Fyne nicht darstellbar – dann korrigiert der Workaround auf
			// eine Rune davor (gleiche Zeile, optisch dieselbe Stelle).
			wantOff := len([]rune(line1))
			if got := e.CursorTextOffset(); got != wantOff && got != wantOff-1 {
				t.Errorf("Cursor nach Backspace: Offset %d, want %d (oder %d)", got, wantOff, wantOff-1)
			}
		})
	}
}

// Gegenproben: Der Workaround darf Normalfälle nicht verändern.
func TestBackspaceWorkaroundLeavesNormalCasesAlone(t *testing.T) {
	newBody := func(t *testing.T, text string) *tabBackEntry {
		t.Helper()
		e := newTabBackEntry()
		e.Wrapping = fyne.TextWrapWord
		e.TextStyle.Monospace = true
		w := test.NewWindow(e)
		t.Cleanup(w.Close)
		w.Resize(fyne.NewSize(300, 400))
		w.Canvas().Focus(e)
		e.SetText(text)
		return e
	}

	t.Run("Backspace mitten im Text (Umlaut)", func(t *testing.T) {
		e := newBody(t, "grüße\nwelt")
		for i := 0; i < 3; i++ { // hinter "grü"
			e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyRight})
		}
		e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})
		if e.Text != "grße\nwelt" || e.CursorTextOffset() != 2 {
			t.Fatalf("got %q offset %d", e.Text, e.CursorTextOffset())
		}
	})

	t.Run("Backspace am Textanfang ist ein No-Op", func(t *testing.T) {
		e := newBody(t, "abc")
		e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})
		if e.Text != "abc" || e.CursorTextOffset() != 0 {
			t.Fatalf("got %q offset %d", e.Text, e.CursorTextOffset())
		}
	})

	t.Run("Backspace mit Selektion löscht die Selektion", func(t *testing.T) {
		e := newBody(t, "abcdef")
		for i := 0; i < 2; i++ {
			e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyRight})
		}
		e.KeyDown(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})
		e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyRight})
		e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyRight})
		e.KeyUp(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})
		if e.SelectedText() != "cd" {
			t.Fatalf("Selektion: %q", e.SelectedText())
		}
		e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})
		if e.Text != "abef" || e.CursorTextOffset() != 2 {
			t.Fatalf("got %q offset %d", e.Text, e.CursorTextOffset())
		}
	})

	t.Run("Delete am Zeilenende zieht Folgezeile hoch", func(t *testing.T) {
		e := newBody(t, "ab\ncd")
		for i := 0; i < 2; i++ {
			e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyRight})
		}
		e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDelete})
		if e.Text != "abcd" || e.CursorTextOffset() != 2 {
			t.Fatalf("got %q offset %d", e.Text, e.CursorTextOffset())
		}
	})
}
