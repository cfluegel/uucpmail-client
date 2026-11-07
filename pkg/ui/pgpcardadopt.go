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

// showCardAdoptDialog verknüpft eine extern (z.B. mit GnuPG) eingerichtete
// Karte: Slots gegen den Pubring matchen, Stub anlegen, Modus aktivieren.
// RSA-Karten voll; Ed25519-Karten nur Signieren (X25519-Decrypt fehlt in der
// released go-crypto-Hardwareschnittstelle).
func (u *UI) showCardAdoptDialog(win fyne.Window, done func()) {
	backend := pgpcard.DefaultBackend()
	go func() {
		cards, err := backend.Cards(context.Background())
		fyne.Do(func() {
			if err != nil {
				u.showError(err)
				return
			}
			u.cardPick(win, cards, func(info pgpcard.CardInfo) {
				u.cardAdopt(win, info, done)
			})
		})
	}()
}

// cardPick lässt bei mehreren Karten wählen (eine Karte: direkt weiter).
func (u *UI) cardPick(win fyne.Window, cards []pgpcard.CardInfo, next func(pgpcard.CardInfo)) {
	if len(cards) == 1 {
		next(cards[0])
		return
	}
	labels := make([]string, len(cards))
	for i, c := range cards {
		labels[i] = fmt.Sprintf(i18n.T("pgpcard.status.card"), c.Serial, c.Manufacturer)
	}
	sel := widget.NewRadioGroup(labels, nil)
	sel.SetSelected(labels[0])
	d := dialog.NewCustomConfirm(i18n.T("pgpcard.title"), i18n.T("fido2.use"), i18n.T("compose.cancel"),
		sel, func(ok bool) {
			if !ok {
				return
			}
			for i, l := range labels {
				if l == sel.Selected {
					next(cards[i])
					return
				}
			}
		}, win)
	d.Show()
}

func (u *UI) cardAdopt(win fyne.Window, info pgpcard.CardInfo, done func()) {
	sig := info.Slots[pgpcard.SlotSign]
	if !sig.Present {
		// Entweder wirklich leer ODER die Karte meldet ihre Slot-Belegung
		// nicht (ältere Karten vor Spec 3.4, z.B. ZeitControl v2.x). In beiden
		// Fällen kann „vorhandene Karte verwenden" den Schlüssel nicht
		// zuordnen; der Einrichtungs-Wizard ist dann der Weg.
		msg := widget.NewLabel(i18n.T("pgpcard.adopt.noslot"))
		msg.Wrapping = fyne.TextWrapWord
		d := dialog.NewCustom(i18n.T("pgpcard.title"), i18n.T("contacts.close"), msg, win)
		d.Resize(fyne.NewSize(520, 220))
		d.Show()
		return
	}
	var algo string
	switch {
	case sig.Algo == "rsa4096":
		algo = pgpcard.AlgoRSA4096
	case sig.Algo == "rsa2048":
		algo = pgpcard.AlgoRSA2048
	case strings.HasPrefix(sig.Algo, "rsa"):
		algo = pgpcard.AlgoRSA3072
	case sig.Algo == "ed25519":
		algo = pgpcard.AlgoEd25519Sign
	default:
		u.showError(i18n.Errorf("err.pgpcard.notsupported"))
		return
	}

	ent := u.findPublicBySubkeyFpr(sig.Fingerprint)
	if ent == nil {
		msg := widget.NewLabel(fmt.Sprintf(i18n.T("pgpcard.adopt.nopub"), groupFpr(sig.Fingerprint)))
		msg.Wrapping = fyne.TextWrapWord
		d := dialog.NewCustom(i18n.T("pgpcard.title"), i18n.T("contacts.close"), msg, win)
		d.Resize(fyne.NewSize(520, 220))
		d.Show()
		return
	}

	st := &pgpcard.Stub{
		Version:        1,
		Serial:         info.Serial,
		KeyFingerprint: pgpcard.FingerprintHex(ent.PrimaryKey.Fingerprint),
		SignSubkey:     sig.Fingerprint,
		Algo:           algo,
		Created:        time.Now(),
	}
	// DEC/AUT nur übernehmen, wenn der Slot-Key zum selben Schlüssel gehört.
	if algo != pgpcard.AlgoEd25519Sign {
		if dec := info.Slots[pgpcard.SlotDecrypt]; dec.Present && entityHasSubkey(ent, dec.Fingerprint) {
			st.DecryptSubkey = dec.Fingerprint
		}
	}
	if aut := info.Slots[pgpcard.SlotAuth]; aut.Present && entityHasSubkey(ent, aut.Fingerprint) {
		st.AuthSubkey = aut.Fingerprint
	}

	stubPath, err := config.PGPCardStubPath()
	if err == nil {
		err = st.Save(stubPath)
	}
	if err != nil {
		u.showError(err)
		return
	}
	u.cfg.PGPCard = true
	u.saveConfig()
	u.installCardSource()

	text := i18n.T("pgpcard.adopt.done")
	if algo == pgpcard.AlgoEd25519Sign {
		text += "\n\n" + i18n.T("pgpcard.adopt.signonly")
	}
	msg := widget.NewLabel(text)
	msg.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustom(i18n.T("pgpcard.title"), i18n.T("contacts.close"), msg, win)
	d.Resize(fyne.NewSize(520, 220))
	d.Show()
	done()
}

