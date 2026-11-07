package pgp

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
)

// wotPair erzeugt "mich" (A, mit Passphrase) und einen Partner (B) mit zwei
// UIDs; A kennt B's öffentlichen Schlüssel.
func wotPair(t *testing.T) (a, b *Keyring, aFpr, bFpr string) {
	t.Helper()
	a = newTestKeyring(t)
	ai, err := a.Generate("Alice", []string{"alice@uucpmail.de"}, "pw", 0)
	if err != nil {
		t.Fatal(err)
	}
	b = newTestKeyring(t)
	bi, err := b.Generate("Bob", []string{"bob@example.com", "bob@work.example"}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := b.ExportPublic(bi.Fingerprint)
	if _, err := a.ImportPublic(pub); err != nil {
		t.Fatal(err)
	}
	return a, b, ai.Fingerprint, bi.Fingerprint
}

func unlockedOwn(t *testing.T, k *Keyring, fp, pass string) *Entity {
	t.Helper()
	ent, err := Unlock(k.FindPrivateByFingerprint(fp), pass)
	if err != nil {
		t.Fatal(err)
	}
	return ent
}

func TestCertifyAndList(t *testing.T) {
	a, _, aFpr, bFpr := wotPair(t)
	uids := a.UIDs(bFpr)
	if len(uids) != 2 {
		t.Fatalf("UIDs: %v", uids)
	}
	if a.IsVerified(bFpr) {
		t.Fatal("vor dem Zertifizieren schon geprüft")
	}
	if err := a.Certify(bFpr, uids[:1], unlockedOwn(t, a, aFpr, "pw")); err != nil {
		t.Fatal(err)
	}
	certs := a.Certifications(bFpr)
	if len(certs) != 1 {
		t.Fatalf("Zertifizierungen: %+v", certs)
	}
	c := certs[0]
	if c.UID != uids[0] || !c.Mine || c.Status != CertValid || c.SignerFpr != aFpr || c.Revoked || c.SignerRef != aFpr {
		t.Fatalf("Zertifizierung falsch: %+v", c)
	}
	if !a.IsVerified(bFpr) || !a.ListPublic()[0].Verified {
		t.Fatal("zertifizierter Schlüssel nicht als geprüft markiert")
	}
	// Persistenz: nach Neuladen weiterhin da.
	re, err := LoadKeyring(a.pubPath, a.secPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := re.Certifications(bFpr); len(got) != 1 || got[0].Status != CertValid {
		t.Fatalf("nach Reload: %+v", got)
	}
}

func TestCertifyRejects(t *testing.T) {
	a, _, aFpr, bFpr := wotPair(t)
	signer := unlockedOwn(t, a, aFpr, "pw")
	if err := a.Certify(aFpr, a.UIDs(aFpr), signer); !errors.Is(err, errWoTOwnKey) {
		t.Errorf("eigener Schlüssel: %v", err)
	}
	if err := a.Certify(bFpr, nil, signer); !errors.Is(err, errWoTNoUID) {
		t.Errorf("ohne UID: %v", err)
	}
	if err := a.Certify(bFpr, []string{"Gibt Es Nicht <x@y>"}, signer); !errors.Is(err, errWoTNoUID) {
		t.Errorf("unbekannte UID: %v", err)
	}
	if err := a.Certify(bFpr, a.UIDs(bFpr), a.FindPrivateByFingerprint(aFpr)); !errors.Is(err, errWoTLocked) {
		t.Errorf("gesperrter Signierer: %v", err)
	}
}

func TestExportForUIDAndMergeBack(t *testing.T) {
	a, b, aFpr, bFpr := wotPair(t)
	uids := a.UIDs(bFpr)
	if err := a.Certify(bFpr, uids, unlockedOwn(t, a, aFpr, "pw")); err != nil {
		t.Fatal(err)
	}
	exp, err := a.ExportForUID(bFpr, uids[0], aFpr)
	if err != nil {
		t.Fatal(err)
	}
	el, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(exp))
	if err != nil || len(el) != 1 {
		t.Fatalf("Export nicht lesbar: %v", err)
	}
	if n := len(el[0].Identities); n != 1 {
		t.Fatalf("Export enthält %d UIDs, erwartet 1", n)
	}
	if _, ok := el[0].Identities[uids[0]]; !ok {
		t.Fatal("falsche UID exportiert")
	}

	// B importiert die Rücksendung: Merge auf den EIGENEN Schlüssel.
	rep, err := b.ImportPublicReport(exp)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Merged != 1 || !rep.MergedOwn || len(rep.Keys) != 0 {
		t.Fatalf("Merge-Report: %+v", rep)
	}
	// Signierer A unbekannt → angezeigt, aber ungeprüft.
	if c := b.Certifications(bFpr); len(c) != 1 || c[0].Status != CertUnknown || c[0].UID != uids[0] {
		t.Fatalf("vor A-Import: %+v", c)
	}
	// Zweiter Import: nichts Neues.
	if _, err := b.ImportPublicReport(exp); !errors.Is(err, ErrNoNewKeys) {
		t.Fatalf("Merge nicht idempotent: %v", err)
	}
	// A's Schlüssel bekannt → Zertifizierung gültig.
	apub, _ := a.ExportPublic(aFpr)
	if _, err := b.ImportPublic(apub); err != nil {
		t.Fatal(err)
	}
	if c := b.Certifications(bFpr); len(c) != 1 || c[0].Status != CertValid || c[0].SignerFpr != aFpr {
		t.Fatalf("nach A-Import: %+v", c)
	}
	// Weitergegebener eigener Public-Export trägt die Zertifizierung.
	bpub, _ := b.ExportPublic(bFpr)
	c2 := newTestKeyring(t)
	c2.ImportPublic(apub)
	c2.ImportPublic(bpub)
	if c := c2.Certifications(bFpr); len(c) != 1 || c[0].Status != CertValid {
		t.Fatalf("Weiterverteilung: %+v", c)
	}
}

func TestForgedCertificationRejected(t *testing.T) {
	a, b, aFpr, bFpr := wotPair(t)
	uids := a.UIDs(bFpr)
	if err := a.Certify(bFpr, uids[:1], unlockedOwn(t, a, aFpr, "pw")); err != nil {
		t.Fatal(err)
	}
	// Zertifizierung von UID 1 auf UID 2 „umhängen" (Fälschungsversuch).
	ent := a.findByFingerprint(a.public, bFpr)
	var cert = ent.Identities[uids[0]].Signatures
	forged := cert[len(cert)-1]
	ent.Identities[uids[1]].Signatures = append(ent.Identities[uids[1]].Signatures, forged)
	var invalid int
	for _, c := range a.Certifications(bFpr) {
		if c.UID == uids[1] && c.Status == CertInvalid {
			invalid++
		}
	}
	if invalid != 1 {
		t.Fatalf("Fälschung nicht als ungültig erkannt: %+v", a.Certifications(bFpr))
	}
	// Beim Merge (Signierer bekannt) wird die Fälschung verworfen.
	apub, _ := a.ExportPublic(aFpr)
	b.ImportPublic(apub)
	var buf bytes.Buffer
	cp := *ent
	cp.Identities = map[string]*openpgp.Identity{uids[1]: ent.Identities[uids[1]]}
	if err := cp.Serialize(&buf); err != nil {
		t.Fatal(err)
	}
	el, err := openpgp.ReadKeyRing(&buf)
	if err != nil {
		t.Fatal(err)
	}
	own := b.findByFingerprint(b.public, bFpr)
	if n := b.mergeCerts(own, el[0]); n != 0 {
		t.Fatalf("gefälschte Zertifizierung übernommen (%d)", n)
	}
}

// Alte „nur lokal“-Vermerke gelten nicht mehr als geprüft.
func TestVerifiedIgnoresLocalMarks(t *testing.T) {
	a, _, _, bFpr := wotPair(t)
	path := filepath.Join(t.TempDir(), "pgp_verified.json")
	old := `{"` + bFpr + `":{"verified_at":"2026-11-01T10:00:00Z","method":"local"}}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadVerified(path); err != nil {
		t.Fatal(err)
	}
	if a.IsVerified(bFpr) || a.ListPublic()[0].Verified {
		t.Fatal("lokaler Vermerk darf nicht als geprüft zählen")
	}
}

func TestPrimaryUID(t *testing.T) {
	a, _, _, bFpr := wotPair(t)
	if got := a.PrimaryUID(bFpr); got != "Bob <bob@example.com>" {
		t.Errorf("PrimaryUID = %q", got)
	}
	if got := a.PrimaryUID("00"); got != "" {
		t.Errorf("unbekannt: %q", got)
	}
}

// Eigener, unveränderter Schlüssel → ErrNoNewKeys mit OnlyOwn (eindeutige
// Meldung in der UI); fremder bekannter Schlüssel → ohne OnlyOwn.
func TestImportOwnKeyUnchanged(t *testing.T) {
	a, _, aFpr, bFpr := wotPair(t)
	own, _ := a.ExportPublic(aFpr)
	rep, err := a.ImportPublicReport(own)
	if !errors.Is(err, ErrNoNewKeys) || !rep.OnlyOwn {
		t.Fatalf("eigener Key: err=%v rep=%+v", err, rep)
	}
	other, _ := a.ExportPublic(bFpr)
	rep, err = a.ImportPublicReport(other)
	if !errors.Is(err, ErrNoNewKeys) || rep.OnlyOwn {
		t.Fatalf("fremder Key: err=%v rep=%+v", err, rep)
	}
}

func TestKeyParts(t *testing.T) {
	k := newTestKeyring(t)
	info, err := k.Generate("Parts", []string{"parts@uucpmail.de"}, "pw", 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	parts := k.KeyParts(info.Fingerprint)
	if len(parts) != 2 || !parts[0].Primary || parts[1].Primary {
		t.Fatalf("Teile: %+v", parts)
	}
	if parts[0].Fingerprint != info.Fingerprint || !strings.Contains(parts[0].Usage, "C") ||
		!strings.Contains(parts[1].Usage, "E") {
		t.Errorf("Haupt/Unter: %+v", parts)
	}
	if parts[0].Expiry.IsZero() || parts[0].Expired || parts[0].Revoked {
		t.Errorf("Ablauf: %+v", parts[0])
	}
	if k.KeyParts("00") != nil {
		t.Error("unbekannt: nil erwartet")
	}
}
