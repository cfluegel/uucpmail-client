package pgpcard

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"os"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ebfe/scard"

	"github.com/cfluegel/uucpmail-client/pkg/pgp"
)

// Hardware-Tests laufen nur mit echter Karte und explizitem Opt-in:
//
//	PGPCARD_HW=1 [PGPCARD_PIN=… PGPCARD_ADMIN=…] go test ./pkg/pgpcard/ -run HW -v
//
// ACHTUNG: TestHWImportRoundtrip ÜBERSCHREIBT die Slots der eingesteckten
// Karte – nur mit einer Test-/Leerkarte ausführen (zusätzliches Opt-in
// PGPCARD_HW_WRITE=1).
func hwBackend(t *testing.T) Backend {
	t.Helper()
	if os.Getenv("PGPCARD_HW") == "" {
		t.Skip("Hardware-Test: PGPCARD_HW=1 setzen")
	}
	return DefaultBackend()
}

func TestHWListCards(t *testing.T) {
	b := hwBackend(t)
	cards, err := b.Cards(context.Background())
	if err != nil {
		t.Fatalf("Cards: %v", err)
	}
	for _, c := range cards {
		t.Logf("Karte: Serial=%s Hersteller=%s Reader=%q RSABits=%v", c.Serial, c.Manufacturer, c.Reader, c.RSABits)
		for slot, si := range c.Slots {
			t.Logf("  %-7s present=%v algo=%s fpr=%s created=%s", slot, si.Present, si.Algo, si.Fingerprint, si.Created)
		}
	}
}

// TestHWRawDump liest zentrale DOs roh (ohne Bibliothek, nur GET DATA –
// rein lesend) und loggt sie als Hex: Grundlage für Testvektoren neuer
// Kartenmodelle. Le erweitert (00 08 00), da z.B. FA sonst 6700 liefert.
func TestHWRawDump(t *testing.T) {
	hwBackend(t)
	sctx, err := scard.EstablishContext()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sctx.Release() }()
	readers, err := sctx.ListReaders()
	if err != nil || len(readers) == 0 {
		t.Fatalf("keine Reader: %v", err)
	}
	for _, r := range readers {
		card, err := sctx.Connect(r, scard.ShareShared, scard.ProtocolAny)
		if err != nil {
			t.Logf("%s: %v", r, err)
			continue
		}
		tx := func(apdu ...byte) []byte {
			resp, err := card.Transmit(apdu)
			if err != nil {
				t.Fatalf("Transmit % X: %v", apdu, err)
			}
			return resp
		}
		t.Logf("Reader %q", r)
		t.Logf("SELECT: % X", tx(0x00, 0xA4, 0x04, 0x00, 0x06, 0xD2, 0x76, 0x00, 0x01, 0x24, 0x01, 0x00))
		for _, do := range [][2]byte{{0x00, 0x6E}, {0x5F, 0x52}, {0x00, 0xC4}, {0x00, 0xFA}, {0x00, 0xF9}, {0x7F, 0x66}, {0x00, 0x7A}} {
			t.Logf("GET DATA %02X%02X: % X", do[0], do[1], tx(0x00, 0xCA, do[0], do[1], 0x00, 0x08, 0x00))
		}
		_ = card.Disconnect(scard.LeaveCard)
	}
}

