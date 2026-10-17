package pgp

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestKeyring(t *testing.T) *Keyring {
	t.Helper()
	dir := t.TempDir()
	k, err := LoadKeyring(filepath.Join(dir, "pubring.asc"), filepath.Join(dir, "secring.asc"))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestGenerateSaveLoadRoundtrip(t *testing.T) {
	k := newTestKeyring(t)
	info, err := k.Generate("Christoph Test", []string{"test@uucpmail.de"}, "geheim123", 0)
	if err != nil {
		t.Fatal(err)
	}
	if info.Email != "test@uucpmail.de" || info.Name != "Christoph Test" || !info.IsPrivate {
		t.Errorf("info: %+v", info)
	}
	if len(info.Fingerprint) != 40 {
		t.Errorf("fingerprint: %q", info.Fingerprint)
	}
	// Dateirechte 0600
	for _, p := range []string{k.secPath, k.pubPath} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %o", p, st.Mode().Perm())
		}
	}
	// Neu laden: privater Schlüssel verschlüsselt, per E-Mail auffindbar.
	k2, err := LoadKeyring(k.pubPath, k.secPath)
	if err != nil {
		t.Fatal(err)
	}
	priv := k2.FindPrivateByEmail("Test <TEST@uucpmail.de>")
	if priv == nil {
		t.Fatal("privater Schlüssel nach Reload nicht auffindbar")
	}
	if !NeedsPassphrase(priv) {
		t.Error("privater Schlüssel liegt unverschlüsselt auf der Platte")
	}
	if k2.FindPublicByEmail("test@uucpmail.de") == nil {
		t.Error("public-Hälfte fehlt im Pubring")
	}
}

func TestUnlockWrongAndRightPassphrase(t *testing.T) {
	k := newTestKeyring(t)
	if _, err := k.Generate("T", []string{"t@uucpmail.de"}, "richtig", 0); err != nil {
		t.Fatal(err)
	}
	priv := k.FindPrivateByEmail("t@uucpmail.de")
	if _, err := Unlock(priv, "falsch"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("err = %v, erwartet ErrWrongPassphrase", err)
	}
	unlocked, err := Unlock(priv, "richtig")
	if err != nil {
		t.Fatal(err)
	}
	if NeedsPassphrase(unlocked) {
		t.Error("kopie nicht entsperrt")
	}
	// Original bleibt verschlüsselt (Deep-Copy-Garantie).
	if !NeedsPassphrase(priv) {
		t.Error("original wurde mit entsperrt")
	}
}

func TestSignVerifyAndTamper(t *testing.T) {
	k := newTestKeyring(t)
	k.Generate("Signer", []string{"s@uucpmail.de"}, "pw", 0)
	unlocked, err := Unlock(k.FindPrivateByEmail("s@uucpmail.de"), "pw")
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("Content-Type: text/plain\r\n\r\nWichtiger Inhalt\r\n")
	sig, err := SignDetached(unlocked, data)
	if err != nil {
		t.Fatal(err)
	}
	if res := Verify(data, sig, k.PublicEntities()); res.Status != SigValid ||
		res.SignerEmail != "s@uucpmail.de" {
		t.Errorf("verify: %+v", res)
	}
	// Manipulation → ungültig.
	tampered := bytes.Replace(data, []byte("Wichtiger"), []byte("Gefälscht"), 1)
	if res := Verify(tampered, sig, k.PublicEntities()); res.Status != SigInvalid {
		t.Errorf("tamper nicht erkannt: %+v", res)
	}
	// Unbekannter Schlüssel → SigUnknownKey.
	other := newTestKeyring(t)
	other.Generate("Other", []string{"o@uucpmail.de"}, "", 0)
	if res := Verify(data, sig, other.PublicEntities()); res.Status != SigUnknownKey {
		t.Errorf("unknown key: %+v", res)
	}
}

