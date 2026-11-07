package pgpcard_test

import (
	"bytes"
	"context"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"

	"github.com/cfluegel/uucpmail-client/pkg/pgp"
	"github.com/cfluegel/uucpmail-client/pkg/pgpcard"
)

type countingPrompter struct {
	pin   string
	calls int
}

func (p *countingPrompter) RequestPIN(context.Context, pgpcard.CardInfo, int) (string, error) {
	p.calls++
	return p.pin, nil
}

// TestCardEntityRoundtrip bildet den kompletten Wizard+Mailbetrieb-Pfad mit
// dem Fake-Backend ab: Schlüssel erzeugen → Subkeys „keytocard" → Stub →
// BindEntity → Signieren und Entschlüsseln laufen über die Karte.
func TestCardEntityRoundtrip(t *testing.T) {
	ent, err := pgp.GenerateCardEntity("Test Nutzer", []string{"test@uucpmail.de", "zweit@uucpmail.de"}, 3072, 2*pgp.CardSubkeyLifetime)
	if err != nil {
		t.Fatal(err)
	}

	// Struktur: drei Subkeys mit getrennten Rollen, Laufzeit 1 Jahr.
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
	if signSK == nil || decSK == nil || authSK == nil {
		t.Fatalf("Subkey-Rollen unvollständig: sign=%v dec=%v auth=%v", signSK != nil, decSK != nil, authSK != nil)
	}
	for _, sk := range []*openpgp.Subkey{signSK, decSK, authSK} {
		if sk.Sig.KeyLifetimeSecs == nil || *sk.Sig.KeyLifetimeSecs != uint32(pgp.CardSubkeyLifetime/time.Second) {
			t.Errorf("Subkey-Laufzeit falsch: %v", sk.Sig.KeyLifetimeSecs)
		}
	}

	// „keytocard": alle drei Subkeys auf die Fake-Karte.
	card := pgpcard.NewFakeCard("FAKE0001")
	backend := &pgpcard.FakeBackend{Card: card}
	sess, err := backend.Open(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.VerifyAdminPIN(pgpcard.DefaultAdminPIN); err != nil {
		t.Fatal(err)
	}
	for slot, sk := range map[pgpcard.Slot]*openpgp.Subkey{
		pgpcard.SlotSign:    signSK,
		pgpcard.SlotDecrypt: decSK,
		pgpcard.SlotAuth:    authSK,
	} {
		rsaKey, ok := sk.PrivateKey.PrivateKey.(*rsa.PrivateKey)
		if !ok {
			t.Fatalf("Subkey %s ist kein RSA-Schlüssel: %T", slot, sk.PrivateKey.PrivateKey)
		}
		if err := sess.ImportKey(slot, rsaKey, sk.PublicKey.CreationTime); err != nil {
			t.Fatalf("ImportKey %s: %v", slot, err)
		}
	}
	// Karten-Fingerprints müssen exakt den Keyring-Fingerprints entsprechen
	// (sonst stimmt die Clock/Fingerprint-Logik des Imports nicht).
	info := sess.Info()
	if got, want := info.Slots[pgpcard.SlotSign].Fingerprint, pgpcard.FingerprintHex(signSK.PublicKey.Fingerprint); got != want {
		t.Errorf("SIG-Fingerprint: Karte %s, Keyring %s", got, want)
	}
	_ = sess.Close()

	stub := &pgpcard.Stub{
		Version:        1,
		Serial:         card.Serial,
		KeyFingerprint: pgpcard.FingerprintHex(ent.PrimaryKey.Fingerprint),
		SignSubkey:     pgpcard.FingerprintHex(signSK.PublicKey.Fingerprint),
		DecryptSubkey:  pgpcard.FingerprintHex(decSK.PublicKey.Fingerprint),
		AuthSubkey:     pgpcard.FingerprintHex(authSK.PublicKey.Fingerprint),
		Algo:           pgpcard.AlgoRSA3072,
		Created:        ent.PrimaryKey.CreationTime,
	}

	// Öffentliche Hälfte wie aus dem Pubring.
	var pubBuf bytes.Buffer
	if err := ent.Serialize(&pubBuf); err != nil {
		t.Fatal(err)
	}
	pubs, err := openpgp.ReadKeyRing(&pubBuf)
	if err != nil {
		t.Fatal(err)
	}
	pubOnly := pubs[0]

	prompt := &countingPrompter{pin: pgpcard.DefaultPIN}
	ck := &pgpcard.CardKey{
		Backend: backend,
		Stub:    stub,
		Prompt:  prompt,
		Cache:   pgpcard.NewPINCache(10 * time.Minute),
	}
	bound := pgpcard.BindEntity(pubOnly, ck)

	// Signieren über die Karte; Verifikation gegen den Pubring.
	msg := []byte("Hallo, signiert von der Karte!\n")
	sig, err := pgp.SignDetached(bound, msg)
	if err != nil {
		t.Fatalf("SignDetached über Karte: %v", err)
	}
	if _, err := openpgp.CheckArmoredDetachedSignature(openpgp.EntityList{pubOnly}, bytes.NewReader(msg), bytes.NewReader(sig), nil); err != nil {
		t.Errorf("Karten-Signatur verifiziert nicht: %v", err)
	}

	// Verschlüsseln an den Pubkey, Entschlüsseln über die Karte.
	ct, err := pgp.Encrypt([]byte("geheime Mail"), []*openpgp.Entity{pubOnly}, nil)
	if err != nil {
		t.Fatal(err)
	}
	plain, _, err := pgp.Decrypt(ct, openpgp.EntityList{bound}, nil)
	if err != nil {
		t.Fatalf("Decrypt über Karte: %v", err)
	}
	if string(plain) != "geheime Mail" {
		t.Errorf("Plaintext: %q", plain)
	}

	// PIN wurde nur beim ersten Kartenzugriff abgefragt (Cache greift).
	if prompt.calls != 1 {
		t.Errorf("PIN-Prompts: %d, erwartet 1 (Cache)", prompt.calls)
	}
}

// TestCardEntityWrongPINRetry: falsche gecachte PIN wird verworfen und neu
// gepromptet; gesperrte Karte bricht sauber ab.
func TestCardEntityPINFlow(t *testing.T) {
	ent, err := pgp.GenerateCardEntity("Test", []string{"t@example.org"}, 3072, 0)
	if err != nil {
		t.Fatal(err)
	}
	var signSK *openpgp.Subkey
	for i := range ent.Subkeys {
		if ent.Subkeys[i].Sig.FlagSign {
			signSK = &ent.Subkeys[i]
		}
	}
	card := pgpcard.NewFakeCard("FAKE0002")
	card.PIN = "wunschpin"
	backend := &pgpcard.FakeBackend{Card: card}
	sess, _ := backend.Open(context.Background(), "")
	_ = sess.VerifyAdminPIN(pgpcard.DefaultAdminPIN)
	rsaKey := signSK.PrivateKey.PrivateKey.(*rsa.PrivateKey)
	if err := sess.ImportKey(pgpcard.SlotSign, rsaKey, signSK.PublicKey.CreationTime); err != nil {
		t.Fatal(err)
	}
	_ = sess.Close()

	stub := &pgpcard.Stub{
		Version: 1, Serial: card.Serial,
		KeyFingerprint: pgpcard.FingerprintHex(ent.PrimaryKey.Fingerprint),
		SignSubkey:     pgpcard.FingerprintHex(signSK.PublicKey.Fingerprint),
		Algo:           pgpcard.AlgoRSA3072, Created: ent.PrimaryKey.CreationTime,
	}
	var pubBuf bytes.Buffer
	_ = ent.Serialize(&pubBuf)
	pubs, _ := openpgp.ReadKeyRing(&pubBuf)

	// Veraltete PIN im Cache: wird verworfen, Prompt liefert die richtige.
	cache := pgpcard.NewPINCache(10 * time.Minute)
	cache.Put(card.Serial, "000000")
	prompt := &countingPrompter{pin: "wunschpin"}
	bound := pgpcard.BindEntity(pubs[0], &pgpcard.CardKey{Backend: backend, Stub: stub, Prompt: prompt, Cache: cache})
	if _, err := pgp.SignDetached(bound, []byte("x")); err != nil {
		t.Fatalf("Signieren nach PIN-Korrektur: %v", err)
	}
	if prompt.calls != 1 {
		t.Errorf("PIN-Prompts: %d, erwartet 1", prompt.calls)
	}
}
