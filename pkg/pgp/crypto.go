package pgp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
	pgperrors "github.com/ProtonMail/go-crypto/openpgp/errors"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// SigStatus ist der Signaturstatus einer Nachricht (kein Trust-Modell in V1).
type SigStatus int

const (
	SigNone       SigStatus = iota // unsigniert
	SigValid                       // gültig, Schlüssel bekannt
	SigInvalid                     // Signatur falsch / Daten manipuliert
	SigUnknownKey                  // signiert, Schlüssel nicht im Keyring
)

// SigResult beschreibt das Prüfergebnis samt Unterzeichner (falls bekannt).
type SigResult struct {
	Status            SigStatus
	SignerEmail       string
	SignerFingerprint string
}

// ErrWrongPassphrase signalisiert eine falsche Passphrase (UI: erneut fragen).
var ErrWrongPassphrase = errors.New("falsche Passphrase")

// ErrNoPrivateKey: verschlüsselte Nachricht, aber kein passender eigener Schlüssel.
var ErrNoPrivateKey = errors.New("kein passender privater Schlüssel für diese Nachricht")

// Unlock liefert eine entsperrte Deep-Copy des Entity (Original bleibt
// verschlüsselt im Keyring). Primärschlüssel UND alle Subkeys werden entsperrt.
func Unlock(e *openpgp.Entity, passphrase string) (*openpgp.Entity, error) {
	var buf bytes.Buffer
	if err := e.SerializePrivateWithoutSigning(&buf, nil); err != nil {
		return nil, err
	}
	el, err := openpgp.ReadKeyRing(&buf)
	if err != nil || len(el) == 0 {
		return nil, fmt.Errorf("schlüsselkopie fehlgeschlagen: %w", err)
	}
	cp := el[0]
	pw := []byte(passphrase)
	if cp.PrivateKey != nil && cp.PrivateKey.Encrypted {
		if err := cp.PrivateKey.Decrypt(pw); err != nil {
			return nil, ErrWrongPassphrase
		}
	}
	for _, sk := range cp.Subkeys {
		if sk.PrivateKey != nil && sk.PrivateKey.Encrypted {
			if err := sk.PrivateKey.Decrypt(pw); err != nil {
				return nil, ErrWrongPassphrase
			}
		}
	}
	return cp, nil
}

// NeedsPassphrase meldet, ob der Schlüssel verschlüsselt gespeichert ist.
func NeedsPassphrase(e *openpgp.Entity) bool {
	return e.PrivateKey != nil && e.PrivateKey.Encrypted
}

// SignDetached erzeugt eine abgetrennte Armor-Signatur im TEXT-Modus:
// gehasht wird über kanonisierte CRLF-Zeilenenden. Das ist für PGP/MIME
// entscheidend, weil Mail-Gateways (hier: rmail/Postfix auf dem Serverweg)
// CRLF zu LF normalisieren – eine Binär-Signatur würde dadurch brechen,
// die Text-Signatur bleibt gültig (so macht es auch GnuPG für PGP/MIME).
func SignDetached(signer *openpgp.Entity, data []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := openpgp.ArmoredDetachSignText(&buf, signer, bytes.NewReader(data), nil); err != nil {
		return nil, err
	}
	buf.WriteString("\n")
	return buf.Bytes(), nil
}

// NormalizeCRLF kanonisiert Zeilenenden auf CRLF (für die Verifikation von
// Alt-Signaturen im Binary-Modus nach Gateway-Normalisierung).
func NormalizeCRLF(data []byte) []byte {
	lf := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))
}

// Encrypt verschlüsselt data an alle Empfänger (Armor). signer != nil erzeugt
// eine kombinierte signed+encrypted One-Pass-Message (RFC 3156 §6.2).
func Encrypt(data []byte, to []*openpgp.Entity, signer *openpgp.Entity) ([]byte, error) {
	var buf bytes.Buffer
	aw, err := armor.Encode(&buf, "PGP MESSAGE", nil)
	if err != nil {
		return nil, err
	}
	pw, err := openpgp.Encrypt(aw, to, signer, nil, nil)
	if err != nil {
		return nil, err
	}
	if _, err := pw.Write(data); err != nil {
		return nil, err
	}
	if err := pw.Close(); err != nil {
		return nil, err
	}
	if err := aw.Close(); err != nil {
		return nil, err
	}
	buf.WriteString("\n")
	return buf.Bytes(), nil
}

