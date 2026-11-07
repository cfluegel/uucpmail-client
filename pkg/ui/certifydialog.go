package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/compose"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"github.com/cfluegel/uucpmail-client/pkg/pgp"
)

// showCertifyDialog zertifiziert einen fremden Schlüssel nach persönlicher
// Prüfung (Key Signing Party): Fingerprint groß anzeigen, Identitäten
// wählen, Pflicht-Bestätigung; danach exportierbar zertifizieren und
// caff-artig zurücksenden – oder nur signieren und später über
// showSendCertsDialog verschicken.
func (u *UI) showCertifyDialog(win fyne.Window, info pgp.KeyInfo, done func()) {
	privs := u.keyring.ListPrivate()
	if len(privs) == 0 {
		u.showError(i18n.Errorf("err.wot.noownkey"))
		return
	}
	uids := u.keyring.UIDs(info.Fingerprint)

	intro := widget.NewLabel(i18n.T("wot.certify.intro"))
	intro.Wrapping = fyne.TextWrapWord
	fprLabel := widget.NewLabel(groupFpr(info.Fingerprint))
	fprLabel.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	// Ablaufdatum neben dem Erstellungsdatum; abgelaufen wie im Info-Dialog
	// mit gelbem Warn-Icon markiert.
	expiry := i18n.T("settings.pgp.noexpiry")
	expired := false
	if !info.Expiry.IsZero() {
		expiry = info.Expiry.Format(i18n.T("date.short"))
		expired = time.Now().After(info.Expiry)
	}
	var meta fyne.CanvasObject = widget.NewLabel(fmt.Sprintf("%s · %s %s · %s %s", u.keyring.KeyTypes(info.Fingerprint),
		i18n.T("wot.sheet.created"), info.Created.Format(i18n.T("date.short")),
		i18n.T("wot.info.expires"), expiry))
	if expired {
		meta = container.NewHBox(meta, warnIcon(), widget.NewLabel(i18n.T("wot.info.expired")))
	}

	var okBtn *widget.Button
	checks := make([]*widget.Check, len(uids))
	uidBox := container.NewVBox(widget.NewLabel(i18n.T("wot.certify.uids")))
	for i, uid := range uids {
		text := uid
		if u.keyring.UIDEmail(info.Fingerprint, uid) == "" {
			text += "  " + i18n.T("wot.certify.noemail")
		}
		// OnChanged erst unten setzen: SetChecked löst ihn aus, okBtn
		// existiert hier noch nicht.
		checks[i] = widget.NewCheck(text, nil)
		checks[i].SetChecked(true)
		uidBox.Add(checks[i])
	}

	signerLabels := make([]string, len(privs))
	for i, p := range privs {
		signerLabels[i] = fmt.Sprintf("%s <%s> – %s", p.Name, p.Email, groupFpr(p.Fingerprint[len(p.Fingerprint)-16:]))
	}
	signerSel := widget.NewSelect(signerLabels, nil)
	signerSel.SetSelectedIndex(0)

	modeSend, modeSign := i18n.T("wot.certify.mode.send"), i18n.T("wot.certify.mode.sign")
	mode := widget.NewRadioGroup([]string{modeSend, modeSign}, nil)
	mode.Required = true // eine der beiden Optionen ist Pflicht
	mode.SetSelected(modeSend)

	confirm := widget.NewCheck(i18n.T("wot.certify.confirm"), nil)

	var d dialog.Dialog
	selectedUIDs := func() []string {
		var out []string
		for i, c := range checks {
			if c.Checked {
				out = append(out, uids[i])
			}
		}
		return out
	}
	updateOK := func() {
		if confirm.Checked && len(selectedUIDs()) > 0 {
			okBtn.Enable()
		} else {
			okBtn.Disable()
		}
	}
	okBtn = widget.NewButton(i18n.T("wot.certify.do"), func() {
		d.Hide()
		send := mode.Selected == modeSend
		signer := privs[signerSel.SelectedIndex()]
		chosen := selectedUIDs()
		u.unlockForCertify(win, signer, func(unlocked *pgp.Entity) {
			if err := u.keyring.Certify(info.Fingerprint, chosen, unlocked); err != nil {
				u.showError(err)
				return
			}
			done()
			if !send {
				dialog.ShowInformation(i18n.T("wot.title"), i18n.T("wot.send.skipped"), win)
				return
			}
			u.sendCertifications(win, info, chosen, signer)
		})
	})
	okBtn.Importance = widget.HighImportance
	okBtn.Disable()
	for _, c := range checks {
		c.OnChanged = func(bool) { updateOK() }
	}
	confirm.OnChanged = func(bool) { updateOK() }
	mode.OnChanged = func(string) { updateOK() }
	cancelBtn := widget.NewButton(i18n.T("compose.cancel"), func() { d.Hide() })

	form := container.NewVBox(
		intro,
		widget.NewLabel(fmt.Sprintf("%s <%s>", info.Name, info.Email)),
		fprLabel, meta,
		widget.NewSeparator(),
		uidBox,
		container.NewBorder(nil, nil, widget.NewLabel(i18n.T("wot.certify.signer")), nil, signerSel),
		mode,
		widget.NewSeparator(),
		confirm,
	)
	content := container.NewBorder(nil, container.NewCenter(container.NewHBox(cancelBtn, okBtn)), nil, nil,
		container.NewVScroll(form))
	d = dialog.NewCustomWithoutButtons(i18n.T("wot.certify.title"), content, win)
	d.Resize(fyne.NewSize(640, 560))
	d.Show()
}

