package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"github.com/cfluegel/uucpmail-client/pkg/pgp"
	"github.com/cfluegel/uucpmail-client/pkg/pgpcard"
)

// showCardWizard führt durch die Karten-Einrichtung: Karte erkennen →
// Admin-PIN (Werks-PINs erzwingen Änderung) → Schlüsselformular →
// Pflicht-Backup → keytocard (SIG/DEC/AUT) → Übernahme in den Ring + Stub.
// Reihenfolge bewusst: das Backup entsteht VOR dem Kartenschreiben.
func (u *UI) showCardWizard(win fyne.Window, done func()) {
	backend := pgpcard.DefaultBackend()
	go func() {
		cards, err := backend.Cards(context.Background())
		if err != nil {
			fyne.Do(func() { u.showError(err) })
			return
		}
		fyne.Do(func() {
			u.cardPick(win, cards, func(info pgpcard.CardInfo) {
				u.cardWizardAdmin(win, backend, info, done)
			})
		})
	}()
}

// cardWizardAdmin öffnet die Session und verifiziert die Admin-PIN; bei
// Werks-PINs wird die Änderung erzwungen, bevor es weitergeht.
func (u *UI) cardWizardAdmin(win fyne.Window, backend pgpcard.Backend, info pgpcard.CardInfo, done func()) {
	adminEntry := widget.NewPasswordEntry()
	label := widget.NewLabel(i18n.T("pgpcard.admin.label"))
	label.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustomConfirm(i18n.T("pgpcard.title"), i18n.T("settings.save"), i18n.T("compose.cancel"),
		container.NewVBox(label, adminEntry), func(ok bool) {
			if !ok {
				return
			}
			adminPIN := adminEntry.Text
			go func() {
				sess, err := backend.Open(context.Background(), info.Serial)
				if err == nil {
					err = sess.VerifyAdminPIN(adminPIN)
					if err != nil {
						_ = sess.Close()
					}
				}
				fyne.Do(func() {
					if err != nil {
						u.showError(err)
						u.cardWizardAdmin(win, backend, info, done)
						return
					}
					// Werks-PIN-Erkennung: Admin direkt, Nutzer-PIN per
					// Probe-Verify (kostet bei geänderter PIN einen Versuch,
					// der Zähler stellt sich beim nächsten Erfolg zurück).
					adminDefault := adminPIN == pgpcard.DefaultAdminPIN
					userDefault := sess.VerifyPIN(pgpcard.DefaultPIN) == nil
					if adminDefault || userDefault {
						u.cardWizardForcePINs(win, sess, info, adminPIN, adminDefault, userDefault, done)
						return
					}
					u.cardWizardKeyForm(win, sess, info, done)
				})
			}()
		}, win)
	d.Resize(fyne.NewSize(460, 170))
	d.Show()
	win.Canvas().Focus(adminEntry)
}

