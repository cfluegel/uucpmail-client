package pgpcard

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048) // klein = schnelle Tests
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestStubRoundtrip(t *testing.T) {
	st := &Stub{
		Version:        1,
		Serial:         "0005ABCD",
		KeyFingerprint: "AAAA000000000000000000000000000000000001",
		SignSubkey:     "AAAA000000000000000000000000000000000002",
		DecryptSubkey:  "AAAA000000000000000000000000000000000003",
		AuthSubkey:     "AAAA000000000000000000000000000000000004",
		Algo:           AlgoRSA3072,
		Created:        time.Now().UTC().Truncate(time.Second),
	}
	path := filepath.Join(t.TempDir(), "pgpcard_key.json")
	if err := st.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadStub(path)
	if err != nil {
		t.Fatal(err)
	}
	if *got != *st {
		t.Errorf("Stub-Roundtrip: got %+v, want %+v", got, st)
	}
}

func TestLoadStubRejectsBadAlgoAndMissingFields(t *testing.T) {
	dir := t.TempDir()
	bad := &Stub{Version: 1, Serial: "01", KeyFingerprint: "AA", SignSubkey: "BB", Algo: "dsa1024"}
	path := filepath.Join(dir, "bad.json")
	if err := bad.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStub(path); err == nil {
		t.Error("unbekannter Algo wurde akzeptiert")
	}
	empty := &Stub{Version: 1, Algo: AlgoRSA3072}
	path2 := filepath.Join(dir, "empty.json")
	if err := empty.Save(path2); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStub(path2); err == nil {
		t.Error("Stub ohne Fingerprints wurde akzeptiert")
	}
}

func TestFakeImportSignDecrypt(t *testing.T) {
	card := NewFakeCard("0005TEST")
	backend := &FakeBackend{Card: card}
	sess, err := backend.Open(context.Background(), "0005TEST")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()

	key := testRSAKey(t)
	created := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

	// Import verlangt Admin-PIN.
	if err := sess.ImportKey(SlotSign, key, created); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("Import ohne Admin-PIN: err=%v, erwartet ErrNotVerified", err)
	}
	if err := sess.VerifyAdminPIN(DefaultAdminPIN); err != nil {
		t.Fatal(err)
	}
	if err := sess.ImportKey(SlotSign, key, created); err != nil {
		t.Fatal(err)
	}
	if err := sess.ImportKey(SlotDecrypt, key, created); err != nil {
		t.Fatal(err)
	}

	// Signieren/Entschlüsseln verlangt Nutzer-PIN.
	if _, err := sess.Signer(key.Public()); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("Signer ohne PIN: err=%v, erwartet ErrNotVerified", err)
	}
	if err := sess.VerifyPIN(DefaultPIN); err != nil {
		t.Fatal(err)
	}
	signer, err := sess.Signer(key.Public())
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("uucpmail"))
	sig, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Errorf("Karten-Signatur ungültig: %v", err)
	}

	ct, err := rsa.EncryptPKCS1v15(rand.Reader, &key.PublicKey, []byte("geheim"))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := sess.Decrypter(key.Public())
	if err != nil {
		t.Fatal(err)
	}
	pt, err := dec.Decrypt(rand.Reader, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(pt) != "geheim" {
		t.Errorf("Decrypt: %q", pt)
	}

	// Slot-Infos tragen Fingerprints.
	info := sess.Info()
	if !info.Slots[SlotSign].Present || len(info.Slots[SlotSign].Fingerprint) != 40 {
		t.Errorf("SIG-Slot-Info unvollständig: %+v", info.Slots[SlotSign])
	}
	if info.Slots[SlotAuth].Present {
		t.Error("AUT-Slot fälschlich belegt")
	}
}

