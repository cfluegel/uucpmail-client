package pgp

import (
	"fmt"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// AlgoName benennt den Algorithmus eines (Unter-)Schlüssels, angelehnt an
// GnuPG: „RSA 4096“, „DSA 1024“, „ElGamal 2048“, „Ed25519“, „ECDSA nistp256“,
// „ECDH cv25519“, „X25519“, Post-Quanten-Verfahren nach RFC-Entwurf.
func AlgoName(pk *packet.PublicKey) string {
	withBits := func(name string) string {
		if bits, err := pk.BitLength(); err == nil {
			return fmt.Sprintf("%s %d", name, bits)
		}
		return name
	}
	switch pk.PubKeyAlgo {
	case packet.PubKeyAlgoRSA, packet.PubKeyAlgoRSASignOnly, packet.PubKeyAlgoRSAEncryptOnly:
		return withBits("RSA")
	case packet.PubKeyAlgoDSA:
		return withBits("DSA")
	case packet.PubKeyAlgoElGamal:
		return withBits("ElGamal")
	case packet.PubKeyAlgoEd25519:
		return "Ed25519"
	case packet.PubKeyAlgoEd448:
		return "Ed448"
	case packet.PubKeyAlgoX25519:
		return "X25519"
	case packet.PubKeyAlgoX448:
		return "X448"
	case packet.PubKeyAlgoEdDSA:
		if c, err := pk.Curve(); err == nil && c == packet.Curve448 {
			return "Ed448"
		}
		return "Ed25519"
	case packet.PubKeyAlgoECDSA:
		return "ECDSA " + curveName(pk, "")
	case packet.PubKeyAlgoECDH:
		return "ECDH " + curveName(pk, "cv")
	case packet.PubKeyAlgoMldsa65Ed25519:
		return "ML-DSA-65+Ed25519"
	case packet.PubKeyAlgoMldsa87Ed448:
		return "ML-DSA-87+Ed448"
	case packet.PubKeyAlgoSlhdsaShake128s:
		return "SLH-DSA-SHAKE-128s"
	case packet.PubKeyAlgoSlhdsaShake128f:
		return "SLH-DSA-SHAKE-128f"
	case packet.PubKeyAlgoSlhdsaShake256s:
		return "SLH-DSA-SHAKE-256s"
	case packet.PubKeyAlgoMlkem768X25519:
		return "ML-KEM-768+X25519"
	case packet.PubKeyAlgoMlkem1024X448:
		return "ML-KEM-1024+X448"
	}
	return fmt.Sprintf("algo %d", pk.PubKeyAlgo)
}

// curveName liefert den GnuPG-Kurvennamen (nistp256, brainpoolP384r1,
// secp256k1, cv25519 …); prefix25519 ist das Präfix für Curve25519/448
// (ECDH: „cv“).
func curveName(pk *packet.PublicKey, prefix25519 string) string {
	c, err := pk.Curve()
	if err != nil {
		return "?"
	}
	switch c {
	case packet.Curve25519:
		return prefix25519 + "25519"
	case packet.Curve448:
		return prefix25519 + "448"
	case packet.CurveNistP256, packet.CurveNistP384, packet.CurveNistP521:
		return "nist" + strings.ToLower(string(c))
	case packet.CurveSecP256k1:
		return "secp256k1"
	case packet.CurveBrainpoolP256, packet.CurveBrainpoolP384, packet.CurveBrainpoolP512:
		return "brainpool" + strings.TrimPrefix(string(c), "Brainpool") + "r1"
	}
	return string(c)
}

// KeyAlgo benennt den Algorithmus des Primärschlüssels ("" = unbekannt).
func (k *Keyring) KeyAlgo(fingerprint string) string {
	e := k.findByFingerprint(k.public, fingerprint)
	if e == nil {
		return ""
	}
	return AlgoName(e.PrimaryKey)
}

// KeyTypes fasst Primär- und Unterschlüssel zusammen wie GnuPG, z. B.
// „Ed25519 / ECDH cv25519“ oder „DSA 1024 / ElGamal 2048“ (doppelte
// Typen nur einmal).
func (k *Keyring) KeyTypes(fingerprint string) string {
	e := k.findByFingerprint(k.public, fingerprint)
	if e == nil {
		return ""
	}
	names := []string{AlgoName(e.PrimaryKey)}
	seen := map[string]bool{names[0]: true}
	for _, sk := range e.Subkeys {
		if n := AlgoName(sk.PublicKey); !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	return strings.Join(names, " / ")
}

// KeyPart beschreibt Haupt- oder Unterschlüssel für die Info-Anzeige.
type KeyPart struct {
	Primary     bool
	Algo        string // AlgoName
	Fingerprint string
	Usage       string // GnuPG-Kürzel aus den Key-Flags (S, C, E, A); leer = keine Flags
	Created     time.Time
	Expiry      time.Time // Zero-Wert = läuft nicht ab
	Expired     bool
	Revoked     bool
}

// KeyParts liefert Haupt- und Unterschlüssel eines Pubring-Schlüssels
// (Hauptschlüssel zuerst). Die Verwendung kommt ausschließlich aus den
// Key-Flags der Selbstsignatur – fehlen sie, bleibt Usage leer (so
// sieht man, warum go-crypto einen Unterschlüssel nicht verwendet).
func (k *Keyring) KeyParts(fingerprint string) []KeyPart {
	e := k.findByFingerprint(k.public, fingerprint)
	if e == nil {
		return nil
	}
	now := time.Now()
	part := func(pk *packet.PublicKey, sig *packet.Signature, revoked bool) KeyPart {
		p := KeyPart{Algo: AlgoName(pk), Fingerprint: strings.ToUpper(fmt.Sprintf("%x", pk.Fingerprint)),
			Created: pk.CreationTime, Revoked: revoked}
		if sig == nil {
			return p
		}
		if sig.KeyLifetimeSecs != nil && *sig.KeyLifetimeSecs > 0 {
			p.Expiry = pk.CreationTime.Add(time.Duration(*sig.KeyLifetimeSecs) * time.Second)
			p.Expired = now.After(p.Expiry)
		}
		if sig.FlagsValid {
			for _, f := range []struct {
				on bool
				c  string
			}{{sig.FlagSign, "S"}, {sig.FlagCertify, "C"},
				{sig.FlagEncryptCommunications || sig.FlagEncryptStorage, "E"}, {sig.FlagAuthenticate, "A"}} {
				if f.on {
					p.Usage += f.c
				}
			}
		}
		return p
	}
	selfSig, _ := e.PrimarySelfSignature()
	primary := part(e.PrimaryKey, selfSig, e.Revoked(now))
	primary.Primary = true
	parts := []KeyPart{primary}
	for _, sk := range e.Subkeys {
		parts = append(parts, part(sk.PublicKey, sk.Sig, sk.Revoked(now)))
	}
	return parts
}
