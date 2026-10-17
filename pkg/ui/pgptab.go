package ui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"github.com/cfluegel/uucpmail-client/pkg/pgp"
)

// buildPGPTab baut den Einstellungs-Tab für die OpenPGP-Schlüsselverwaltung.
// ttlEntry wird vom Speichern-Handler des Settings-Dialogs ausgewertet.
func (u *UI) buildPGPTab(win fyne.Window, ttlEntry *widget.Entry) fyne.CanvasObject {
	ownBox := container.NewVBox()
	othersBox := container.NewVBox()

	var render func()

	// --- Eigener Schlüssel ---
	renderOwn := func() {
		ownBox.Objects = nil
		privs := u.keyring.ListPrivate()
		if len(privs) == 0 {
			ownBox.Add(widget.NewLabel(i18n.T("settings.pgp.none")))
		}
		for _, info := range privs {
			info := info
			expiry := i18n.T("settings.pgp.noexpiry")
			if !info.Expiry.IsZero() {
				expiry = fmt.Sprintf(i18n.T("settings.pgp.expiry"), info.Expiry.Format("02.01.2006"))
				if time.Now().After(info.Expiry) {
					expiry = "⚠ " + expiry
				}
			}
			label := widget.NewLabel(fmt.Sprintf("%s <%s>\n%s\n%s",
				info.Name, strings.Join(info.Emails, ", "), expiry, groupFpr(info.Fingerprint)))
			label.Wrapping = fyne.TextWrapWord
			exportBtn := widget.NewButton(i18n.T("settings.pgp.export"), func() {
				u.exportPublicKey(win, info)
			})
			delBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
				dialog.ShowConfirm(i18n.T("settings.pgp.delete"),
					i18n.T("settings.pgp.delpriv.confirm"), func(ok bool) {
						if !ok {
							return
						}
						if err := u.keyring.DeletePrivate(info.Fingerprint); err != nil {
							u.showError(err)
							return
						}
						u.pgpCache.Remove(info.Fingerprint)
						render()
					}, win)
			})
			delBtn.Importance = widget.DangerImportance
			ownBox.Add(container.NewBorder(nil, nil, nil,
				container.NewHBox(exportBtn, delBtn), label))
		}
		genBtn := widget.NewButton(i18n.T("settings.pgp.gen"), func() {
			u.showKeygenDialog(win, render)
		})
		importBtn := widget.NewButton(i18n.T("settings.pgp.import.priv"), func() {
			u.importKeyFile(win, true, render)
		})
		ownBox.Add(container.NewHBox(genBtn, importBtn))
	}

	// --- Fremde öffentliche Schlüssel ---
	renderOthers := func() {
		othersBox.Objects = nil
		for _, info := range u.keyring.ListPublic() {
			info := info
			label := widget.NewLabel(fmt.Sprintf("%s <%s>\n%s", info.Name, info.Email, groupFpr(info.Fingerprint)))
			label.Wrapping = fyne.TextWrapWord
			delBtn := widget.NewButton(i18n.T("settings.pgp.delete"), func() {
				dialog.ShowConfirm(i18n.T("settings.pgp.delete"), i18n.T("settings.pgp.delete.confirm"),
					func(ok bool) {
						if !ok {
							return
						}
						if err := u.keyring.DeletePublic(info.Fingerprint); err != nil {
							u.showError(err)
							return
						}
						render()
					}, win)
			})
			othersBox.Add(container.NewBorder(nil, nil, nil, delBtn, label))
		}
		importBtn := widget.NewButton(i18n.T("settings.pgp.import.pub"), func() {
			u.importPubKeyDialog(win, render)
		})
		othersBox.Add(importBtn)
	}

	ownTitle := widget.NewLabel(i18n.T("settings.pgp.own"))
	ownTitle.TextStyle.Bold = true
	othersTitle := widget.NewLabel(i18n.T("settings.pgp.others"))
	othersTitle.TextStyle.Bold = true
	ttlRow := container.NewBorder(nil, nil, widget.NewLabel(i18n.T("settings.pgp.cachettl")), nil, ttlEntry)

	// Der gesamte Tab wird bei Änderungen neu befüllt – Teil-Refreshs in
	// geschachtelten VBoxen führten zu überlappenden Trennlinien.
	root := container.NewVBox()
	render = func() {
		renderOwn()
		renderOthers()
		root.Objects = []fyne.CanvasObject{
			ownTitle, ownBox,
			widget.NewSeparator(),
			othersTitle, othersBox,
			widget.NewSeparator(),
			ttlRow,
		}
		root.Refresh()
	}
	render()
	return container.NewVScroll(root)
}

