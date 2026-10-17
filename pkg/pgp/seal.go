package pgp

import (
	"fmt"
	"strings"

	"github.com/cfluegel/uucpmail-client/pkg/compose"
	"github.com/cfluegel/uucpmail-client/pkg/pgpmime"
)

// ErrMissingKeys: für diese Empfänger fehlt ein öffentlicher Schlüssel.
type ErrMissingKeys struct{ Addrs []string }

func (e *ErrMissingKeys) Error() string {
	return "für folgende Empfänger fehlt ein öffentlicher PGP-Schlüssel: " + strings.Join(e.Addrs, ", ")
}

// ErrKeyLocked: der eigene Schlüssel ist nicht entsperrt (UI muss vorab die
// Passphrase abfragen und den Cache füllen).
type ErrKeyLocked struct{ Email, Fingerprint string }

func (e *ErrKeyLocked) Error() string {
	return fmt.Sprintf("privater PGP-Schlüssel für %s ist gesperrt", e.Email)
}

// ErrNoOwnKey: keine eigene Identität mit privatem Schlüssel.
type ErrNoOwnKey struct{ Email string }

func (e *ErrNoOwnKey) Error() string {
	return fmt.Sprintf("kein eigener PGP-Schlüssel für %s vorhanden", e.Email)
}

// MailSealer implementiert das Sealer-Interface der Sync-Engine: er macht aus
// dem inneren MIME-Entity je nach Flags ein multipart/signed bzw.
// multipart/encrypted (kombinierte One-Pass-Message bei beidem) und setzt die
// Nachricht mit den äußeren Headern wieder zusammen.
type MailSealer struct {
	Keyring *Keyring
	Cache   *Cache
}

// Signer liefert den entsperrten eigenen Schlüssel für die Identität –
// aus dem Cache oder direkt, wenn der Schlüssel keine Passphrase hat.
func (s *MailSealer) Signer(identity string) (*Entity, error) {
	own := s.Keyring.FindPrivateByEmail(identity)
	if own == nil {
		return nil, &ErrNoOwnKey{Email: identity}
	}
	if !NeedsPassphrase(own) {
		return own, nil
	}
	info := infoOf(own, true)
	if unlocked := s.Cache.Get(info.Fingerprint); unlocked != nil {
		return unlocked, nil
	}
	return nil, &ErrKeyLocked{Email: info.Email, Fingerprint: info.Fingerprint}
}

// Seal verarbeitet das innere Entity gemäß msg.Sign/msg.Encrypt.
func (s *MailSealer) Seal(msg *compose.Message, outerHdr, inner []byte) ([]byte, error) {
	var signer *Entity
	if msg.Sign {
		var err error
		if signer, err = s.Signer(msg.Identity); err != nil {
			return nil, err
		}
	}

	if msg.Encrypt {
		rcpts := msg.EnvelopeRecipients()
		if missing := s.Keyring.MissingRecipients(rcpts); len(missing) > 0 {
			return nil, &ErrMissingKeys{Addrs: missing}
		}
		var entities []*Entity
		seen := map[string]bool{}
		for _, r := range rcpts {
			e := s.Keyring.FindPublicByEmail(r)
			if !seen[fpr(e)] {
				seen[fpr(e)] = true
				entities = append(entities, e)
			}
		}
		// Encrypt-to-self: eigene Kopie (Gesendet) bleibt lesbar.
		if own := s.Keyring.FindPublicByEmail(msg.Identity); own != nil && !seen[fpr(own)] {
			entities = append(entities, own)
		}
		armored, err := Encrypt(inner, entities, signer)
		if err != nil {
			return nil, err
		}
		return compose.Assemble(outerHdr, pgpmime.WrapEncrypted(armored)), nil
	}

	// Nur signiert: abgetrennte Signatur über die exakten inner-Bytes.
	sig, err := SignDetached(signer, pgpmime.SignedPayload(inner))
	if err != nil {
		return nil, err
	}
	return compose.Assemble(outerHdr, pgpmime.WrapSigned(inner, sig)), nil
}
