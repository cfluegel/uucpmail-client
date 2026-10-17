package ui

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/contacts"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"github.com/cfluegel/uucpmail-client/pkg/pgp"
)

// contactRow baut eine 3-Spalten-Zeile (Nachname | Vorname | E-Mail);
// 🔑 markiert Kontakte mit hinterlegtem PGP-Schlüssel.
func contactRowTemplate() fyne.CanvasObject {
	last := widget.NewLabel("last")
	last.Truncation = fyne.TextTruncateEllipsis
	first := widget.NewLabel("first")
	first.Truncation = fyne.TextTruncateEllipsis
	email := widget.NewLabel("email")
	email.Truncation = fyne.TextTruncateEllipsis
	return container.New(layout.NewGridLayoutWithColumns(3), last, first, email)
}

func fillContactRow(o fyne.CanvasObject, c contacts.Contact) {
	row := o.(*fyne.Container)
	lastText := c.LastName
	if c.KeyFingerprint != "" {
		lastText = "🔑 " + lastText
	}
	row.Objects[0].(*widget.Label).SetText(lastText)
	row.Objects[1].(*widget.Label).SetText(c.FirstName)
	row.Objects[2].(*widget.Label).SetText(c.Email)
}

func contactHeader() fyne.CanvasObject {
	mk := func(key string) fyne.CanvasObject {
		l := widget.NewLabel(i18n.T(key))
		l.TextStyle.Bold = true
		return l
	}
	return container.New(layout.NewGridLayoutWithColumns(3),
		mk("ab.col.last"), mk("ab.col.first"), mk("ab.col.email"))
}

// showContactsWindow öffnet die Kontaktverwaltung als Dialog über dem
// Hauptfenster (Toolbar-Button rechts neben Synchronisieren).
func (u *UI) showContactsWindow() {
	content := u.buildContactsTab(u.win)
	d := dialog.NewCustom(i18n.T("settings.contacts"), i18n.T("contacts.close"), content, u.win)
	d.Resize(fyne.NewSize(640, 480))
	d.Show()
}

// buildContactsTab baut die Kontaktverwaltung (CRUD-Liste).
func (u *UI) buildContactsTab(win fyne.Window) fyne.CanvasObject {
	items := u.contacts.List()
	selected := -1

	var list *widget.List
	list = widget.NewList(
		func() int { return len(items) },
		func() fyne.CanvasObject { return contactRowTemplate() },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < len(items) {
				fillContactRow(o, items[i])
			}
		},
	)
	list.OnSelected = func(i widget.ListItemID) { selected = i }

	reload := func() {
		items = u.contacts.List()
		selected = -1
		list.UnselectAll()
		list.Refresh()
	}

	newBtn := widget.NewButton(i18n.T("contacts.new"), func() {
		u.showContactEditDialog(win, nil, reload)
	})
	editBtn := widget.NewButton(i18n.T("contacts.edit"), func() {
		if selected >= 0 && selected < len(items) {
			c := items[selected]
			u.showContactEditDialog(win, &c, reload)
		}
	})
	delBtn := widget.NewButton(i18n.T("contacts.delete"), func() {
		if selected < 0 || selected >= len(items) {
			return
		}
		c := items[selected]
		dialog.ShowConfirm(i18n.T("contacts.delete"), i18n.T("contacts.delete.confirm"),
			func(ok bool) {
				if !ok {
					return
				}
				if err := u.contacts.Delete(c.Email); err != nil {
					u.showError(contactErr(err))
					return
				}
				reload()
			}, win)
	})

	empty := widget.NewLabel(i18n.T("contacts.empty"))
	top := container.NewVBox(contactHeader(), widget.NewSeparator())
	bottom := container.NewHBox(newBtn, editBtn, delBtn)
	if len(items) == 0 {
		top.Add(empty)
	}
	return container.NewBorder(top, bottom, nil, nil, list)
}

// showContactEditDialog ist der gemeinsame Anlege-/Bearbeiten-Dialog
// (auch vom Compose-Picker genutzt). existing == nil → neuer Kontakt.
func (u *UI) showContactEditDialog(win fyne.Window, existing *contacts.Contact, done func()) {
	u.showContactEditDialogPrefilled(win, existing, contacts.Contact{}, "", done)
}

