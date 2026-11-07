package pgp

import (
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

func TestAlgoNames(t *testing.T) {
	cases := []struct {
		cfg  packet.Config
		want string // KeyTypes-Zusammenfassung
	}{
		{packet.Config{Algorithm: packet.PubKeyAlgoRSA, RSABits: 2048}, "RSA 2048"},
		{packet.Config{Algorithm: packet.PubKeyAlgoEdDSA, Curve: packet.Curve25519}, "Ed25519 / ECDH cv25519"},
		{packet.Config{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP256}, "ECDSA nistp256 / ECDH nistp256"},
		{packet.Config{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveBrainpoolP384}, "ECDSA brainpoolP384r1 / ECDH brainpoolP384r1"},
		{packet.Config{Algorithm: packet.PubKeyAlgoEd25519, V6Keys: true}, "Ed25519 / X25519"},
		{packet.Config{Algorithm: packet.PubKeyAlgoEd448, V6Keys: true}, "Ed448 / X448"},
	}
	for _, c := range cases {
		cfg := c.cfg
		e, err := openpgp.NewEntity("T", "", "t@example.org", &cfg)
		if err != nil {
			t.Fatalf("%s: %v", c.want, err)
		}
		k := newTestKeyring(t)
		k.public = append(k.public, e)
		if got := k.KeyTypes(fpr(e)); got != c.want {
			t.Errorf("KeyTypes = %q, erwartet %q", got, c.want)
		}
	}
	// Nur Unterschlüssel-Algorithmen, direkt benannt.
	if n := AlgoName(&packet.PublicKey{PubKeyAlgo: packet.PubKeyAlgoX448}); n != "X448" {
		t.Errorf("X448: %q", n)
	}
	if n := AlgoName(&packet.PublicKey{PubKeyAlgo: 99}); n != "algo 99" {
		t.Errorf("unbekannt: %q", n)
	}
}
