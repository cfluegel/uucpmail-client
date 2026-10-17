package pgpmime_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cfluegel/uucpmail-client/pkg/compose"
	mailmime "github.com/cfluegel/uucpmail-client/pkg/mime"
	"github.com/cfluegel/uucpmail-client/pkg/pgp"
	"github.com/cfluegel/uucpmail-client/pkg/pgpmime"
)

func testKeyring(t *testing.T, name, email, pass string) *pgp.Keyring {
	t.Helper()
	dir := t.TempDir()
	k, err := pgp.LoadKeyring(filepath.Join(dir, "pub.asc"), filepath.Join(dir, "sec.asc"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.Generate(name, []string{email}, pass, 0); err != nil {
		t.Fatal(err)
	}
	return k
}

func testMessage(sign, encrypt bool) *compose.Message {
	return &compose.Message{
		Identity: "alice@uucpmail.de",
		To:       []string{"bob@example.com"},
		Subject:  "PGP Roundtrip",
		Body:     "Vertrauliche Grüße\nmit Umlauten äöü.",
		NodeName: "u7a2f4c",
		Date:     time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC),
		Sign:     sign,
		Encrypt:  encrypt,
		Attachments: []compose.Attachment{{
			Filename: "daten.bin", MIMEType: "application/octet-stream",
			Data: bytes.Repeat([]byte{0x00, 0xFF, 0x42}, 100),
		}},
	}
}

// TestSignedRoundtrip ist das harte CRLF-Gate: bauen → vollständige Mail →
// mailmime.Parse → SignedPart byte-identisch → Signatur gültig.
func TestSignedRoundtrip(t *testing.T) {
	alice := testKeyring(t, "Alice", "alice@uucpmail.de", "pw")
	signer, err := pgp.Unlock(alice.FindPrivateByEmail("alice@uucpmail.de"), "pw")
	if err != nil {
		t.Fatal(err)
	}
	msg := testMessage(true, false)
	outer, inner := msg.BuildParts("test", false)
	sig, err := pgp.SignDetached(signer, pgpmime.SignedPayload(inner))
	if err != nil {
		t.Fatal(err)
	}
	full := compose.Assemble(outer, pgpmime.WrapSigned(inner, sig))

	pm, err := mailmime.Parse(full)
	if err != nil {
		t.Fatal(err)
	}
	if !pm.Signed {
		t.Fatal("multipart/signed nicht erkannt")
	}
	if !bytes.Equal(pm.SignedPart, inner) {
		t.Fatalf("SignedPart nicht byte-identisch (len got=%d want=%d)", len(pm.SignedPart), len(inner))
	}
	if res := pgp.Verify(pm.SignedPart, pm.Signature, alice.PublicEntities()); res.Status != pgp.SigValid {
		t.Fatalf("signatur ungültig: %+v", res)
	}
	// Anzeige-Inhalt intakt (Text + Anhang).
	if !strings.Contains(pm.TextPlain, "Vertrauliche Grüße") || len(pm.Attachments) != 1 {
		t.Errorf("anzeige: text=%q atts=%d", pm.TextPlain, len(pm.Attachments))
	}
	// Manipulation im übertragenen Teil → ungültig.
	tampered := bytes.Replace(full, []byte("Vertrauliche"), []byte("Oeffentliche"), 1)
	pm2, _ := mailmime.Parse(tampered)
	if res := pgp.Verify(pm2.SignedPart, pm2.Signature, alice.PublicEntities()); res.Status != pgp.SigInvalid {
		t.Errorf("manipulation nicht erkannt: %+v", res)
	}
}

// TestEncryptedRoundtrip: Seal → Parse → Decrypt → Re-Parse, inkl.
// encrypt-to-self und eingebetteter Signatur.
func TestEncryptedSignedRoundtrip(t *testing.T) {
	alice := testKeyring(t, "Alice", "alice@uucpmail.de", "alicepw")
	bob := testKeyring(t, "Bob", "bob@example.com", "bobpw")
	bobPub, _ := bob.ExportPublic(bob.ListPrivate()[0].Fingerprint)
	if _, err := alice.ImportPublic(bobPub); err != nil {
		t.Fatal(err)
	}

	cache := pgp.NewCache(time.Minute)
	unlocked, err := pgp.Unlock(alice.FindPrivateByEmail("alice@uucpmail.de"), "alicepw")
	if err != nil {
		t.Fatal(err)
	}
	cache.Put(alice.ListPrivate()[0].Fingerprint, unlocked)
	sealer := &pgp.MailSealer{Keyring: alice, Cache: cache}

	msg := testMessage(true, true)
	outer, inner := msg.BuildParts("test", false)
	full, err := sealer.Seal(msg, outer, inner)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(full, []byte("Vertrauliche")) {
		t.Fatal("klartext in der verschlüsselten Mail sichtbar")
	}

	pm, err := mailmime.Parse(full)
	if err != nil {
		t.Fatal(err)
	}
	if !pm.Encrypted || len(pm.EncryptedPayload) == 0 {
		t.Fatal("multipart/encrypted nicht erkannt")
	}

	// Bob entschlüsselt (Alices Pubkey für die Signaturprüfung importieren).
	alicePub, _ := alice.ExportPublic(alice.ListPrivate()[0].Fingerprint)
	bob.ImportPublic(alicePub)
	bobUnlocked, err := pgp.Unlock(bob.FindPrivateByEmail("bob@example.com"), "bobpw")
	if err != nil {
		t.Fatal(err)
	}
	innerGot, sig, err := pgp.Decrypt(pm.EncryptedPayload, pgp.EntityList{bobUnlocked}, bob.PublicEntities())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(innerGot, inner) {
		t.Error("inneres Entity nach Decrypt verändert")
	}
	if sig.Status != pgp.SigValid || sig.SignerEmail != "alice@uucpmail.de" {
		t.Errorf("eingebettete Signatur: %+v", sig)
	}
	// Entschlüsseltes Entity durch dieselbe Parse-Pipeline.
	pmInner, err := mailmime.Parse(append([]byte("From: x@y.z\r\nMIME-Version: 1.0\r\n"), innerGot...))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pmInner.TextPlain, "Vertrauliche Grüße") || len(pmInner.Attachments) != 1 {
		t.Errorf("innerer inhalt: text=%q atts=%d", pmInner.TextPlain, len(pmInner.Attachments))
	}
	if !bytes.Equal(pmInner.Attachments[0].Data, msg.Attachments[0].Data) {
		t.Error("anhang-bytes verändert")
	}

	// Encrypt-to-self: Alice liest ihre Sent-Kopie.
	if _, _, err := pgp.Decrypt(pm.EncryptedPayload, pgp.EntityList{unlocked}, alice.PublicEntities()); err != nil {
		t.Errorf("encrypt-to-self: %v", err)
	}
}