// showContactEditDialogPrefilled erlaubt das Wiedereröffnen mit eingegebenen
// Werten nach einem Validierungsfehler (NewCustomConfirm schließt bei Confirm).
func (u *UI) showContactEditDialogPrefilled(win fyne.Window, existing *contacts.Contact,
	draft contacts.Contact, errText string, done func()) {

	initial := draft
	if initial == (contacts.Contact{}) && existing != nil {
		initial = *existing
	}

	firstEntry := widget.NewEntry()
	firstEntry.SetText(initial.FirstName)
	lastEntry := widget.NewEntry()
	lastEntry.SetText(initial.LastName)
	emailEntry := widget.NewEntry()
	emailEntry.SetText(initial.Email)
	emailEntry.SetPlaceHolder("name@example.com")

	keyFpr := initial.KeyFingerprint
	keyStatus := widget.NewLabel("")
	keyStatus.Wrapping = fyne.TextWrapWord
	updateKeyStatus := func() {
		if keyFpr != "" {
			keyStatus.SetText(fmt.Sprintf(i18n.T("contacts.key.set"), groupFpr(keyFpr)))
		} else {
			keyStatus.SetText(i18n.T("contacts.key.none"))
		}
	}
	updateKeyStatus()

	keyPaste := widget.NewMultiLineEntry()
	keyPaste.SetPlaceHolder(i18n.T("contacts.key.paste"))
	keyPaste.Wrapping = fyne.TextWrapBreak
	keyPaste.SetMinRowsVisible(4)

	fileBtn := widget.NewButton(i18n.T("settings.pgp.fromfile"), func() {
		fd := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
			if err != nil || rc == nil {
				return
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				u.showError(err)
				return
			}
			keyPaste.SetText(string(data))
		}, win)
		fd.Show()
	})
	removeKeyBtn := widget.NewButton(i18n.T("contacts.key.remove"), func() {
		keyFpr = ""
		updateKeyStatus()
	})
	if keyFpr == "" {
		removeKeyBtn.Hide()
	}

	rows := []fyne.CanvasObject{}
	if errText != "" {
		errLabel := widget.NewLabel(errText)
		errLabel.Importance = widget.DangerImportance
		errLabel.Wrapping = fyne.TextWrapWord
		rows = append(rows, errLabel)
	}
	form := widget.NewForm(
		widget.NewFormItem(i18n.T("contacts.first"), firstEntry),
		widget.NewFormItem(i18n.T("contacts.last"), lastEntry),
		widget.NewFormItem(i18n.T("contacts.email"), emailEntry),
	)
	rows = append(rows, form, widget.NewSeparator(), keyStatus, keyPaste,
		container.NewHBox(fileBtn, removeKeyBtn))
	content := container.NewVBox(rows...)

	oldEmail := ""
	if existing != nil {
		oldEmail = existing.Email
	}
	d := dialog.NewCustomConfirm(i18n.T("settings.contacts"),
		i18n.T("settings.save"), i18n.T("compose.cancel"),
		container.NewVScroll(content), func(ok bool) {
			if !ok {
				return
			}
			c := contacts.Contact{
				FirstName:      firstEntry.Text,
				LastName:       lastEntry.Text,
				Email:          emailEntry.Text,
				KeyFingerprint: keyFpr,
			}
			reopen := func(msg string) {
				u.showContactEditDialogPrefilled(win, existing, c, msg, done)
			}
			// Optionalen Schlüssel zuerst in den Keyring importieren.
			if pasted := strings.TrimSpace(keyPaste.Text); pasted != "" {
				infos, err := u.keyring.ImportPublic([]byte(pasted))
				switch {
				case err == nil:
					c.KeyFingerprint = pickKeyForEmail(infos, c.Email)
				case strings.Contains(err.Error(), "kein neuer Schlüssel"):
					// Key war schon im Ring: vorhandenen referenzieren.
					if e := u.keyring.FindPublicByEmail(c.Email); e != nil {
						c.KeyFingerprint = pgp.FingerprintOf(e)
					} else {
						reopen(err.Error())
						return
					}
				default:
					reopen(err.Error())
					return
				}
			}
			var err error
			if existing == nil {
				err = u.contacts.Add(c)
			} else {
				err = u.contacts.Update(oldEmail, c)
			}
			if err != nil {
				reopen(contactErr(err).Error())
				return
			}
			done()
		}, win)
	d.Resize(fyne.NewSize(560, 500))
	d.Show()
	win.Canvas().Focus(firstEntry)
}

// pickKeyForEmail wählt aus importierten Keys den zur Kontakt-Mail passenden
// (UID-Match, case-insensitive), sonst den ersten.
func pickKeyForEmail(infos []pgp.KeyInfo, email string) string {
	if len(infos) == 0 {
		return ""
	}
	for _, info := range infos {
		if strings.EqualFold(info.Email, email) {
			return info.Fingerprint
		}
	}
	return infos[0].Fingerprint
}

// contactErr übersetzt die typisierten contacts-Fehler.
func contactErr(err error) error {
	switch {
	case errors.Is(err, contacts.ErrInvalidEmail):
		return errors.New(i18n.T("contacts.err.email"))
	case errors.Is(err, contacts.ErrDuplicateEmail):
		return errors.New(i18n.T("contacts.err.duplicate"))
	case errors.Is(err, contacts.ErrNameRequired):
		return errors.New(i18n.T("contacts.err.name"))
	default:
		return err
	}
}
