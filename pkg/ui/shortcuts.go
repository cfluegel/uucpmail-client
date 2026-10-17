package ui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// setupShortcuts registriert die kleine Shortcut-Basis auf dem Hauptfenster-
// Canvas (Compose-Fenster haben eigene Canvas und bleiben unberührt):
// N=Neu, R=Antworten, Shift+R=Allen antworten, F=Weiterleiten,
// Entf/Backspace=Löschen, Cmd/Ctrl+S=Sync. Die Buchstaben-Kürzel feuern nur,
// wenn kein Eingabefeld den Fokus hat (SetOnTypedKey-Semantik).
func (u *UI) setupShortcuts() {
	canvas := u.win.Canvas()

	var lastShiftR time.Time
	canvas.AddShortcut(&desktop.CustomShortcut{
		KeyName: fyne.KeyR, Modifier: fyne.KeyModifierShift,
	}, func(fyne.Shortcut) {
		lastShiftR = time.Now()
		u.messageView.composeReply(true)
	})
	canvas.AddShortcut(&desktop.CustomShortcut{
		KeyName: fyne.KeyS, Modifier: fyne.KeyModifierShortcutDefault,
	}, func(fyne.Shortcut) { u.runSync() })

	canvas.SetOnTypedKey(func(ev *fyne.KeyEvent) {
		switch ev.Name {
		case fyne.KeyN:
			u.showCompose()
		case fyne.KeyR:
			// Shift+R löst zusätzlich ein TypedKey R aus – unterdrücken.
			if time.Since(lastShiftR) < 200*time.Millisecond {
				return
			}
			u.messageView.composeReply(false)
		case fyne.KeyF:
			u.messageView.composeForward()
		case fyne.KeyDelete, fyne.KeyBackspace:
			u.messageView.deleteCurrent()
		}
	})
}