func TestFakePINRetriesAndBlock(t *testing.T) {
	card := NewFakeCard("0005TEST")
	sess, err := (&FakeBackend{Card: card}).Open(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	var pinErr *PINError
	if err := sess.VerifyPIN("000000"); !errors.As(err, &pinErr) || pinErr.Retries != 2 {
		t.Fatalf("1. Fehlversuch: err=%v, erwartet PINError{2}", err)
	}
	if err := sess.VerifyPIN("000000"); !errors.As(err, &pinErr) || pinErr.Retries != 1 {
		t.Fatalf("2. Fehlversuch: err=%v, erwartet PINError{1}", err)
	}
	if err := sess.VerifyPIN("000000"); !errors.Is(err, ErrPINBlocked) {
		t.Fatalf("3. Fehlversuch: err=%v, erwartet ErrPINBlocked", err)
	}
	if err := sess.VerifyPIN(DefaultPIN); !errors.Is(err, ErrPINBlocked) {
		t.Fatalf("richtige PIN nach Blockade: err=%v, erwartet ErrPINBlocked", err)
	}
}

func TestFakeChangePINEnforcesLength(t *testing.T) {
	card := NewFakeCard("0005TEST")
	sess, _ := (&FakeBackend{Card: card}).Open(context.Background(), "")
	if err := sess.ChangePIN(DefaultPIN, "123"); err == nil {
		t.Error("zu kurze PIN akzeptiert")
	}
	if err := sess.ChangePIN(DefaultPIN, "wunschpin"); err != nil {
		t.Fatal(err)
	}
	if err := sess.VerifyPIN("wunschpin"); err != nil {
		t.Errorf("neue PIN gilt nicht: %v", err)
	}
	if err := sess.ChangeAdminPIN(DefaultAdminPIN, "adminwunsch1"); err != nil {
		t.Fatal(err)
	}
	if err := sess.VerifyAdminPIN("adminwunsch1"); err != nil {
		t.Errorf("neue Admin-PIN gilt nicht: %v", err)
	}
}

func TestOpenWrongSerial(t *testing.T) {
	backend := &FakeBackend{Card: NewFakeCard("AAAA0001")}
	if _, err := backend.Open(context.Background(), "BBBB0002"); !errors.Is(err, ErrWrongCard) {
		t.Errorf("err=%v, erwartet ErrWrongCard", err)
	}
	if _, err := (&FakeBackend{}).Open(context.Background(), ""); !errors.Is(err, ErrNoCard) {
		t.Errorf("err=%v, erwartet ErrNoCard", err)
	}
}

func TestPINCacheTTL(t *testing.T) {
	now := time.Now()
	c := NewPINCache(10 * time.Minute)
	c.now = func() time.Time { return now }
	c.Put("S1", "123456")
	if pin, ok := c.Get("S1"); !ok || pin != "123456" {
		t.Fatalf("Get: %q %v", pin, ok)
	}
	now = now.Add(10*time.Minute + time.Second)
	if _, ok := c.Get("S1"); ok {
		t.Error("PIN nach TTL noch im Cache")
	}
	// TTL 0 = nie cachen.
	c0 := NewPINCache(0)
	c0.Put("S1", "123456")
	if _, ok := c0.Get("S1"); ok {
		t.Error("TTL 0 hat gecacht")
	}
	// Remove wirkt.
	c.Put("S2", "654321")
	c.Remove("S2")
	if _, ok := c.Get("S2"); ok {
		t.Error("Remove hat nicht entfernt")
	}
}

func TestFakeResetPINKDFFactoryReset(t *testing.T) {
	card := NewFakeCard("F1")
	card.KDFSupported = true
	b := &FakeBackend{Card: card}
	sess, _ := b.Open(context.Background(), "F1")
	if !sess.Info().KDFPossible() {
		t.Fatal("KDF sollte möglich sein")
	}
	if err := sess.SetupKDF(DefaultPIN, DefaultAdminPIN); err != nil || !sess.Info().KDFActive || sess.Info().KDFPossible() {
		t.Fatalf("SetupKDF: %v", err)
	}
	for i := 0; i < 3; i++ {
		_ = sess.VerifyPIN("falsch1")
	}
	if !errors.Is(sess.VerifyPIN(DefaultPIN), ErrPINBlocked) {
		t.Fatal("PIN sollte gesperrt sein")
	}
	if err := sess.ResetPIN("falschadmin", "999999"); err == nil {
		t.Fatal("falsche Admin-PIN akzeptiert")
	}
	if err := sess.ResetPIN(DefaultAdminPIN, "999999"); err != nil || sess.VerifyPIN("999999") != nil {
		t.Fatalf("ResetPIN: %v", err)
	}
	if err := sess.FactoryReset(); err != nil || sess.Info().KDFActive || card.PIN != DefaultPIN || !card.KDFSupported {
		t.Fatalf("FactoryReset: %v %+v", err, card)
	}
}