// unlockForCertify liefert den entsperrten eigenen Schlüssel (Cache, sonst
// Passphrase-Abfrage). Zertifiziert wird immer mit dem Hauptschlüssel aus
// dem Secring – auch bei Karten-Schlüsseln aus dem Wizard.
func (u *UI) unlockForCertify(win fyne.Window, signer pgp.KeyInfo, next func(*pgp.Entity)) {
	own := u.keyring.FindPrivateByFingerprint(signer.Fingerprint)
	if own == nil {
		u.showError(i18n.Errorf("err.wot.noownkey"))
		return
	}
	if !pgp.NeedsPassphrase(own) {
		next(own)
		return
	}
	if c := u.pgpCache.Get(signer.Fingerprint); c != nil {
		next(c)
		return
	}
	u.promptPassphrase(win, own, signer, "", func(unlocked *pgp.Entity, ok bool) {
		if ok {
			next(unlocked)
		}
	})
}

// sendCertifications schickt die Zertifizierung caff-artig zurück: je
// Identität eine signierte und an den Zielschlüssel verschlüsselte Mail mit
// genau dieser Identität und der eigenen Zertifizierung als Anhang. Nur wer
// Adresse UND Schlüssel kontrolliert, kann sie lesen und übernehmen.
func (u *UI) sendCertifications(win fyne.Window, info pgp.KeyInfo, uids []string, signer pgp.KeyInfo) {
	if !u.keyring.CanEncryptTo(info.Fingerprint) {
		dialog.ShowInformation(i18n.T("wot.send.title"), i18n.T("wot.send.noenc"), win)
		return
	}
	type target struct{ uid, email string }
	var targets []target
	for _, uid := range uids {
		if email := u.keyring.UIDEmail(info.Fingerprint, uid); email != "" {
			targets = append(targets, target{uid, email})
		}
	}
	if len(targets) == 0 {
		dialog.ShowInformation(i18n.T("wot.send.title"), i18n.T("wot.send.noaddr"), win)
		return
	}
	identity := u.certifyIdentity(signer)
	if identity == "" {
		u.showError(i18n.Errorf("err.wot.noidentity"))
		return
	}

	var list strings.Builder
	for _, t := range targets {
		fmt.Fprintf(&list, "  • %s\n", t.email)
	}
	ask := widget.NewLabel(fmt.Sprintf(i18n.T("wot.send.ask"), list.String()))
	ask.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustomConfirm(i18n.T("wot.send.title"), i18n.T("wot.send.do"), i18n.T("wot.send.later"),
		ask, func(ok bool) {
			if !ok {
				dialog.ShowInformation(i18n.T("wot.send.title"), i18n.T("wot.send.skipped"), win)
				return
			}
			// Mail-Signierschlüssel der Absender-Identität entsperren (Datei-
			// Key: Passphrase; Karte: PIN später in der Sende-Goroutine).
			u.ensureUnlockedSigner(win, identity, func(ok bool) {
				if !ok {
					return
				}
				keyID := info.Fingerprint
				if len(keyID) > 16 {
					keyID = keyID[len(keyID)-16:]
				}
				var msgs []*compose.Message
				for _, t := range targets {
					data, err := u.keyring.ExportForUID(info.Fingerprint, t.uid, signer.Fingerprint)
					if err != nil {
						u.showError(err)
						return
					}
					msgs = append(msgs, &compose.Message{
						Identity: identity,
						To:       []string{t.email},
						Subject:  fmt.Sprintf(i18n.T("wot.mail.subject"), "0x"+keyID),
						Body:     fmt.Sprintf(i18n.T("wot.mail.body"), t.uid, groupFpr(info.Fingerprint)),
						NodeName: u.cfg.NodeName,
						Attachments: []compose.Attachment{{
							Filename: "OpenPGP_0x" + keyID + ".asc",
							Data:     data,
							MIMEType: "application/pgp-keys",
						}},
						Sign:    true,
						Encrypt: true,
					})
				}
				// Sealen im Hintergrund: Karten-PIN-Dialog läuft über fyne.Do
				// (sonst Deadlock im UI-Thread, Muster doEnqueue).
				go func() {
					sent := 0
					var firstErr error
					for _, m := range msgs {
						if _, err := u.engine.Enqueue(m); err != nil {
							if firstErr == nil {
								firstErr = err
							}
							continue
						}
						sent++
					}
					fyne.Do(func() {
						u.refresh()
						if firstErr != nil {
							if u.pgpCardKey != nil && strings.Contains(firstErr.Error(), "before Serialize") {
								firstErr = fmt.Errorf("%s", i18n.T("pgpcard.signfail"))
							}
							u.showError(firstErr)
						}
						if sent > 0 {
							u.log.append(fmt.Sprintf(i18n.T("wot.send.done"), sent))
							dialog.ShowInformation(i18n.T("wot.send.title"),
								fmt.Sprintf(i18n.T("wot.send.done"), sent), win)
						}
					})
				}()
			})
		}, win)
	d.Resize(fyne.NewSize(520, 300))
	d.Show()
}