// Decrypt entschlüsselt eine Armor-Message mit bereits ENTSPERRTEN privaten
// Schlüsseln (kein PromptFunction-Callback – bewusste Entscheidung, dessen
// Retry-Semantik ist fehleranfällig). pub dient der Prüfung einer eventuell
// eingebetteten Signatur.
func Decrypt(armored []byte, priv openpgp.EntityList, pub openpgp.EntityList) ([]byte, SigResult, error) {
	block, err := armor.Decode(bytes.NewReader(armored))
	if err != nil {
		return nil, SigResult{}, fmt.Errorf("kein gültiger PGP-Block: %w", err)
	}
	ring := append(append(openpgp.EntityList{}, priv...), pub...)
	md, err := openpgp.ReadMessage(block.Body, ring, nil, nil)
	if err != nil {
		if strings.Contains(err.Error(), "private key") || strings.Contains(err.Error(), "no armored data") ||
			strings.Contains(err.Error(), "incorrect key") {
			return nil, SigResult{}, ErrNoPrivateKey
		}
		return nil, SigResult{}, err
	}
	plain, err := io.ReadAll(io.LimitReader(md.UnverifiedBody, 64<<20))
	if err != nil {
		return nil, SigResult{}, fmt.Errorf("entschlüsselung fehlgeschlagen: %w", err)
	}
	// Signaturstatus erst NACH vollständigem Lesen auswerten (go-crypto-Kontrakt).
	res := SigResult{Status: SigNone}
	if md.IsSigned {
		switch {
		case md.SignatureError != nil:
			res.Status = SigInvalid
		case md.SignedBy == nil:
			res.Status = SigUnknownKey
			res.SignerFingerprint = fmt.Sprintf("%X", md.SignedByKeyId)
		default:
			res = signerResult(md.SignedBy.Entity)
		}
	}
	return plain, res, nil
}

// DecryptionCandidates liefert die eigenen Schlüssel, an die die Nachricht
// tatsächlich verschlüsselt ist (Abgleich der EncryptedKey-Key-IDs aus dem
// Paketkopf – ohne Passphrase lesbar). Leeres Ergebnis = keiner passt.
func DecryptionCandidates(armored []byte, k *Keyring) []KeyInfo {
	block, err := armor.Decode(bytes.NewReader(armored))
	if err != nil {
		return nil
	}
	wanted := map[uint64]bool{}
	pr := packet.NewReader(block.Body)
	for {
		p, err := pr.Next()
		if err != nil {
			break
		}
		if ek, ok := p.(*packet.EncryptedKey); ok {
			wanted[ek.KeyId] = true
		}
	}
	var out []KeyInfo
	for _, e := range k.PrivateEntities() {
		match := wanted[e.PrimaryKey.KeyId]
		for _, sk := range e.Subkeys {
			if wanted[sk.PublicKey.KeyId] {
				match = true
			}
		}
		if match {
			out = append(out, infoOf(e, true))
		}
	}
	return out
}

// Verify prüft eine abgetrennte Signatur über signedData.
func Verify(signedData, armoredSig []byte, pub openpgp.EntityList) SigResult {
	signer, err := openpgp.CheckArmoredDetachedSignature(
		pub, bytes.NewReader(signedData), bytes.NewReader(armoredSig), nil)
	return verdict(signer, err)
}

// VerifyClearsigned prüft einen Clearsign-Block und liefert den Klartext.
func VerifyClearsigned(body []byte, pub openpgp.EntityList) ([]byte, SigResult) {
	block, _ := clearsign.Decode(body)
	if block == nil {
		return body, SigResult{Status: SigInvalid}
	}
	signer, err := openpgp.CheckDetachedSignature(
		pub, bytes.NewReader(block.Bytes), block.ArmoredSignature.Body, nil)
	return block.Plaintext, verdict(signer, err)
}

func verdict(signer *openpgp.Entity, err error) SigResult {
	switch {
	case err == nil && signer != nil:
		return signerResult(signer)
	case errors.Is(err, pgperrors.ErrUnknownIssuer):
		return SigResult{Status: SigUnknownKey}
	default:
		return SigResult{Status: SigInvalid}
	}
}

func signerResult(e *openpgp.Entity) SigResult {
	res := SigResult{Status: SigValid, SignerFingerprint: fpr(e)}
	for _, id := range e.Identities {
		if id.UserId != nil {
			res.SignerEmail = id.UserId.Email
			break
		}
	}
	return res
}
