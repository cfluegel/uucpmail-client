package ui

import (
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"github.com/cfluegel/uucpmail-client/pkg/pgp"
)

// promptPassphrase fragt die Passphrase für einen eigenen Schlüssel ab,
// entsperrt ihn und legt ihn in den Cache. Bei falscher Passphrase öffnet
// sich ein FRISCHER Dialog mit Fehlerhinweis (Fyne-Dialoge sind nicht
// wiederverwendbar). cb(nil, false) bei Abbruch.
func (u *UI) promptPassphrase(win fyne.Window, entity *pgp.Entity, info pgp.KeyInfo,
	errText string, cb func(unlocked *pgp.Entity, ok bool)) {

	passEntry := widget.NewPasswordEntry()
	// Beschriftung über dem Feld (eigene Zeile), damit das Passwortfeld die
	// volle Dialogbreite bekommt – bequemer für lange Passphrases.
	label := widget.NewLabel(fmt.Sprintf(i18n.T("pgp.pass.label"), info.Email))
	label.Wrapping = fyne.TextWrapWord
	rows := []fyne.CanvasObject{label, passEntry}
	if errText != "" {
		errLabel := widget.NewLabel(errText)
		errLabel.Importance = widget.DangerImportance
		rows = append([]fyne.CanvasObject{errLabel}, rows...)
	}
	content := container.NewVBox(rows...)
	d := dialog.NewCustomConfirm(i18n.T("pgp.pass.title"), i18n.T("compose.send"), i18n.T("compose.cancel"),
		content, func(ok bool) {
			if !ok {
				cb(nil, false)
				return
			}
			unlocked, err := pgp.Unlock(entity, passEntry.Text)
			if errors.Is(err, pgp.ErrWrongPassphrase) {
				u.promptPassphrase(win, entity, info, i18n.T("pgp.err.wrongpass"), cb)
				return
			}
			if err != nil {
				u.showError(err)
				cb(nil, false)
				return
			}
			u.pgpCache.Put(info.Fingerprint, unlocked)
			cb(unlocked, true)
		}, win)
	d.Resize(fyne.NewSize(520, 180))
	d.Show()
	win.Canvas().Focus(passEntry)
}

// ensureUnlockedSigner stellt sicher, dass der eigene Schlüssel der Identität
// entsperrt im Cache liegt, und ruft dann done(true) auf.
func (u *UI) ensureUnlockedSigner(win fyne.Window, identity string, done func(ok bool)) {
	own := u.keyring.FindPrivateByEmail(identity)
	if own == nil {
		u.showError(fmt.Errorf(i18n.T("pgp.err.noprivkey"), identity))
		done(false)
		return
	}
	infos := u.keyring.ListPrivate()
	var info pgp.KeyInfo
	for _, i := range infos {
		if i.Email != "" && u.keyring.FindPrivateByEmail(i.Email) == own {
			info = i
			break
		}
	}
	if !pgp.NeedsPassphrase(own) {
		done(true)
		return
	}
	if u.pgpCache.Get(info.Fingerprint) != nil {
		done(true)
		return
	}
	u.promptPassphrase(win, own, info, "", func(_ *pgp.Entity, ok bool) { done(ok) })
}