// cardWizardForcePINs erzwingt neue PINs, solange Werks-PINs aktiv sind.
// Bei frischer Karte (beide Werks-PINs) mit KDF-Fähigkeit wird zusätzlich
// angeboten, KDF einzuschalten – vor dem PIN-Wechsel, damit die neuen PINs
// gleich gehasht gesetzt werden.
func (u *UI) cardWizardForcePINs(win fyne.Window, sess pgpcard.Session, info pgpcard.CardInfo,
	adminPIN string, adminDefault, userDefault bool, done func()) {

	msg := widget.NewLabel(i18n.T("pgpcard.wizard.pins.forced"))
	msg.Wrapping = fyne.TextWrapWord
	pin1 := widget.NewPasswordEntry()
	pin2 := widget.NewPasswordEntry()
	items := []*widget.FormItem{
		widget.NewFormItem(i18n.T("pgpcard.wizard.pin.new"), pin1),
		widget.NewFormItem(i18n.T("pgpcard.wizard.pin.new2"), pin2),
	}
	var adm1, adm2 *widget.Entry
	if adminDefault {
		adm1, adm2 = widget.NewPasswordEntry(), widget.NewPasswordEntry()
		items = append(items,
			widget.NewFormItem(i18n.T("pgpcard.wizard.adminpin.new"), adm1),
			widget.NewFormItem(i18n.T("pgpcard.wizard.adminpin.new2"), adm2))
	}
	form := widget.NewForm(items...)
	box := container.NewVBox(msg, form)
	var kdfCheck *widget.Check
	if adminDefault && userDefault && info.KDFPossible() {
		kdfCheck = widget.NewCheck(i18n.T("pgpcard.wizard.kdf"), nil)
		kdfCheck.SetChecked(true)
		box.Add(kdfCheck)
	}
	d := dialog.NewCustomConfirm(i18n.T("pgpcard.title"), i18n.T("settings.save"), i18n.T("compose.cancel"),
		box, func(ok bool) {
			if !ok {
				_ = sess.Close()
				return
			}
			fail := func(key string) {
				u.showError(fmt.Errorf("%s", i18n.T(key)))
				u.cardWizardForcePINs(win, sess, info, adminPIN, adminDefault, userDefault, done)
			}
			if pin1.Text != pin2.Text || (adminDefault && adm1.Text != adm2.Text) {
				fail("pgpcard.wizard.pin.mismatch")
				return
			}
			if len(pin1.Text) < pgpcard.MinPINLen || (adminDefault && len(adm1.Text) < pgpcard.MinAdminPINLen) {
				fail("pgpcard.wizard.pin.short")
				return
			}
			newAdmin := adminPIN
			if adminDefault {
				newAdmin = adm1.Text
			}
			newPIN := pin1.Text
			withKDF := kdfCheck != nil && kdfCheck.Checked
			go func() {
				var err error
				if withKDF {
					err = sess.SetupKDF(pgpcard.DefaultPIN, pgpcard.DefaultAdminPIN)
				}
				if err == nil {
					err = sess.ChangePIN(pgpcard.DefaultPIN, newPIN)
				}
				if err == nil && adminDefault {
					err = sess.ChangeAdminPIN(pgpcard.DefaultAdminPIN, newAdmin)
				}
				if err == nil {
					err = sess.VerifyAdminPIN(newAdmin)
				}
				fyne.Do(func() {
					if err != nil {
						u.showError(err)
						_ = sess.Close()
						return
					}
					u.cardWizardKeyForm(win, sess, info, done)
				})
			}()
		}, win)
	d.Resize(fyne.NewSize(480, 360))
	d.Show()
}

// cardWizardKeyForm: Name, Identitäten, Master-Laufzeit (Subkeys fest 1 Jahr),
// RSA-Länge (nach Kartenfähigkeit gefiltert), Passphrase (Pflicht).
func (u *UI) cardWizardKeyForm(win fyne.Window, sess pgpcard.Session, info pgpcard.CardInfo, done func()) {
	nameEntry := widget.NewEntry()
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

	allBits := []string{"2048", "3072", "4096"}
	bitsOptions := allBits
	if len(info.RSABits) > 0 {
		bitsOptions = nil
		for _, b := range info.RSABits {
			if b == 2048 || b == 3072 || b == 4096 {
				bitsOptions = append(bitsOptions, fmt.Sprintf("%d", b))
			}
		}
		if len(bitsOptions) == 0 {
			bitsOptions = allBits // Karte meldet nichts Passendes – Import entscheidet
		}
	}
	bitsSelect := widget.NewSelect(bitsOptions, nil)
	// Standard bleibt 3072; nur wenn die Karte 3072 nicht anbietet, die erste
	// gemeldete Länge.
	def := bitsOptions[0]
	for _, b := range bitsOptions {
		if b == "3072" {
			def = "3072"
		}
	}
	bitsSelect.SetSelected(def)

	passEntry := widget.NewPasswordEntry()
	pass2Entry := widget.NewPasswordEntry()
	subHint := widget.NewLabel(i18n.T("pgpcard.wizard.subhint"))
	subHint.Wrapping = fyne.TextWrapWord

	items := []*widget.FormItem{
		widget.NewFormItem(i18n.T("settings.pgp.gen.name"), nameEntry),
		widget.NewFormItem(i18n.T("settings.pgp.gen.email"), idChecks),
		widget.NewFormItem("", extraEntry),
		widget.NewFormItem(i18n.T("settings.pgp.gen.lifetime"), lifetimeSelect),
		widget.NewFormItem(i18n.T("pgpcard.wizard.rsabits"), bitsSelect),
		widget.NewFormItem(i18n.T("settings.pgp.gen.pass"), passEntry),
		widget.NewFormItem(i18n.T("settings.pgp.gen.pass2"), pass2Entry),
	}
	form := widget.NewForm(items...)
	d := dialog.NewCustomConfirm(i18n.T("pgpcard.title"), i18n.T("settings.save"), i18n.T("compose.cancel"),
		container.NewVBox(form, subHint), func(ok bool) {
			if !ok {
				_ = sess.Close()
				return
			}
			retry := func(key string) {
				u.showError(fmt.Errorf("%s", i18n.T(key)))
				u.cardWizardKeyForm(win, sess, info, done)
			}
			if passEntry.Text == "" {
				retry("pgpcard.wizard.passreq")
				return
			}
			if passEntry.Text != pass2Entry.Text {
				retry("settings.pgp.gen.mismatch")
				return
			}
			emails := append([]string{}, idChecks.Selected...)
			if extra := strings.TrimSpace(extraEntry.Text); extra != "" {
				emails = append(emails, extra)
			}
			if len(emails) == 0 {
				retry("settings.pgp.gen.noemail")
				return
			}
			lifetime := lifetimes[1].dur
			if i := lifetimeSelect.SelectedIndex(); i >= 0 && i < len(lifetimes) {
				lifetime = lifetimes[i].dur
			}
			bits := 3072
			switch bitsSelect.Selected {
			case "2048":
				bits = 2048
			case "4096":
				bits = 4096
			}
			u.cardWizardGenerate(win, sess, info, nameEntry.Text, emails, bits, lifetime, passEntry.Text, done)
		}, win)
	d.Resize(fyne.NewSize(520, 480))
	d.Show()
}