func TestEncryptDecryptRoundtripWithSelfAndSignature(t *testing.T) {
	alice := newTestKeyring(t)
	alice.Generate("Alice", []string{"alice@uucpmail.de"}, "alicepw", 0)
	bob := newTestKeyring(t)
	bob.Generate("Bob", []string{"bob@example.com"}, "bobpw", 0)

	// Alice verschlüsselt an Bob + sich selbst, signiert.
	aliceUnlocked, err := Unlock(alice.FindPrivateByEmail("alice@uucpmail.de"), "alicepw")
	if err != nil {
		t.Fatal(err)
	}
	// Bobs Public Key in Alices Ring importieren.
	bobPub, err := bob.ExportPublic(bob.ListPrivate()[0].Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := alice.ImportPublic(bobPub); err != nil {
		t.Fatal(err)
	}
	plain := []byte("Content-Type: text/plain; charset=utf-8\r\n\r\nGeheime Grüße\r\n")
	recipients := []*Entity{
		alice.FindPublicByEmail("bob@example.com"),
		alice.FindPublicByEmail("alice@uucpmail.de"), // encrypt-to-self
	}
	armored, err := Encrypt(plain, recipients, aliceUnlocked)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(armored, []byte("BEGIN PGP MESSAGE")) {
		t.Fatal("kein Armor")
	}

	// Bob entschlüsselt (braucht Alices Public Key für die Signaturprüfung).
	alicePub, _ := alice.ExportPublic(alice.ListPrivate()[0].Fingerprint)
	bob.ImportPublic(alicePub)
	bobUnlocked, err := Unlock(bob.FindPrivateByEmail("bob@example.com"), "bobpw")
	if err != nil {
		t.Fatal(err)
	}
	got, sig, err := Decrypt(armored, EntityList{bobUnlocked}, bob.PublicEntities())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("klartext verändert: %q", got)
	}
	if sig.Status != SigValid || sig.SignerEmail != "alice@uucpmail.de" {
		t.Errorf("eingebettete Signatur: %+v", sig)
	}

	// Encrypt-to-self: Alice kann ihre Sent-Kopie lesen.
	got2, _, err := Decrypt(armored, EntityList{aliceUnlocked}, alice.PublicEntities())
	if err != nil || !bytes.Equal(got2, plain) {
		t.Errorf("encrypt-to-self fehlgeschlagen: %v", err)
	}

	// Fremder ohne Schlüssel → ErrNoPrivateKey.
	eve := newTestKeyring(t)
	eve.Generate("Eve", []string{"eve@example.com"}, "", 0)
	evePriv := eve.FindPrivateByEmail("eve@example.com")
	if _, _, err := Decrypt(armored, EntityList{evePriv}, nil); !errors.Is(err, ErrNoPrivateKey) {
		t.Errorf("err = %v, erwartet ErrNoPrivateKey", err)
	}
}

func TestClearsignVerify(t *testing.T) {
	k := newTestKeyring(t)
	k.Generate("C", []string{"c@uucpmail.de"}, "", 0)
	// Clearsign-Block mit go-crypto bauen ist Empfangs-Feature; hier reicht:
	// kaputter Block → SigInvalid, Klartext bleibt lesbar.
	plain, res := VerifyClearsigned([]byte("kein pgp block"), k.PublicEntities())
	if res.Status != SigInvalid || len(plain) == 0 {
		t.Errorf("res=%+v", res)
	}
}

func TestKeyringPublicManagement(t *testing.T) {
	k := newTestKeyring(t)
	k.Generate("Me", []string{"me@uucpmail.de"}, "pw", 0)
	other := newTestKeyring(t)
	other.Generate("Peer", []string{"peer@example.com"}, "", 0)
	pub, _ := other.ExportPublic(other.ListPrivate()[0].Fingerprint)

	infos, err := k.ImportPublic(pub)
	if err != nil || len(infos) != 1 || infos[0].Email != "peer@example.com" {
		t.Fatalf("import: %+v err=%v", infos, err)
	}
	// Duplikat → Fehler "kein neuer Schlüssel".
	if _, err := k.ImportPublic(pub); err == nil {
		t.Error("duplikat nicht erkannt")
	}
	if len(k.ListPublic()) != 1 { // eigener Key zählt nicht als fremder
		t.Errorf("ListPublic: %d", len(k.ListPublic()))
	}
	if missing := k.MissingRecipients([]string{"peer@example.com", "unbekannt@example.com"}); len(missing) != 1 || missing[0] != "unbekannt@example.com" {
		t.Errorf("missing: %v", missing)
	}
	// Eigenen Public nicht löschbar, fremden schon.
	if err := k.DeletePublic(k.ListPrivate()[0].Fingerprint); err == nil {
		t.Error("eigener Key löschbar")
	}
	if err := k.DeletePublic(infos[0].Fingerprint); err != nil {
		t.Fatal(err)
	}
	if len(k.ListPublic()) != 0 {
		t.Error("fremder Key nicht gelöscht")
	}
}

