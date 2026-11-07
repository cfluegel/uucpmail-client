package pgp

import (
	"bytes"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
)

// RevocationReasons sind die Widerrufsgründe in der Reihenfolge des
// GnuPG-Menüs (gpg --gen-revoke): 0 kein Grund, 1 kompromittiert,
// 2 ersetzt, 3 nicht mehr verwendet. Die Werte sind die RFC-4880-Codes.
var RevocationReasons = []packet.ReasonForRevocation{
	packet.NoReason,
	packet.KeyCompromised,
	packet.KeySuperseded,
	packet.KeyRetired,
}

// RevocationCertificate erzeugt ein Widerrufszertifikat für den
// Hauptschlüssel (entsperrte Entity mit privatem Master) im Format von
// „gpg --gen-revoke“: nur das Revocation-Signaturpaket als Armor. Die
// Entity selbst wird nicht verändert – der Schlüssel bleibt gültig, bis
// jemand das Zertifikat importiert.
func RevocationCertificate(ent *Entity, reason packet.ReasonForRevocation, text string) ([]byte, error) {
	// go-crypto prüft das nicht selbst und gerät sonst in einen Panic.
	fpr := FingerprintOf(ent)
	if ent.PrivateKey == nil || ent.PrivateKey.Dummy() {
		return nil, i18n.Errorf("err.pgp.noownkey", fpr)
	}
	if ent.PrivateKey.Encrypted {
		return nil, i18n.Errorf("err.pgp.keylocked", fpr)
	}
	cp := *ent
	cp.Revocations = nil
	if err := cp.RevokeKey(reason, text, nil); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType,
		map[string]string{"Comment": "This is a revocation certificate"})
	if err != nil {
		return nil, err
	}
	if err := cp.Revocations[0].Serialize(w); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	buf.WriteString("\n")
	return buf.Bytes(), nil
}
