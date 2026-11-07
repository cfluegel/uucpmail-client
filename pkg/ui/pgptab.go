package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"github.com/cfluegel/uucpmail-client/pkg/pgp"
	"github.com/cfluegel/uucpmail-client/pkg/pgpcard"
)

// buildPGPTab baut den Einstellungs-Tab für die OpenPGP-Schlüsselverwaltung.
// ttlEntry und ksEntry wertet der Speichern-Handler des Settings-Dialogs aus.
func (u *UI) buildPGPTab(win fyne.Window, ttlEntry, ksEntry *widget.Entry) fyne.CanvasObject {
	ownBox := container.NewVBox()

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
				expiry = fmt.Sprintf(i18n.T("settings.pgp.expiry"), info.Expiry.Format(i18n.T("date.short")))
				if time.Now().After(info.Expiry) {
					expiry = "⚠ " + expiry
				}
			}
			label := widget.NewLabel(fmt.Sprintf("%s <%s>\n%s\n%s",
				info.Name, strings.Join(info.Emails, ", "), expiry, groupFpr(info.Fingerprint)))
			label.Wrapping = fyne.TextWrapWord
			exportBtn := widget.NewButtonWithIcon(i18n.T("settings.pgp.export.short"), theme.UploadIcon(), func() {
				u.exportPublicKey(win, info)
			})
			revokeBtn := widget.NewButtonWithIcon(i18n.T("settings.pgp.revoke.btn"), theme.CancelIcon(), func() {
				u.showRevokeDialog(win, info)
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
				container.NewHBox(exportBtn, revokeBtn, delBtn), label))
		}
		genBtn := widget.NewButton(i18n.T("settings.pgp.gen"), func() {
			u.showKeygenDialog(win, render)
		})
		importBtn := widget.NewButton(i18n.T("settings.pgp.import.priv"), func() {
			u.importKeyFile(win, true, render)
		})
		ownBox.Add(container.NewHBox(genBtn, importBtn))
	}

	ownTitle := widget.NewLabel(i18n.T("settings.pgp.own"))
	ownTitle.TextStyle.Bold = true
	// Öffentliche Schlüssel anderer werden im Web-of-Trust-Fenster verwaltet
	// (Nutzerwunsch 2026-11-05) – hier nur der Hinweis dorthin.
	othersHint := widget.NewLabel(i18n.T("settings.pgp.others.moved"))
	othersHint.Wrapping = fyne.TextWrapWord
	cardTitle := widget.NewLabel(i18n.T("pgpcard.section"))
	cardTitle.TextStyle.Bold = true
	cardBox := container.NewVBox()
	ttlRow := container.NewBorder(nil, nil, widget.NewLabel(i18n.T("settings.pgp.cachettl")), nil, ttlEntry)
	ksRow := container.NewBorder(nil, nil, widget.NewLabel(i18n.T("settings.pgp.keyserver")), nil, ksEntry)

	// Der gesamte Tab wird bei Änderungen neu befüllt – Teil-Refreshs in
	// geschachtelten VBoxen führten zu überlappenden Trennlinien.
	root := container.NewVBox()
	render = func() {
		renderOwn()
		u.updateWoTAction()
		u.renderCardSection(win, cardBox, render)
		// Reihenfolge (Nutzerwunsch 2026-11-04): eigener Schlüssel, Smartcard,
		// Passphrase-Cache; fremde Schlüssel → Web-of-Trust-Fenster.
		root.Objects = []fyne.CanvasObject{
			ownTitle, ownBox,
			widget.NewSeparator(),
			cardTitle, cardBox,
			widget.NewSeparator(),
			ttlRow,
			ksRow,
			widget.NewSeparator(),
			othersHint,
		}
		root.Refresh()
	}
	render()
	return container.NewVScroll(root)
}

