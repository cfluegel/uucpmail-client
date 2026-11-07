package pgpcard

import (
	"context"
	"crypto"
	"errors"
	"io"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// CardKey bündelt alles, was für Kartenoperationen im Mailbetrieb nötig ist.
// Jede Operation öffnet eine frische Session (Karte kann jederzeit gezogen
// werden); nur die verifizierte PIN wird gecacht.
type CardKey struct {
	Backend Backend
	Stub    *Stub
	Prompt  Prompter
	Cache   *PINCache
}

// BindEntity verknüpft die öffentliche Entity aus dem Pubring mit der Karte:
// die im Stub genannten Subkeys erhalten Karten-PrivateKeys (crypto.Signer/
// crypto.Decrypter über PSO:SIGN/PSO:DECIPHER, docs/YUBIKEY.md §2). Die
// Original-Entity bleibt unverändert; zurück kommt eine flache Kopie, die
// überall dort einsetzbar ist, wo sonst eine entsperrte Entity erwartet wird.
func BindEntity(pub *openpgp.Entity, ck *CardKey) *openpgp.Entity {
	e := *pub
	e.Subkeys = append([]openpgp.Subkey(nil), pub.Subkeys...)
	for i := range e.Subkeys {
		sk := &e.Subkeys[i]
		fpr := FingerprintHex(sk.PublicKey.Fingerprint)
		switch fpr {
		case ck.Stub.SignSubkey:
			// Für RSA castet go-crypto PrivateKey.PrivateKey direkt auf
			// crypto.Signer (packet/signature.go); NewSignerPrivateKey würde
			// den Interface-Typ ablehnen – Paket manuell um den vorhandenen
			// Public Key bauen (gleiches Muster unten beim Decrypter).
			priv := &packet.PrivateKey{PublicKey: *sk.PublicKey}
			priv.PrivateKey = &cardSigner{ck: ck, pub: sk.PublicKey.PublicKey}
			priv.IsSubkey = true
			sk.PrivateKey = priv
		case ck.Stub.DecryptSubkey:
			if ck.Stub.DecryptSubkey == "" {
				continue
			}
			// NewDecrypterPrivateKey akzeptiert nur echte Schlüssel; der
			// RSA-Entschlüsselungspfad castet aber PrivateKey.PrivateKey auf
			// crypto.Decrypter (packet/encrypted_key.go) – deshalb wird das
			// Paket manuell um den vorhandenen Public Key gebaut.
			priv := &packet.PrivateKey{PublicKey: *sk.PublicKey}
			priv.PrivateKey = &cardDecrypter{ck: ck, pub: sk.PublicKey.PublicKey}
			priv.IsSubkey = true
			sk.PrivateKey = priv
		}
	}
	return &e
}

// withSession öffnet die Karte, sorgt für eine verifizierte PIN (Cache →
// sonst Prompt mit Retry-Anzeige) und führt op aus.
func (ck *CardKey) withSession(op func(Session) ([]byte, error)) ([]byte, error) {
	ctx := context.Background()
	sess, err := ck.Backend.Open(ctx, ck.Stub.Serial)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sess.Close() }()

	pin, fromCache := ck.Cache.Get(ck.Stub.Serial)
	retries := -1
	for {
		if !fromCache {
			pin, err = ck.Prompt.RequestPIN(ctx, sess.Info(), retries)
			if err != nil {
				return nil, err
			}
		}
		verr := sess.VerifyPIN(pin)
		if verr == nil {
			ck.Cache.Put(ck.Stub.Serial, pin)
			break
		}
		ck.Cache.Remove(ck.Stub.Serial)
		var pe *PINError
		if errors.As(verr, &pe) {
			fromCache = false
			retries = pe.Retries
			continue
		}
		return nil, verr
	}
	return op(sess)
}

// cardSigner: crypto.Signer über PSO:SIGN (SIG-Slot).
type cardSigner struct {
	ck  *CardKey
	pub crypto.PublicKey
}

func (s *cardSigner) Public() crypto.PublicKey { return s.pub }

func (s *cardSigner) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	return s.ck.withSession(func(sess Session) ([]byte, error) {
		signer, err := sess.Signer(s.pub)
		if err != nil {
			return nil, err
		}
		return signer.Sign(rand, digest, opts)
	})
}

// cardDecrypter: crypto.Decrypter über PSO:DECIPHER (DEC-Slot).
type cardDecrypter struct {
	ck  *CardKey
	pub crypto.PublicKey
}

func (d *cardDecrypter) Public() crypto.PublicKey { return d.pub }

func (d *cardDecrypter) Decrypt(rand io.Reader, ciphertext []byte, opts crypto.DecrypterOpts) ([]byte, error) {
	return d.ck.withSession(func(sess Session) ([]byte, error) {
		dec, err := sess.Decrypter(d.pub)
		if err != nil {
			return nil, err
		}
		return dec.Decrypt(rand, ciphertext, opts)
	})
}
