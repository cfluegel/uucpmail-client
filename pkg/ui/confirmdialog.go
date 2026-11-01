package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
)

// keyButton ist ein Button, der zusätzlich auf Enter/Return reagiert und mit
// den Pfeiltasten links/rechts den Fokus zum Nachbar-Button wechselt
// (Fynes widget.Button aktiviert nur über die Leertaste).
type keyButton struct {
	widget.Button
	canvas   fyne.Canvas
	neighbor fyne.Focusable
	onEscape func()
}

func newKeyButton(label string, tapped func()) *keyButton {
	b := &keyButton{}
	b.Text = label
	b.OnTapped = tapped
	b.ExtendBaseWidget(b)
	return b
}

func (b *keyButton) TypedKey(ev *fyne.KeyEvent) {
	switch ev.Name {
	case fyne.KeyReturn, fyne.KeyEnter:
		if b.OnTapped != nil {
			b.OnTapped()
		}
	case fyne.KeyLeft, fyne.KeyRight:
		if b.canvas != nil && b.neighbor != nil {
			b.canvas.Focus(b.neighbor)
		}
	case fyne.KeyEscape:
		if b.onEscape != nil {
			b.onEscape()
		}
	default:
		b.Button.TypedKey(ev)
	}
}

// showConfirmKeyboard zeigt eine Ja/Nein-Rückfrage, die komplett per Tastatur
// bedienbar ist: Enter aktiviert den fokussierten Button (Start: Ja),
// Pfeiltasten/Tab wechseln zwischen Ja und Nein, Escape bricht ab.
// Nach dem Schließen geht der Fokus zurück in die Mailliste.
func (u *UI) showConfirmKeyboard(title, message string, cb func(bool)) {
	var d dialog.Dialog
	done := func(ok bool) {
		d.Hide()
		u.focusZone(zoneList)
		cb(ok)
	}
	yes := newKeyButton(i18n.T("dialog.yes"), func() { done(true) })
	yes.Importance = widget.HighImportance
	no := newKeyButton(i18n.T("dialog.no"), func() { done(false) })
	cv := u.win.Canvas()
	yes.canvas, yes.neighbor, yes.onEscape = cv, no, func() { done(false) }
	no.canvas, no.neighbor, no.onEscape = cv, yes, func() { done(false) }

	msg := widget.NewLabel(message)
	msg.Wrapping = fyne.TextWrapWord
	msg.Alignment = fyne.TextAlignCenter
	content := container.NewVBox(msg, container.NewCenter(container.NewHBox(no, yes)))
	d = dialog.NewCustomWithoutButtons(title, content, u.win)
	d.SetOnClosed(func() { u.focusZone(zoneList) })
	d.Resize(fyne.NewSize(420, 160))
	d.Show()
	cv.Focus(yes)
}