func TestCacheExpiry(t *testing.T) {
	c := NewCache(15 * time.Minute)
	clock := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return clock }

	k := newTestKeyring(t)
	k.Generate("C", []string{"c@uucpmail.de"}, "pw", 0)
	ent, _ := Unlock(k.FindPrivateByEmail("c@uucpmail.de"), "pw")
	fpr := k.ListPrivate()[0].Fingerprint

	c.Put(fpr, ent)
	if c.Get(fpr) == nil {
		t.Fatal("cache-miss direkt nach Put")
	}
	clock = clock.Add(14 * time.Minute)
	if c.Get(fpr) == nil {
		t.Fatal("cache-miss vor Ablauf")
	}
	clock = clock.Add(2 * time.Minute) // 16 min > TTL
	if c.Get(fpr) != nil {
		t.Fatal("cache liefert abgelaufenen Schlüssel")
	}

	// TTL 0 = nie cachen.
	c0 := NewCache(0)
	c0.Put(fpr, ent)
	if c0.Get(fpr) != nil {
		t.Error("TTL 0 cached trotzdem")
	}
}

func TestDecryptionCandidates(t *testing.T) {
	k := newTestKeyring(t)
	k.Generate("A", []string{"a@uucpmail.de"}, "pwA", 0)
	k.Generate("B", []string{"b@uucpmail.de"}, "pwB", 0)

	// Nur an B verschlüsseln.
	armored, err := Encrypt([]byte("geheim"), []*Entity{k.FindPublicByEmail("b@uucpmail.de")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cands := DecryptionCandidates(armored, k)
	if len(cands) != 1 || cands[0].Email != "b@uucpmail.de" {
		t.Fatalf("kandidaten: %+v", cands)
	}

	// An beide → beide Kandidaten.
	armored2, _ := Encrypt([]byte("geheim"), []*Entity{
		k.FindPublicByEmail("a@uucpmail.de"), k.FindPublicByEmail("b@uucpmail.de")}, nil)
	if got := len(DecryptionCandidates(armored2, k)); got != 2 {
		t.Fatalf("kandidaten: %d", got)
	}

	// An Fremdschlüssel → keiner passt.
	other := newTestKeyring(t)
	other.Generate("X", []string{"x@example.com"}, "", 0)
	armored3, _ := Encrypt([]byte("geheim"), []*Entity{other.FindPublicByEmail("x@example.com")}, nil)
	if got := len(DecryptionCandidates(armored3, k)); got != 0 {
		t.Fatalf("kandidaten: %d", got)
	}
}

func TestDeletePrivateRemovesBothHalves(t *testing.T) {
	k := newTestKeyring(t)
	k.Generate("A", []string{"a@uucpmail.de"}, "pw", 0)
	k.Generate("B", []string{"b@uucpmail.de"}, "pw", 0)
	fprA := ""
	for _, i := range k.ListPrivate() {
		if i.Email == "a@uucpmail.de" {
			fprA = i.Fingerprint
		}
	}
	if err := k.DeletePrivate(fprA); err != nil {
		t.Fatal(err)
	}
	if k.FindPrivateByEmail("a@uucpmail.de") != nil || k.FindPublicByEmail("a@uucpmail.de") != nil {
		t.Error("schlüsselhälften nicht vollständig entfernt")
	}
	if k.FindPrivateByEmail("b@uucpmail.de") == nil {
		t.Error("anderer Schlüssel mitgelöscht")
	}
	// Persistenz
	k2, _ := LoadKeyring(k.pubPath, k.secPath)
	if k2.FindPrivateByEmail("a@uucpmail.de") != nil {
		t.Error("löschung nicht persistiert")
	}
	if err := k.DeletePrivate("DEADBEEF"); err == nil {
		t.Error("unbekannter Fingerprint nicht abgelehnt")
	}
}