// showKeygenDialog fragt Name, Identitäten (ein Schlüssel kann mehrere UIDs
// tragen), Gültigkeit und Passphrase ab und erzeugt den Schlüssel.
func (u *UI) showKeygenDialog(win fyne.Window, done func()) {
	nameEntry := widget.NewEntry()
	// Identitäten als Mehrfachauswahl; die Standard-Identität ist vorgewählt.
	idChecks := widget.NewCheckGroup(u.cfg.Identities, nil)
	if def, err := u.cfg.Identity(""); err == nil {
		idChecks.SetSelected([]string{def})
	}
	extraEntry := widget.NewEntry()
	extraEntry.SetPlaceHolder(i18n.T("settings.pgp.gen.extra"))

	lifetimes := []struct {
		label string
		dur   time.Duration
	}{
		{"1 " + i18n.T("settings.pgp.gen.year"), 365 * 24 * time.Hour},
		{"2 " + i18n.T("settings.pgp.gen.years"), 2 * 365 * 24 * time.Hour},
		{"3 " + i18n.T("settings.pgp.gen.years"), 3 * 365 * 24 * time.Hour},
		{"5 " + i18n.T("settings.pgp.gen.years"), 5 * 365 * 24 * time.Hour},
		{i18n.T("settings.pgp.gen.nolimit"), 0},
	}
	var lifetimeLabels []string
	for _, l := range lifetimes {
		lifetimeLabels = append(lifetimeLabels, l.label)
	}
	lifetimeSelect := widget.NewSelect(lifetimeLabels, nil)
	lifetimeSelect.SetSelectedIndex(1) // Standard: 2 Jahre

	passEntry := widget.NewPasswordEntry()
	pass2Entry := widget.NewPasswordEntry()

	items := []*widget.FormItem{
		widget.NewFormItem(i18n.T("settings.pgp.gen.name"), nameEntry),
		widget.NewFormItem(i18n.T("settings.pgp.gen.email"), idChecks),
		widget.NewFormItem("", extraEntry),
		widget.NewFormItem(i18n.T("settings.pgp.gen.lifetime"), lifetimeSelect),
		widget.NewFormItem(i18n.T("settings.pgp.gen.pass"), passEntry),
		widget.NewFormItem(i18n.T("settings.pgp.gen.pass2"), pass2Entry),
	}
	d := dialog.NewForm(i18n.T("settings.pgp.gen"), i18n.T("settings.save"), i18n.T("compose.cancel"),
		items, func(ok bool) {
			if !ok {
				return
			}
			if passEntry.Text != pass2Entry.Text {
				dialog.ShowError(fmt.Errorf("%s", i18n.T("settings.pgp.gen.mismatch")), win)
				return
			}
			emails := append([]string{}, idChecks.Selected...)
			if extra := strings.TrimSpace(extraEntry.Text); extra != "" {
				emails = append(emails, extra)
			}
			if len(emails) == 0 {
				dialog.ShowError(fmt.Errorf("%s", i18n.T("settings.pgp.gen.noemail")), win)
				return
			}
			lifetime := lifetimes[1].dur
			if i := lifetimeSelect.SelectedIndex(); i >= 0 && i < len(lifetimes) {
				lifetime = lifetimes[i].dur
			}
			name, pass := nameEntry.Text, passEntry.Text
			go func() {
				_, err := u.keyring.Generate(name, emails, pass, lifetime)
				fyne.Do(func() {
					if err != nil {
						u.showError(err)
						return
					}
					done()
				})
			}()
		}, win)
	d.Resize(fyne.NewSize(500, 420))
	d.Show()
}

