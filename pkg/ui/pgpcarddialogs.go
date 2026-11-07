package ui

import (
	"context"
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"github.com/cfluegel/uucpmail-client/pkg/pgp"
	"github.com/cfluegel/uucpmail-client/pkg/pgpcard"
)

// uiCardPrompter fragt die Karten-PIN modal ab (fyne.Do + Kanal, Muster
// uiPrompter in fido2dialogs.go). retries >= 0 zeigt die Restversuche an.
type uiCardPrompter struct{ u *UI }

func (p uiCardPrompter) RequestPIN(ctx context.Context, info pgpcard.CardInfo, retries int) (string, error) {
	type res struct {
		pin string
		err error
	}
	ch := make(chan res, 1)
	fyne.Do(func() {
		entry := widget.NewPasswordEntry()
		var d dialog.Dialog
		submit := func() {
			d.Hide()
			ch <- res{pin: entry.Text}
		}
		entry.OnSubmitted = func(string) { submit() }
		okBtn := widget.NewButton(i18n.T("settings.save"), submit)
		okBtn.Importance = widget.HighImportance
		cancelBtn := widget.NewButton(i18n.T("compose.cancel"), func() {
			d.Hide()
			ch <- res{err: errors.New(i18n.T("pgpcard.pin.cancelled"))}
		})
		label := fmt.Sprintf(i18n.T("pgpcard.pin.label"), info.Serial)
		box := container.NewVBox(widget.NewLabel(label))
		if retries >= 0 {
			warn := widget.NewLabel(fmt.Sprintf(i18n.T("pgpcard.pin.retries"), retries))
			warn.Importance = widget.WarningImportance
			box.Add(warn)
		}
		box.Add(entry)
		box.Add(container.NewCenter(container.NewHBox(cancelBtn, okBtn)))
		win := p.u.dialogParent()
		d = dialog.NewCustomWithoutButtons(i18n.T("pgpcard.title"), box, win)
		d.Show()
		win.Canvas().Focus(entry)
	})
	select {
	case r := <-ch:
		return r.pin, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// cardSource implementiert pgp.CardSource: liefert die kartengebundene
// Entity, wenn der aktive Schlüssel der Identität laut Stub auf der Karte
// liegt – sonst nil (normaler Datei-Schlüssel-Weg).
type cardSource struct{ u *UI }

func (c *cardSource) CardEntity(identity string) (*pgp.Entity, error) {
	ck := c.u.pgpCardKey
	if ck == nil || !c.u.isCardIdentity(identity) {
		return nil, nil
	}
	pub := c.u.keyring.FindPublicByFingerprint(ck.Stub.KeyFingerprint)
	if pub == nil {
		return nil, nil
	}
	// Karte erreichbar? Fehlt sie (z.B. ausgesteckt), hier mit klarer Meldung
	// abbrechen. Grund: go-crypto verschluckt im RSA-Signierpfad den Fehler
	// des Karten-Signers (Variable-Shadowing) und meldet sonst kryptisch
	// „need to call Sign before Serialize". Diese Vorabprüfung läuft in der
	// Sende-Goroutine, nicht im UI-Thread.
	sess, err := ck.Backend.Open(context.Background(), ck.Stub.Serial)
	if err != nil {
		return nil, err
	}
	_ = sess.Close()
	return pgpcard.BindEntity(pub, ck), nil
}

// isCardIdentity meldet, ob der aktive Schlüssel dieser Identität auf der
// Karte liegt – reine Fingerprint-Prüfung gegen den Stub, OHNE Kartenzugriff
// (damit im UI-Thread aufrufbar, z.B. in ensureUnlockedSigner).
func (u *UI) isCardIdentity(identity string) bool {
	if u.pgpCardKey == nil {
		return false
	}
	pub := u.keyring.FindPublicByEmail(identity)
	return pub != nil && pgpcard.FingerprintHex(pub.PrimaryKey.Fingerprint) == u.pgpCardKey.Stub.KeyFingerprint
}

// installCardSource verdrahtet den kartengestützten Schlüssel gemäß Config
// (pgp_card + vorhandener Stub); PIN-Cache nutzt die PGP-Cache-Dauer.
func (u *UI) installCardSource() {
	u.pgpCardKey = nil
	u.sealer.Card = nil
	if !u.cfg.PGPCard {
		return
	}
	stubPath, err := config.PGPCardStubPath()
	if err != nil {
		return
	}
	stub, err := pgpcard.LoadStub(stubPath)
	if err != nil {
		fmt.Println("pgpcard stub not loadable, using file key:", err)
		return
	}
	u.pgpCardKey = &pgpcard.CardKey{
		Backend: pgpcard.DefaultBackend(),
		Stub:    stub,
		Prompt:  uiCardPrompter{u: u},
		Cache:   pgpcard.NewPINCache(pgpCacheTTL(u.cfg.PGPCacheMin)),
	}
	u.sealer.Card = &cardSource{u: u}
}

// cardBoundEntityFor liefert die kartengebundene Entity zu einem
// Fingerprint (Entschlüsselungs-Pfad), sonst nil.
func (u *UI) cardBoundEntityFor(fingerprint string) *pgp.Entity {
	if u.pgpCardKey == nil || u.pgpCardKey.Stub.KeyFingerprint != fingerprint {
		return nil
	}
	pub := u.keyring.FindPublicByFingerprint(fingerprint)
	if pub == nil {
		return nil
	}
	return pgpcard.BindEntity(pub, u.pgpCardKey)
}