// findPublicBySubkeyFpr sucht die Entity, die den Subkey-Fingerprint trägt.
func (u *UI) findPublicBySubkeyFpr(fpr string) *pgp.Entity {
	for _, e := range u.keyring.PublicEntities() {
		if entityHasSubkey(e, fpr) {
			return e
		}
	}
	return nil
}

func entityHasSubkey(e *pgp.Entity, fpr string) bool {
	for i := range e.Subkeys {
		if pgpcard.FingerprintHex(e.Subkeys[i].PublicKey.Fingerprint) == fpr {
			return true
		}
	}
	return false
}

// showCardDetails zeigt die von der Karte gemeldeten Angaben (Serial,
// Hersteller, Reader, unterstützte RSA-Längen, Slot-Belegung mit Algorithmus,
// Fingerprint und Erzeugungszeit). Nutzt nur öffentliche Datenobjekte – keine
// PIN nötig. Ältere Karten (vor Spec 3.4) melden Fingerprints/Slot-Status oft
// nicht; das wird als Hinweis angezeigt.
func (u *UI) showCardDetails(win fyne.Window, done func()) {
	go func() {
		cards, err := pgpcard.DefaultBackend().Cards(context.Background())
		fyne.Do(func() {
			if err != nil {
				u.showError(err)
				return
			}
			var b strings.Builder
			for i, c := range cards {
				if i > 0 {
					b.WriteString("\n\n")
				}
				fmt.Fprintf(&b, "%s: %s\n%s: %s\n%s: %s\n",
					i18n.T("pgpcard.detail.serial"), c.Serial,
					i18n.T("pgpcard.detail.maker"), c.Manufacturer,
					i18n.T("pgpcard.detail.reader"), c.Reader)
				if c.Version != "" {
					fmt.Fprintf(&b, "%s: %s\n", i18n.T("pgpcard.detail.version"), c.Version)
				}
				if len(c.RSABits) > 0 {
					fmt.Fprintf(&b, "%s: %v\n", i18n.T("pgpcard.detail.rsabits"), c.RSABits)
				}
				if len(c.Algos) > 0 {
					fmt.Fprintf(&b, "%s: %s\n", i18n.T("pgpcard.detail.algos"), strings.Join(c.Algos, ", "))
				}
				fmt.Fprintf(&b, "%s: %d / %d\n%s: %s\n%s: %s\n%s: %d\n",
					i18n.T("pgpcard.detail.retries"), c.PINRetries, c.AdminRetries,
					i18n.T("pgpcard.detail.sigpin"), sigPINText(c),
					i18n.T("pgpcard.detail.kdf"), kdfText(c),
					i18n.T("pgpcard.detail.sigcount"), c.SigCount)
				anyFpr := false
				for _, slot := range []pgpcard.Slot{pgpcard.SlotSign, pgpcard.SlotDecrypt, pgpcard.SlotAuth} {
					si := c.Slots[slot]
					name := strings.ToUpper(string(slot))
					if !si.Present {
						fmt.Fprintf(&b, "\n%s: %s", name, i18n.T("pgpcard.detail.empty"))
						continue
					}
					anyFpr = true
					fmt.Fprintf(&b, "\n%s: %s\n  %s\n  %s", name, si.Algo,
						groupFpr(si.Fingerprint), si.Created.Format(i18n.T("date.short")))
				}
				if !anyFpr {
					b.WriteString("\n\n" + i18n.T("pgpcard.detail.nofpr"))
				}
			}
			text := b.String()
			if text == "" {
				text = i18n.T("pgpcard.status.none")
			}
			label := widget.NewLabel(text)
			label.Wrapping = fyne.TextWrapWord
			label.TextStyle.Monospace = true
			var d dialog.Dialog
			// Kartenaktionen: bei mehreren Karten erst wählen (cardPick).
			unblockBtn := widget.NewButton(i18n.T("pgpcard.unblock.btn"), func() {
				d.Hide()
				u.cardPick(win, cards, func(c pgpcard.CardInfo) { u.showCardUnblock(win, c) })
			})
			var resettable []pgpcard.CardInfo
			for _, c := range cards {
				if c.CanReset {
					resettable = append(resettable, c)
				}
				if c.PINRetries == 0 {
					unblockBtn.Importance = widget.WarningImportance
				}
			}
			resetBtn := widget.NewButton(i18n.T("pgpcard.reset.btn"), func() {
				d.Hide()
				u.cardPick(win, resettable, func(c pgpcard.CardInfo) { u.showCardFactoryReset(win, c, done) })
			})
			resetBtn.Importance = widget.DangerImportance
			if len(cards) == 0 {
				unblockBtn.Disable()
			}
			if len(resettable) == 0 {
				resetBtn.Disable()
			}
			content := container.NewBorder(nil, container.NewHBox(unblockBtn, resetBtn), nil, nil,
				container.NewVScroll(label))
			d = dialog.NewCustom(i18n.T("pgpcard.detail.title"), i18n.T("contacts.close"), content, win)
			d.Resize(fyne.NewSize(560, 520))
			d.Show()
		})
	}()
}