// TestHWImportRoundtrip ist der Kern-Spike: keytocard + PSO:SIGN +
// PSO:DECIPHER auf echter Hardware, verifiziert gegen go-crypto.
// ÜBERSCHREIBT die Karte – zusätzliches Opt-in PGPCARD_HW_WRITE=1.
// PINs via PGPCARD_PIN/PGPCARD_ADMIN (Default: Werks-PINs).
func TestHWImportRoundtrip(t *testing.T) {
	b := hwBackend(t)
	if os.Getenv("PGPCARD_HW_WRITE") == "" {
		t.Skip("Schreibtest: PGPCARD_HW_WRITE=1 setzen (überschreibt die Karte!)")
	}
	pin := os.Getenv("PGPCARD_PIN")
	if pin == "" {
		pin = DefaultPIN
	}
	adminPIN := os.Getenv("PGPCARD_ADMIN")
	if adminPIN == "" {
		adminPIN = DefaultAdminPIN
	}

	ent, err := pgp.GenerateCardEntity("HW Test", []string{"hwtest@uucpmail.de"}, 3072, 0)
	if err != nil {
		t.Fatal(err)
	}
	var signSK, decSK, authSK *openpgp.Subkey
	for i := range ent.Subkeys {
		sk := &ent.Subkeys[i]
		switch {
		case sk.Sig.FlagSign:
			signSK = sk
		case sk.Sig.FlagEncryptCommunications || sk.Sig.FlagEncryptStorage:
			decSK = sk
		case sk.Sig.FlagAuthenticate:
			authSK = sk
		}
	}

	sess, err := b.Open(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if err := sess.VerifyAdminPIN(adminPIN); err != nil {
		t.Fatalf("Admin-PIN abgelehnt (PGPCARD_ADMIN setzen?): %v – ABBRUCH, um den Zähler zu schonen", err)
	}

	for _, e := range []struct {
		slot Slot
		sk   *openpgp.Subkey
	}{{SlotSign, signSK}, {SlotDecrypt, decSK}, {SlotAuth, authSK}} {
		start := time.Now()
		if err := sess.ImportKey(e.slot, e.sk.PrivateKey.PrivateKey, e.sk.PublicKey.CreationTime); err != nil {
			t.Fatalf("ImportKey %s: %v", e.slot, err)
		}
		t.Logf("ImportKey %s ok (%.1fs)", e.slot, time.Since(start).Seconds())
	}

	// Karten-Fingerprints: Spec-3.x-Karten geben sie zurück und müssen dann
	// den Keyring-Fingerprints entsprechen. Ältere Karten (ZeitControl v2.1)
	// liefern die Fingerprint-DOs nicht – das ist nur fürs Status-/Adopt-UI
	// relevant, nicht für Sign/Decrypt. Daher: prüfen, wenn vorhanden.
	info := sess.Info()
	if fp := info.Slots[SlotSign].Fingerprint; fp == "" {
		t.Log("Hinweis: Karte liefert keine SIG-Fingerprint-DOs (Status/Adopt eingeschränkt)")
	} else if want := FingerprintHex(signSK.PublicKey.Fingerprint); fp != want {
		t.Errorf("SIG-Fingerprint: Karte %s, Keyring %s", fp, want)
	}
	if fp := info.Slots[SlotDecrypt].Fingerprint; fp != "" {
		if want := FingerprintHex(decSK.PublicKey.Fingerprint); fp != want {
			t.Errorf("DEC-Fingerprint: Karte %s, Keyring %s", fp, want)
		}
	}

	if err := sess.VerifyPIN(pin); err != nil {
		t.Fatalf("Nutzer-PIN abgelehnt (PGPCARD_PIN setzen?): %v", err)
	}

	// Roh-Roundtrip: PSO:SIGN gegen crypto/rsa-Verify.
	signer, err := sess.Signer(&signSK.PrivateKey.PrivateKey.(*rsa.PrivateKey).PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("uucpmail hw spike"))
	start := time.Now()
	sig, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		t.Fatalf("PSO:SIGN: %v", err)
	}
	t.Logf("PSO:SIGN ok (%.1fs)", time.Since(start).Seconds())
	if err := rsa.VerifyPKCS1v15(&signSK.PrivateKey.PrivateKey.(*rsa.PrivateKey).PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("Karten-Signatur ungültig: %v", err)
	}

	// Roh-Roundtrip: PSO:DECIPHER gegen crypto/rsa-Encrypt.
	decPub := &decSK.PrivateKey.PrivateKey.(*rsa.PrivateKey).PublicKey
	ct, err := rsa.EncryptPKCS1v15(rand.Reader, decPub, []byte("spike geheim"))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := sess.Decrypter(decPub)
	if err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	plain, err := dec.Decrypt(rand.Reader, ct, nil)
	if err != nil {
		t.Fatalf("PSO:DECIPHER: %v", err)
	}
	t.Logf("PSO:DECIPHER ok (%.1fs)", time.Since(start).Seconds())
	if string(plain) != "spike geheim" {
		t.Fatalf("Decrypt-Plaintext: %q", plain)
	}
	_ = sess.Close()

	// Voller go-crypto-Pfad über BindEntity (wie im Mailbetrieb).
	var pubBuf bytes.Buffer
	if err := ent.Serialize(&pubBuf); err != nil {
		t.Fatal(err)
	}
	pubs, err := openpgp.ReadKeyRing(&pubBuf)
	if err != nil {
		t.Fatal(err)
	}
	stub := &Stub{
		Version: 1, Serial: info.Serial,
		KeyFingerprint: FingerprintHex(ent.PrimaryKey.Fingerprint),
		SignSubkey:     FingerprintHex(signSK.PublicKey.Fingerprint),
		DecryptSubkey:  FingerprintHex(decSK.PublicKey.Fingerprint),
		AuthSubkey:     FingerprintHex(authSK.PublicKey.Fingerprint),
		Algo:           AlgoRSA3072, Created: ent.PrimaryKey.CreationTime,
	}
	ck := &CardKey{Backend: b, Stub: stub, Prompt: envPrompter{pin: pin}, Cache: NewPINCache(10 * time.Minute)}
	bound := BindEntity(pubs[0], ck)

	msg := []byte("Hallo von der echten Karte!\n")
	armSig, err := pgp.SignDetached(bound, msg)
	if err != nil {
		t.Fatalf("SignDetached über Karte: %v", err)
	}
	if _, err := openpgp.CheckArmoredDetachedSignature(openpgp.EntityList{pubs[0]}, bytes.NewReader(msg), bytes.NewReader(armSig), nil); err != nil {
		t.Errorf("Detached-Signatur verifiziert nicht: %v", err)
	}
	ctMail, err := pgp.Encrypt([]byte("verschlüsselte Mail"), []*openpgp.Entity{pubs[0]}, nil)
	if err != nil {
		t.Fatal(err)
	}
	plainMail, _, err := pgp.Decrypt(ctMail, openpgp.EntityList{bound}, nil)
	if err != nil {
		t.Fatalf("pgp.Decrypt über Karte: %v", err)
	}
	if string(plainMail) != "verschlüsselte Mail" {
		t.Errorf("Mail-Plaintext: %q", plainMail)
	}
}

