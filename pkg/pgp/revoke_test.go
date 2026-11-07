package pgp

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

func TestRevocationCertificate(t *testing.T) {
	k := newTestKeyring(t)
	info, err := k.Generate("Rev Test", []string{"rev@uucpmail.de"}, "pw", 0)
	if err != nil {
		t.Fatal(err)
	}
	locked := k.FindPrivateByFingerprint(info.Fingerprint)
	if _, err := RevocationCertificate(locked, packet.KeyCompromised, "x"); err == nil {
		t.Error("gesperrter Schlüssel: Fehler erwartet")
	}
	ent, err := Unlock(locked, "pw")
	if err != nil {
		t.Fatal(err)
	}
	cert, err := RevocationCertificate(ent, packet.KeyCompromised, "Laptop gestohlen")
	if err != nil {
		t.Fatal(err)
	}
	if ent.Revoked(time.Now()) || len(ent.Revocations) != 0 {
		t.Error("Original-Entity darf nicht widerrufen sein")
	}

	block, err := armor.Decode(bytes.NewReader(cert))
	if err != nil {
		t.Fatal(err)
	}
	if block.Type != "PGP PUBLIC KEY BLOCK" || block.Header["Comment"] != "This is a revocation certificate" {
		t.Errorf("Armor: %q %v", block.Type, block.Header)
	}
	rd := packet.NewReader(block.Body)
	p, err := rd.Next()
	if err != nil {
		t.Fatal(err)
	}
	sig, ok := p.(*packet.Signature)
	if !ok || sig.SigType != packet.SigTypeKeyRevocation {
		t.Fatalf("Paket: %T %+v", p, p)
	}
	if sig.RevocationReason == nil || *sig.RevocationReason != packet.KeyCompromised ||
		sig.RevocationReasonText != "Laptop gestohlen" {
		t.Errorf("Grund: %v %q", sig.RevocationReason, sig.RevocationReasonText)
	}
	if err := ent.PrimaryKey.VerifyRevocationSignature(sig); err != nil {
		t.Errorf("Signatur: %v", err)
	}
	if _, err := rd.Next(); err == nil {
		t.Error("genau ein Paket erwartet")
	}

	// Für die manuelle Gegenprobe mit GnuPG.
	if dir := os.Getenv("REVOKE_DUMP"); dir != "" {
		pub, _ := k.ExportPublic(info.Fingerprint)
		_ = os.WriteFile(filepath.Join(dir, "pub.asc"), pub, 0o600)
		_ = os.WriteFile(filepath.Join(dir, "rev.asc"), cert, 0o600)
	}
}