// certifyIdentity wählt die Absenderadresse für die Rücksendung: eine
// konfigurierte Identität, die zum zertifizierenden Schlüssel gehört, sonst
// die Standard-Identität.
func (u *UI) certifyIdentity(signer pgp.KeyInfo) string {
	for _, id := range u.cfg.Identities {
		for _, e := range signer.Emails {
			if strings.EqualFold(id, e) {
				return id
			}
		}
	}
	id, err := u.cfg.Identity("")
	if err != nil {
		return ""
	}
	return id
}

// showSendCertsDialog verschickt vorhandene eigene Zertifizierungen
// (erneut): Auswahl der signierten Identitäten, die primäre vorgewählt;
// danach der gewohnte Ablauf von sendCertifications (Bestätigung mit
// Adressliste, je Identität eine verschlüsselte Mail).
func (u *UI) showSendCertsDialog(win fyne.Window, info pgp.KeyInfo) {
	// Signierte Identitäten je eigenem Schlüssel (gültig, nicht widerrufen).
	bySigner := map[string][]string{}
	for _, c := range u.keyring.Certifications(info.Fingerprint) {
		if c.Mine && c.Status == pgp.CertValid && !c.Revoked && !slices.Contains(bySigner[c.SignerFpr], c.UID) {
			bySigner[c.SignerFpr] = append(bySigner[c.SignerFpr], c.UID)
		}
	}
	var signers []pgp.KeyInfo
	for _, p := range u.keyring.ListPrivate() {
		if len(bySigner[p.Fingerprint]) > 0 {
			signers = append(signers, p)
		}
	}
	if len(signers) == 0 {
		return // Knopf ist dann ohnehin deaktiviert
	}
	primary := u.keyring.PrimaryUID(info.Fingerprint)

	intro := widget.NewLabel(fmt.Sprintf(i18n.T("wot.sendcert.intro"), info.Name, info.Email))
	intro.Wrapping = fyne.TextWrapWord
	uidBox := container.NewVBox()
	var checks []*widget.Check
	var uids []string
	var sendBtn *widget.Button
	update := func() {
		for _, c := range checks {
			if c.Checked {
				sendBtn.Enable()
				return
			}
		}
		sendBtn.Disable()
	}
	signer := signers[0]
	fill := func() {
		uids = bySigner[signer.Fingerprint]
		pre := 0
		if i := slices.Index(uids, primary); i >= 0 {
			pre = i
		}
		uidBox.Objects = nil
		checks = nil
		for i, uid := range uids {
			c := widget.NewCheck(uid, func(bool) { update() })
			c.Checked = i == pre
			checks = append(checks, c)
			uidBox.Add(c)
		}
		uidBox.Refresh()
		update()
	}

	var d dialog.Dialog
	sendBtn = widget.NewButton(i18n.T("wot.send.do"), func() {
		var chosen []string
		for i, c := range checks {
			if c.Checked {
				chosen = append(chosen, uids[i])
			}
		}
		d.Hide()
		u.sendCertifications(win, info, chosen, signer)
	})
	sendBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton(i18n.T("compose.cancel"), func() { d.Hide() })

	form := container.NewVBox(intro)
	if len(signers) > 1 {
		labels := make([]string, len(signers))
		for i, p := range signers {
			labels[i] = fmt.Sprintf("%s <%s> – %s", p.Name, p.Email, groupFpr(p.Fingerprint[len(p.Fingerprint)-16:]))
		}
		sel := widget.NewSelect(labels, nil)
		sel.SetSelectedIndex(0)
		sel.OnChanged = func(string) {
			signer = signers[sel.SelectedIndex()]
			fill()
		}
		form.Add(container.NewBorder(nil, nil, widget.NewLabel(i18n.T("wot.certify.signer")), nil, sel))
	}
	form.Add(widget.NewLabel(i18n.T("wot.sendcert.uids")))
	form.Add(uidBox)
	fill()

	content := container.NewBorder(nil, container.NewCenter(container.NewHBox(cancelBtn, sendBtn)), nil, nil,
		container.NewVScroll(form))
	d = dialog.NewCustomWithoutButtons(i18n.T("wot.sendcert.title"), content, win)
	d.Resize(fyne.NewSize(560, 380))
	d.Show()
}