// importKeyFile importiert eine .asc-Datei (privat oder öffentlich).
func (u *UI) importKeyFile(win fyne.Window, private bool, done func()) {
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
		if private {
			_, err = u.keyring.ImportPrivate(data)
		} else {
			_, err = u.keyring.ImportPublic(data)
		}
		if err != nil {
			u.showError(err)
			return
		}
		done()
	}, win)
	fd.Show()
}

// importPubKeyDialog importiert öffentliche Schlüssel aus einem Textfeld
// (Armor einfügen) – spiegelbildlich zum Export; alternativ aus Datei.
func (u *UI) importPubKeyDialog(win fyne.Window, done func()) {
	entry := widget.NewMultiLineEntry()
	entry.SetPlaceHolder(i18n.T("settings.pgp.paste"))
	entry.Wrapping = fyne.TextWrapBreak
	fileBtn := widget.NewButton(i18n.T("settings.pgp.fromfile"), nil)

	content := container.NewBorder(nil, fileBtn, nil, nil, container.NewVScroll(entry))
	d := dialog.NewCustomConfirm(i18n.T("settings.pgp.import.pub"),
		i18n.T("settings.pgp.import.do"), i18n.T("compose.cancel"), content,
		func(ok bool) {
			if !ok {
				return
			}
			if _, err := u.keyring.ImportPublic([]byte(entry.Text)); err != nil {
				u.showError(err)
				return
			}
			done()
		}, win)
	fileBtn.OnTapped = func() {
		d.Hide()
		u.importKeyFile(win, false, done)
	}
	d.Resize(fyne.NewSize(560, 420))
	d.Show()
	win.Canvas().Focus(entry)
}

// exportPublicKey zeigt den Armor-Export mit Copy- und Speichern-Option.
func (u *UI) exportPublicKey(win fyne.Window, info pgp.KeyInfo) {
	armored, err := u.keyring.ExportPublic(info.Fingerprint)
	if err != nil {
		u.showError(err)
		return
	}
	entry := widget.NewMultiLineEntry()
	entry.SetText(string(armored))
	entry.Wrapping = fyne.TextWrapBreak
	copyBtn := widget.NewButton(i18n.T("settings.pgp.copy"), func() {
		win.Clipboard().SetContent(string(armored))
	})
	saveBtn := widget.NewButton(i18n.T("compose.save"), func() {
		fs := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
			if err != nil || wc == nil {
				return
			}
			defer wc.Close()
			_, _ = wc.Write(armored)
		}, win)
		fs.SetFileName(info.Email + ".asc")
		fs.Show()
	})
	content := container.NewBorder(nil, container.NewHBox(copyBtn, saveBtn), nil, nil,
		container.NewVScroll(entry))
	d := dialog.NewCustom(i18n.T("settings.pgp.export"), i18n.T("compose.cancel"), content, win)
	d.Resize(fyne.NewSize(560, 420))
	d.Show()
}

// groupFpr formatiert den Fingerprint in 4er-Gruppen.
func groupFpr(fpr string) string {
	var out []byte
	for i, r := range fpr {
		if i > 0 && i%4 == 0 {
			out = append(out, ' ')
		}
		out = append(out, byte(r))
	}
	return string(out)
}

// pgpCacheTTL liest die konfigurierte Cache-Dauer.
func pgpCacheTTL(minutes int) time.Duration {
	return time.Duration(minutes) * time.Minute
}