// renderCardSection befüllt den Smartcard-Abschnitt des PGP-Tabs: Status
// (asynchron über PC/SC), Einrichtung/Verknüpfung, Nutzung-Schalter und –
// bei eigenem Master im Secring – die Subkey-Verlängerung. Der Schalter
// wirkt wie die Schlüssel-Aktionen im SSH-Tab sofort (nicht erst bei Save).
func (u *UI) renderCardSection(win fyne.Window, box *fyne.Container, render func()) {
	box.Objects = nil
	status := widget.NewLabel(i18n.T("pgpcard.status.checking"))
	status.Wrapping = fyne.TextWrapWord
	var stub *pgpcard.Stub
	if path, err := config.PGPCardStubPath(); err == nil {
		stub, _ = pgpcard.LoadStub(path)
	}

	useCheck := widget.NewCheck(i18n.T("pgpcard.use"), func(on bool) {
		u.cfg.PGPCard = on
		u.saveConfig()
		u.installCardSource()
	})
	useCheck.SetChecked(u.cfg.PGPCard && stub != nil)
	if stub == nil {
		useCheck.Disable()
	}

	setupBtn := widget.NewButton(i18n.T("pgpcard.setup.btn"), func() {
		u.showCardWizard(win, render)
	})
	adoptBtn := widget.NewButton(i18n.T("pgpcard.adopt.btn"), func() {
		u.showCardAdoptDialog(win, render)
	})
	detailBtn := widget.NewButton(i18n.T("pgpcard.detail.btn"), func() {
		u.showCardDetails(win, render)
	})
	recheckBtn := widget.NewButton(i18n.T("pgpcard.recheck"), func() {
		render()
	})
	// Kartengebundene Aktionen erst freischalten, wenn der Scan unten eine
	// einsatzbereite Karte meldet (sichtbar, aber vorerst deaktiviert –
	// sonst führt ein Klick ohne Karte ins Leere). „Kartendetails“ und
	// „Erneut prüfen“ bleiben immer nutzbar.
	adoptBtn.Disable()
	buttons := container.NewHBox(setupBtn, adoptBtn, detailBtn, recheckBtn)
	var extendBtn *widget.Button
	if stub != nil && u.keyring.FindPrivateByFingerprint(stub.KeyFingerprint) != nil {
		st := stub
		extendBtn = widget.NewButton(i18n.T("pgpcard.extend.btn"), func() {
			u.showCardExtendDialog(win, st, render)
		})
		extendBtn.Disable()
		buttons.Add(extendBtn)
	}

	go func() {
		cards, err := pgpcard.DefaultBackend().Cards(context.Background())
		fyne.Do(func() {
			switch {
			case errors.Is(err, pgpcard.ErrCardBusy):
				// Karte von einem anderen Programm (gpg/scdaemon) gehalten:
				// klaren Hinweistext statt Roh-Fehler zeigen.
				status.SetText(i18n.T("err.pgpcard.busy"))
			case err != nil:
				status.SetText(err.Error())
			case len(cards) > 0:
				var b strings.Builder
				for i, c := range cards {
					if i > 0 {
						b.WriteString("\n")
					}
					fmt.Fprintf(&b, i18n.T("pgpcard.status.card"), c.Serial, c.Manufacturer)
					anySlot := false
					for _, slot := range []pgpcard.Slot{pgpcard.SlotSign, pgpcard.SlotDecrypt, pgpcard.SlotAuth} {
						si := c.Slots[slot]
						if si.Present {
							anySlot = true
							fmt.Fprintf(&b, "\n  %s: %s %s", strings.ToUpper(string(slot)[:3]), si.Algo, groupFpr(si.Fingerprint[len(si.Fingerprint)-16:]))
						}
					}
					// Leere Karte: kenntlich machen, dass sie erkannt wurde,
					// aber noch eingerichtet werden muss (sonst wirkt die Zeile
					// abgeschnitten).
					if !anySlot {
						b.WriteString("\n" + i18n.T("pgpcard.status.nokeys"))
					}
				}
				status.SetText(b.String())
			default:
				status.SetText(i18n.T("pgpcard.status.none"))
			}
			// Einsatzbereit heißt: eine Karte mit tatsächlich belegtem
			// Signatur-Slot. Eine frische, nicht eingerichtete Karte wird
			// zwar erkannt, taugt aber weder zum Übernehmen noch zum
			// Verlängern – dann bleiben die Knöpfe deaktiviert.
			// „Subkeys verlängern" zusätzlich nur, wenn die zum Stub
			// gehörende Karte (gleiche Serial) steckt.
			if err == nil {
				hasSignKey := false
				stubCardReady := false
				for _, c := range cards {
					if !c.Slots[pgpcard.SlotSign].Present {
						continue
					}
					hasSignKey = true
					if stub != nil && c.Serial == stub.Serial {
						stubCardReady = true
					}
				}
				if hasSignKey {
					adoptBtn.Enable()
				}
				if extendBtn != nil && stubCardReady {
					extendBtn.Enable()
				}
			}
		})
	}()

	box.Add(status)
	box.Add(useCheck)
	box.Add(buttons)
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
	var d dialog.Dialog
	// retryPass meldet den Passphrase-Fehler und öffnet den Dialog danach
	// erneut: Passphrase-Felder geleert, alle anderen Eingaben bleiben.
	retryPass := func(key string) {
		e := dialog.NewError(fmt.Errorf("%s", i18n.T(key)), win)
		e.SetOnClosed(func() {
			passEntry.SetText("")
			pass2Entry.SetText("")
			d.Show()
			win.Canvas().Focus(passEntry)
		})
		e.Show()
	}
	d = dialog.NewForm(i18n.T("settings.pgp.gen"), i18n.T("settings.save"), i18n.T("compose.cancel"),
		items, func(ok bool) {
			if !ok {
				return
			}
			if passEntry.Text == "" {
				retryPass("settings.pgp.gen.nopass")
				return
			}
			if passEntry.Text != pass2Entry.Text {
				retryPass("settings.pgp.gen.mismatch")
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
		if !private {
			// Öffentlich: Merge-Import (übernimmt auch neue Zertifizierungen).
			if _, ok := u.importPublicData(win, data); ok {
				done()
			}
			return
		}
		if _, err = u.keyring.ImportPrivate(data); err != nil {
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
			if _, ok := u.importPublicData(win, []byte(entry.Text)); ok {
				done()
			}
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
	showArmorDialog(win, i18n.T("settings.pgp.export"), nil, armored, info.Email+".asc")
}

// showArmorDialog zeigt einen Armor-Block mit Kopieren und Speichern
// (Dateiname fileName); top ist ein optionaler Hinweis über dem Text.
func showArmorDialog(win fyne.Window, title string, top fyne.CanvasObject, armored []byte, fileName string) {
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
		fs.SetFileName(fileName)
		fs.Show()
	})
	content := container.NewBorder(top, container.NewHBox(copyBtn, saveBtn), nil, nil,
		container.NewVScroll(entry))
	d := dialog.NewCustom(title, i18n.T("compose.cancel"), content, win)
	d.Resize(fyne.NewSize(560, 420))
	d.Show()
}

// groupFpr formatiert den Fingerprint in 4er-Gruppen.
func groupFpr(fpr string) string {
	var out strings.Builder
	for i, r := range fpr {
		if i > 0 && i%4 == 0 {
			out.WriteByte(' ')
		}
		out.WriteRune(r)
	}
	return out.String()
}

// pgpCacheTTL liest die konfigurierte Cache-Dauer.
func pgpCacheTTL(minutes int) time.Duration {
	return time.Duration(minutes) * time.Minute
}