func TestSealerErrors(t *testing.T) {
	alice := testKeyring(t, "Alice", "alice@uucpmail.de", "pw")
	sealer := &pgp.MailSealer{Keyring: alice, Cache: pgp.NewCache(time.Minute)}

	// Fehlender Empfänger-Key.
	msg := testMessage(false, true)
	outer, inner := msg.BuildParts("test", false)
	_, err := sealer.Seal(msg, outer, inner)
	var missing *pgp.ErrMissingKeys
	if !asErr(err, &missing) || missing.Addrs[0] != "bob@example.com" {
		t.Fatalf("err = %v", err)
	}

	// Signieren ohne entsperrten Key → ErrKeyLocked.
	msg2 := testMessage(true, false)
	o2, i2 := msg2.BuildParts("test", false)
	_, err = sealer.Seal(msg2, o2, i2)
	var locked *pgp.ErrKeyLocked
	if !asErr(err, &locked) {
		t.Fatalf("err = %v, erwartet ErrKeyLocked", err)
	}

	// Unbekannte Identität → ErrNoOwnKey.
	msg3 := testMessage(true, false)
	msg3.Identity = "fremd@uucpmail.de"
	o3, i3 := msg3.BuildParts("test", false)
	_, err = sealer.Seal(msg3, o3, i3)
	var noKey *pgp.ErrNoOwnKey
	if !asErr(err, &noKey) {
		t.Fatalf("err = %v, erwartet ErrNoOwnKey", err)
	}
}

func asErr[T error](err error, target *T) bool {
	for err != nil {
		if e, ok := err.(T); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestDraftPGPFlagsHeader(t *testing.T) {
	msg := testMessage(true, true)
	draft := msg.BuildDraft("test")
	if !bytes.Contains(draft, []byte("X-Uucpmail-PGP: sign,encrypt")) {
		t.Error("PGP-Flags nicht im Entwurf")
	}
	sent := msg.Build("test")
	if bytes.Contains(sent, []byte("X-Uucpmail-PGP")) {
		t.Error("privater PGP-Header im Versand-Build")
	}
}

// TestSignedSurvivesGatewayLFNormalization simuliert den Serverweg
// (rmail/Postfix wandeln CRLF→LF): Struktur muss erkannt und die
// Text-Mode-Signatur weiterhin als gültig geprüft werden.
func TestSignedSurvivesGatewayLFNormalization(t *testing.T) {
	alice := testKeyring(t, "Alice", "alice@uucpmail.de", "pw")
	signer, err := pgp.Unlock(alice.FindPrivateByEmail("alice@uucpmail.de"), "pw")
	if err != nil {
		t.Fatal(err)
	}
	msg := testMessage(true, false)
	msg.Attachments = nil // Textmail (Anhänge sind base64 und damit LF-fest)
	outer, inner := msg.BuildParts("test", false)
	sig, err := pgp.SignDetached(signer, inner)
	if err != nil {
		t.Fatal(err)
	}
	full := compose.Assemble(outer, pgpmime.WrapSigned(inner, sig))

	// Gateway-Normalisierung: alle CRLF → LF.
	mangled := bytes.ReplaceAll(full, []byte("\r\n"), []byte("\n"))

	pm, err := mailmime.Parse(mangled)
	if err != nil {
		t.Fatal(err)
	}
	if !pm.Signed {
		t.Fatal("multipart/signed nach LF-Normalisierung nicht erkannt")
	}
	res := pgp.Verify(pm.SignedPart, pm.Signature, alice.PublicEntities())
	if res.Status == pgp.SigInvalid {
		res = pgp.Verify(pgp.NormalizeCRLF(pm.SignedPart), pm.Signature, alice.PublicEntities())
	}
	if res.Status != pgp.SigValid {
		t.Fatalf("signatur nach Gateway-Normalisierung ungültig: %+v", res)
	}
	if !strings.Contains(pm.TextPlain, "Vertrauliche Grüße") {
		t.Errorf("anzeige: %q", pm.TextPlain)
	}
	// Manipulation wird weiterhin erkannt.
	tampered := bytes.Replace(mangled, []byte("Vertrauliche"), []byte("Oeffentliche"), 1)
	pm2, _ := mailmime.Parse(tampered)
	r2 := pgp.Verify(pm2.SignedPart, pm2.Signature, alice.PublicEntities())
	if r2.Status == pgp.SigInvalid {
		r2 = pgp.Verify(pgp.NormalizeCRLF(pm2.SignedPart), pm2.Signature, alice.PublicEntities())
	}
	if r2.Status != pgp.SigInvalid {
		t.Errorf("manipulation nach Normalisierung nicht erkannt: %+v", r2)
	}
}