type envPrompter struct{ pin string }

func (p envPrompter) RequestPIN(context.Context, CardInfo, int) (string, error) {
	return p.pin, nil
}

// TestHWFactoryResetImport setzt die Karte auf Werk zurück und importiert auf
// der frischen Karte EINEN 2048-Signaturschlüssel (native Größe dieser Karte,
// ohne Attribut-Umstellung) über die normale Session-API. Sauberer
// Einzelversuch ohne Alt-Zustand. PGPCARD_HW_RESET=1 (destruktiv!).
func TestHWFactoryResetImport(t *testing.T) {
	if os.Getenv("PGPCARD_HW") == "" || os.Getenv("PGPCARD_HW_RESET") == "" {
		t.Skip("PGPCARD_HW=1 PGPCARD_HW_RESET=1 setzen (Werksreset!)")
	}
	// 1) Werksreset über den rohen Kartenzugriff.
	sctx, err := scard.EstablishContext()
	if err != nil {
		t.Fatal(err)
	}
	readers, err := sctx.ListReaders()
	if err != nil || len(readers) == 0 {
		t.Fatalf("keine Reader: %v", err)
	}
	card, err := openOnReader(sctx, readers[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := card.FactoryReset(); err != nil {
		_ = card.Close()
		_ = sctx.Release()
		t.Fatalf("FactoryReset: %v", err)
	}
	_ = card.Close()
	_ = sctx.Release()
	t.Log("FactoryReset ok")

	// 2) Frische Karte über die Session-API: 2048-Schlüssel importieren.
	sess, err := DefaultBackend().Open(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if err := sess.VerifyAdminPIN(DefaultAdminPIN); err != nil {
		t.Fatalf("Admin-PIN (default): %v", err)
	}
	ent, err := pgp.GenerateCardEntity("Reset Test", []string{"reset@uucpmail.de"}, 2048, 0)
	if err != nil {
		t.Fatal(err)
	}
	var signSK *openpgp.Subkey
	for i := range ent.Subkeys {
		if ent.Subkeys[i].Sig.FlagSign {
			signSK = &ent.Subkeys[i]
		}
	}
	if err := sess.ImportKey(SlotSign, signSK.PrivateKey.PrivateKey, signSK.PublicKey.CreationTime); err != nil {
		t.Fatalf("ImportKey sign (frische Karte, 2048): %v", err)
	}
	t.Log("ImportKey sign ok auf frischer Karte")
	if err := sess.VerifyPIN(DefaultPIN); err != nil {
		t.Fatal(err)
	}
	signer, err := sess.Signer(&signSK.PrivateKey.PrivateKey.(*rsa.PrivateKey).PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("reset spike"))
	sig, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		t.Fatalf("PSO:SIGN: %v", err)
	}
	if err := rsa.VerifyPKCS1v15(&signSK.PrivateKey.PrivateKey.(*rsa.PrivateKey).PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("Signatur ungültig: %v", err)
	}
	t.Log("PSO:SIGN verifiziert – Pipeline auf frischer Karte funktioniert")
}
