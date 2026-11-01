package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

type zoneID int

const (
	zoneList zoneID = iota
	zoneContent
)

// setupKeyboard verdrahtet die Tastaturbedienung des Hauptfensters
// (Compose-Fenster haben eigene Canvas und bleiben unberührt):
// Tab wechselt zwischen Mailliste und Mailansicht (focusZone), Cursortasten
// wirken je Zone, Buchstaben-Kürzel gelten in beiden Zonen. Cmd/Ctrl+S bleibt
// als Canvas-Shortcut; der frühere Shift+R-Shortcut entfällt (Shift wird jetzt
// in der fokussierten Zone korrekt erkannt).
func (u *UI) setupKeyboard() {
	u.win.Canvas().AddShortcut(&desktop.CustomShortcut{
		KeyName: fyne.KeyS, Modifier: fyne.KeyModifierShortcutDefault,
	}, func(fyne.Shortcut) { u.runSync() })
	// Cmd+, (macOS) bzw. Ctrl+, (Windows/Linux): Einstellungen, wie üblich.
	u.win.Canvas().AddShortcut(&desktop.CustomShortcut{
		KeyName: fyne.KeyComma, Modifier: fyne.KeyModifierShortcutDefault,
	}, func(fyne.Shortcut) { u.showSettings(false) })

	// Fallback: landet der Canvas-Fokus im Nichts (z.B. nach Klick in einen
	// leeren Bereich), Taste an die zuletzt aktive Zone zurückgeben.
	u.win.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		u.focusZone(u.activeZone)
		u.handleZoneKey(u.activeZone, ev, false)
	})
}

// focusZone setzt den Canvas-Fokus auf die gewünschte Zone.
func (u *UI) focusZone(z zoneID) {
	if z == zoneContent {
		u.win.Canvas().Focus(u.contentZone)
	} else {
		u.win.Canvas().Focus(u.listZone)
	}
}

func (u *UI) toggleZone() {
	if u.activeZone == zoneList {
		u.focusZone(zoneContent)
	} else {
		u.focusZone(zoneList)
	}
}

// handleZoneKey verarbeitet eine Taste in der aktiven Zone.
func (u *UI) handleZoneKey(zone zoneID, ev *fyne.KeyEvent, shift bool) {
	switch ev.Name {
	case fyne.KeyUp, fyne.KeyDown:
		delta := 1
		if ev.Name == fyne.KeyUp {
			delta = -1
		}
		if zone == zoneList {
			u.messageList.moveCursor(delta, shift)
		} else {
			lines := 1
			if shift {
				if lines = u.cfg.ScrollLines; lines <= 0 {
					lines = 10
				}
			}
			u.messageView.scrollBody(delta * lines)
		}
	case fyne.KeyN:
		u.showCompose()
	case fyne.KeyS:
		u.runSync()
	case fyne.KeyR:
		if u.messageList.selectionCount() > 1 {
			return
		}
		u.messageView.composeReply(shift)
	case fyne.KeyF:
		if u.messageList.selectionCount() > 1 {
			u.forwardSelectedAsEML()
			return
		}
		u.messageView.composeForward()
	case fyne.KeyU:
		u.markSelectionUnread()
	case fyne.KeyDelete, fyne.KeyBackspace:
		u.deleteSelection()
	}
}
