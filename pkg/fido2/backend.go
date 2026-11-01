package fido2

import (
	"context"
	"errors"
)

// Backend abstrahiert den Token-Zugriff. clientData sind die ROHEN zu
// signierenden Daten (die SSH-Session-Daten): das Unix-Backend hasht sie
// selbst (clientDataHash = SHA256), das Windows-Backend reicht sie an
// webauthn.dll durch, die ihrerseits hasht – beides ergibt denselben
// clientDataHash, den der Authenticator mitsigniert.
type Backend interface {
	// GetAssertion signiert über das Token. credID leer = leere AllowList
	// (Resident-Key-Discovery). Es dürfen KEINE Extensions angefordert
	// werden – authData muss exakt 37 Bytes bleiben.
	GetAssertion(ctx context.Context, rpID string, clientData, credID []byte, prompt Prompter) (*Assertion, error)
	// MakeResidentCredential erzeugt einen discoverable Credential
	// (rk=true) für rpID; Algorithmus-Präferenz EdDSA, Fallback ES256.
	MakeResidentCredential(ctx context.Context, rpID, userName string, prompt Prompter) (*Stub, error)
	// EnumerateSSHCredentials listet vorhandene Resident Keys für rpID
	// (credentialManagement; unter Windows ErrNotSupported).
	EnumerateSSHCredentials(ctx context.Context, rpID string, prompt Prompter) ([]Stub, error)
}

// Assertion ist das Ergebnis von GetAssertion.
type Assertion struct {
	AuthData     []byte // SHA256(rpID) || flags || counter – genau 37 Bytes
	Signature    []byte // ed25519: 64 Bytes roh; ecdsa: ASN.1/DER
	CredentialID []byte
}

// Prompter liefert Nutzerinteraktion während einer Token-Operation.
// Die PIN wird bewusst NIE zwischengespeichert.
type Prompter interface {
	RequestPIN(ctx context.Context) (string, error)
	NotifyTouch() // "Security-Key berühren …" anzeigen
	TouchDone()   // Hinweis wieder entfernen
}

var (
	ErrNoDevice       = errors.New("kein FIDO2-Security-Key gefunden")
	ErrPINWrong       = errors.New("falsche PIN")
	ErrNotSupported   = errors.New("auf dieser Plattform nicht unterstützt")
	ErrNoCredentials  = errors.New("kein passender Schlüssel auf dem Token")
	ErrNoUserPresence = errors.New("token hat die Berührung nicht bestätigt")
)

// Flag-Bits im authData (WebAuthn/CTAP2).
const (
	flagUserPresent  = 0x01
	flagUserVerified = 0x04
)

// DefaultBackend liefert das Plattform-Backend.
func DefaultBackend() (Backend, error) { return platformBackend() }