// showCardExtendDialog verlängert die Karten-Subkeys um ein Jahr (neue
// Binding-Signaturen; das Kartenmaterial bleibt unverändert). Geht nur,
// wenn der Master im Secring liegt (nicht bei fremd eingerichteten Karten).
func (u *UI) showCardExtendDialog(win fyne.Window, stub *pgpcard.Stub, done func()) {
	ent := u.keyring.FindPrivateByFingerprint(stub.KeyFingerprint)
	if ent == nil {
		u.showError(i18n.Errorf("err.pgp.keynotfound", stub.KeyFingerprint))
		return
	}
	var info pgp.KeyInfo
	for _, i := range u.keyring.ListPrivate() {
		if i.Fingerprint == stub.KeyFingerprint {
			info = i
			break
		}
	}
	subFprs := stubSubkeyFprs(stub)
	extend := func(unlocked *pgp.Entity) {
		if err := u.keyring.ExtendSubkeys(stub.KeyFingerprint, subFprs, pgp.CardSubkeyLifetime, unlocked); err != nil {
			u.showError(err)
			return
		}
		msg := widget.NewLabel(i18n.T("pgpcard.extend.done"))
		msg.Wrapping = fyne.TextWrapWord
		d := dialog.NewCustom(i18n.T("pgpcard.title"), i18n.T("contacts.close"), msg, win)
		d.Resize(fyne.NewSize(500, 180))
		d.Show()
		done()
	}
	if !pgp.NeedsPassphrase(ent) {
		extend(ent)
		return
	}
	u.promptPassphrase(win, ent, info, "", func(unlocked *pgp.Entity, ok bool) {
		if !ok {
			return
		}
		extend(unlocked)
	})
}

func stubSubkeyFprs(stub *pgpcard.Stub) []string {
	out := []string{stub.SignSubkey}
	if stub.DecryptSubkey != "" {
		out = append(out, stub.DecryptSubkey)
	}
	if stub.AuthSubkey != "" {
		out = append(out, stub.AuthSubkey)
	}
	return out
}

// cardExpiryWarning meldet beim Start ablaufende Karten-Subkeys (<30 Tage).
func (u *UI) cardExpiryWarning() {
	if u.pgpCardKey == nil {
		return
	}
	stub := u.pgpCardKey.Stub
	pub := u.keyring.FindPublicByFingerprint(stub.KeyFingerprint)
	if pub == nil {
		return
	}
	want := map[string]bool{}
	for _, f := range stubSubkeyFprs(stub) {
		want[f] = true
	}
	var earliest time.Time
	for i := range pub.Subkeys {
		sk := &pub.Subkeys[i]
		if !want[pgpcard.FingerprintHex(sk.PublicKey.Fingerprint)] || sk.Sig.KeyLifetimeSecs == nil {
			continue
		}
		exp := sk.PublicKey.CreationTime.Add(time.Duration(*sk.Sig.KeyLifetimeSecs) * time.Second)
		if earliest.IsZero() || exp.Before(earliest) {
			earliest = exp
		}
	}
	if earliest.IsZero() {
		return
	}
	days := int(time.Until(earliest).Hours() / 24)
	if days > 30 {
		return
	}
	if days < 0 {
		days = 0
	}
	text := fmt.Sprintf(i18n.T("pgpcard.expiry.warn"), days)
	u.log.append(text)
	dialog.ShowInformation(i18n.T("pgpcard.title"), text, u.win)
}