// cardWizardGenerate erzeugt den Schlüssel im Hintergrund und geht zum
// Pflicht-Backup über.
func (u *UI) cardWizardGenerate(win fyne.Window, sess pgpcard.Session, info pgpcard.CardInfo,
	name string, emails []string, bits int, lifetime time.Duration, pass string, done func()) {

	prog := progressDialog(win, i18n.T("pgpcard.wizard.generating"))
	go func() {
		ent, err := pgp.GenerateCardEntity(name, emails, bits, lifetime)
		fyne.Do(func() {
			prog.Hide()
			if err != nil {
				u.showError(err)
				_ = sess.Close()
				return
			}
			u.cardWizardBackup(win, sess, info, ent, pass, emails[0], done)
		})
	}()
}

// cardWizardBackup erzwingt den Datei-Export des Master-Schlüssels, BEVOR
// etwas auf die Karte geschrieben wird.
func (u *UI) cardWizardBackup(win fyne.Window, sess pgpcard.Session, info pgpcard.CardInfo,
	ent *pgp.Entity, pass, primaryEmail string, done func()) {

	armored, err := pgp.ExportPrivateArmored(ent, pass)
	if err != nil {
		u.showError(err)
		_ = sess.Close()
		return
	}
	msg := widget.NewLabel(i18n.T("pgpcard.wizard.backup.msg"))
	msg.Wrapping = fyne.TextWrapWord
	var d dialog.Dialog
	saveBtn := widget.NewButton(i18n.T("pgpcard.wizard.backup.btn"), func() {
		fs := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
			if err != nil || wc == nil {
				return // Dialog bleibt offen – Backup ist Pflicht
			}
			_, werr := wc.Write(armored)
			cerr := wc.Close()
			if werr != nil || cerr != nil {
				if werr == nil {
					werr = cerr
				}
				u.showError(werr)
				return
			}
			d.Hide()
			u.cardWizardImport(win, sess, info, ent, pass, done)
		}, win)
		fs.SetFileName("pgp-master-backup-" + primaryEmail + ".asc")
		fs.Show()
	})
	saveBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton(i18n.T("compose.cancel"), func() {
		d.Hide()
		_ = sess.Close()
	})
	d = dialog.NewCustomWithoutButtons(i18n.T("pgpcard.wizard.backup.title"),
		container.NewVBox(msg, container.NewCenter(container.NewHBox(cancelBtn, saveBtn))), win)
	d.Resize(fyne.NewSize(540, 260))
	d.Show()
}

// cardWizardImport überträgt die drei Subkeys auf die Karte (keytocard) und
// schließt ab: Ring + Stub + Config.
func (u *UI) cardWizardImport(win fyne.Window, sess pgpcard.Session, info pgpcard.CardInfo,
	ent *pgp.Entity, pass string, done func()) {

	prog := progressDialog(win, i18n.T("pgpcard.wizard.importing"))
	go func() {
		err := importSubkeysToCard(sess, ent)
		var stub *pgpcard.Stub
		if err == nil {
			stub = stubForEntity(ent, info.Serial)
			var stubPath string
			if stubPath, err = config.PGPCardStubPath(); err == nil {
				err = stub.Save(stubPath)
			}
		}
		if err == nil {
			_, err = u.keyring.AdoptEntity(ent, pass)
		}
		_ = sess.Close()
		fyne.Do(func() {
			prog.Hide()
			if err != nil {
				u.showError(err)
				return
			}
			u.cfg.PGPCard = true
			u.saveConfig()
			u.installCardSource()
			hint := widget.NewLabel(i18n.T("pgpcard.wizard.done"))
			hint.Wrapping = fyne.TextWrapWord
			dd := dialog.NewCustom(i18n.T("pgpcard.title"), i18n.T("contacts.close"), hint, win)
			dd.Resize(fyne.NewSize(560, 280))
			dd.Show()
			done()
		})
	}()
}

// importSubkeysToCard schreibt Sign/Encrypt/Auth in die Slots SIG/DEC/AUT.
func importSubkeysToCard(sess pgpcard.Session, ent *pgp.Entity) error {
	for slot, sk := range cardSlotSubkeys(ent) {
		if sk == nil {
			continue
		}
		if err := sess.ImportKey(slot, sk.PrivateKey.PrivateKey, sk.PublicKey.CreationTime); err != nil {
			return err
		}
	}
	return nil
}

// cardSlotSubkeys ordnet die Subkeys der Entity den Karten-Slots zu.
func cardSlotSubkeys(ent *pgp.Entity) map[pgpcard.Slot]*pgp.Subkey {
	out := map[pgpcard.Slot]*pgp.Subkey{}
	for i := range ent.Subkeys {
		sk := &ent.Subkeys[i]
		switch {
		case sk.Sig.FlagSign:
			out[pgpcard.SlotSign] = sk
		case sk.Sig.FlagEncryptCommunications || sk.Sig.FlagEncryptStorage:
			out[pgpcard.SlotDecrypt] = sk
		case sk.Sig.FlagAuthenticate:
			out[pgpcard.SlotAuth] = sk
		}
	}
	return out
}

// stubForEntity baut den Stub aus Entity + Karten-Serial.
func stubForEntity(ent *pgp.Entity, serial string) *pgpcard.Stub {
	slots := cardSlotSubkeys(ent)
	algo := pgpcard.AlgoRSA3072
	if sk := slots[pgpcard.SlotSign]; sk != nil {
		if bl, err := sk.PublicKey.BitLength(); err == nil {
			switch {
			case bl >= 4096:
				algo = pgpcard.AlgoRSA4096
			case bl <= 2048:
				algo = pgpcard.AlgoRSA2048
			}
		}
	}
	st := &pgpcard.Stub{
		Version:        1,
		Serial:         serial,
		KeyFingerprint: pgpcard.FingerprintHex(ent.PrimaryKey.Fingerprint),
		Algo:           algo,
		Created:        ent.PrimaryKey.CreationTime,
	}
	if sk := slots[pgpcard.SlotSign]; sk != nil {
		st.SignSubkey = pgpcard.FingerprintHex(sk.PublicKey.Fingerprint)
	}
	if sk := slots[pgpcard.SlotDecrypt]; sk != nil {
		st.DecryptSubkey = pgpcard.FingerprintHex(sk.PublicKey.Fingerprint)
	}
	if sk := slots[pgpcard.SlotAuth]; sk != nil {
		st.AuthSubkey = pgpcard.FingerprintHex(sk.PublicKey.Fingerprint)
	}
	return st
}

// progressDialog zeigt einen modalen Wartehinweis ohne Knöpfe.
func progressDialog(win fyne.Window, text string) dialog.Dialog {
	msg := widget.NewLabel(text)
	msg.Alignment = fyne.TextAlignCenter
	msg.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustomWithoutButtons(i18n.T("pgpcard.title"),
		container.NewVBox(msg, widget.NewProgressBarInfinite()), win)
	d.Resize(fyne.NewSize(380, 140))
	d.Show()
	return d
}
