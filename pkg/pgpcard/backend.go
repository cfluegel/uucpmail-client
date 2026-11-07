// Package pgpcard spricht OpenPGP-Smartcards (Card-Spec 3.x) direkt über
// PC/SC an – ohne GnuPG. Der private Karten-Teil lebt auf der Karte; lokal
// liegt nur ein Stub (pgpcard_key.json, stub.go). Die konkrete
// Kartenbibliothek ist hinter Backend/Session gekapselt (einziger Import in
// pcsc.go) und bleibt austauschbar; Tests laufen gegen das Fake-Backend.
package pgpcard

import (
	"context"
	"crypto"
	"fmt"
	"time"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
)

// Slot bezeichnet einen Karten-Schlüsselslot.
type Slot string

const (
	SlotSign    Slot = "sign"    // SIG: Signieren
	SlotDecrypt Slot = "decrypt" // DEC: Entschlüsseln
	SlotAuth    Slot = "auth"    // AUT: Authentisierung (vom Client ungenutzt, aber belegt)
)

// Werks-PINs nach OpenPGP-Card-Spec; der Wizard erzwingt deren Änderung.
const (
	DefaultPIN      = "123456"
	DefaultAdminPIN = "12345678"
	MinPINLen       = 6
	MinAdminPINLen  = 8
)

// SlotInfo beschreibt die Belegung eines Karten-Slots.
type SlotInfo struct {
	Present     bool
	Fingerprint string // OpenPGP-v4-Fingerprint, 40 Hex-Zeichen (Großschreibung)
	Algo        string // z.B. "rsa3072", "rsa4096", "ed25519"
	Created     time.Time
}

// CardInfo beschreibt eine erkannte Karte.
type CardInfo struct {
	Serial       string
	Manufacturer string
	Reader       string
	Slots        map[Slot]SlotInfo
	// RSABits: von der Karte gemeldete RSA-Modulus-Längen (leer, wenn die
	// Karte kein Algorithm-Information-DO hat – dann unbekannt; der Wizard
	// bietet in dem Fall 3072/4096 ungefiltert an).
	RSABits []int

	Version      string   // Card-Spec-Version aus der AID, z. B. "3.4"
	Algos        []string // von der Karte angebotene Algorithmen (SIG-Slot), z. B. "rsa4096", "nistp256"
	SigCount     int      // Signaturzähler
	PINRetries   int      // verbleibende Versuche PW1
	AdminRetries int      // verbleibende Versuche PW3
	SigPINForced bool     // Signatur-PIN bei jeder Signatur (PW1-Status 00)
	MaxPINLen    int      // kleinste der max. PIN-Längen (PW1/PW3)
	KDFSupported bool     // Karte kann KDF (Spec 3.3+)
	KDFActive    bool     // KDF eingeschaltet: PINs werden gehasht übertragen
	CanReset     bool     // Werksreset möglich (Life-Cycle-Status 03/05)
}

// KDFPossible meldet, ob KDF auf dieser Karte eingeschaltet werden kann
// (unterstützt, noch aus, PIN-Felder lang genug für einen SHA-512-Hash).
func (c CardInfo) KDFPossible() bool {
	return c.KDFSupported && !c.KDFActive && c.MaxPINLen >= 64
}

// Session ist eine geöffnete Karte. Sitzungen werden pro Operation frisch
// geöffnet und sofort wieder geschlossen (die Karte kann jederzeit gezogen
// werden); gecacht wird ausschließlich die PIN (cache.go).
type Session interface {
	Info() CardInfo
	VerifyPIN(pin string) error      // PW1 (Nutzer-PIN); Fehler: *PINError / ErrPINBlocked
	VerifyAdminPIN(pin string) error // PW3 (Admin-PIN)
	// Signer/Decrypter liefern den Karten-Schlüssel des SIG- bzw. DEC-Slots.
	// pub ist der erwartete öffentliche Schlüssel (Hint für Karten, die den
	// Public Key nicht herausgeben). Vorher VerifyPIN.
	Signer(pub crypto.PublicKey) (crypto.Signer, error)
	Decrypter(pub crypto.PublicKey) (crypto.Decrypter, error)
	// ImportKey schreibt priv in den Slot (keytocard). created ist die
	// OpenPGP-Erzeugungszeit des (Sub-)Schlüssels – sie bestimmt den
	// Fingerprint, den die Karte speichert, und MUSS mit dem Schlüsselpaket
	// im Keyring übereinstimmen. Vorher VerifyAdminPIN.
	ImportKey(slot Slot, priv crypto.PrivateKey, created time.Time) error
	ChangePIN(current, next string) error      // PW1
	ChangeAdminPIN(current, next string) error // PW3
	// ResetPIN setzt mit der Admin-PIN eine neue Nutzer-PIN und den
	// Fehlbedienungszähler zurück (RESET RETRY COUNTER).
	ResetPIN(adminPIN, newPIN string) error
	// SetupKDF schaltet KDF ein (iteriertes S2K): PINs werden danach nur
	// gehasht übertragen und gespeichert. pin/adminPIN sind die aktuellen
	// PINs; sie bleiben gültig.
	SetupKDF(pin, adminPIN string) error
	// FactoryReset setzt die Karte in den Werkszustand (TERMINATE DF +
	// ACTIVATE FILE): Schlüssel gelöscht, Werks-PINs, KDF aus. Braucht
	// keine PIN – ist PW3 nicht verifiziert, wird es absichtlich gesperrt.
	FactoryReset() error
	Close() error
}

// Backend abstrahiert den Kartenzugriff (Muster pkg/fido2.Backend).
type Backend interface {
	Cards(ctx context.Context) ([]CardInfo, error)
	Open(ctx context.Context, serial string) (Session, error)
}

// Prompter liefert Nutzerinteraktion während einer Kartenoperation.
// retries < 0 bedeutet: Anzahl unbekannt (erster Versuch).
type Prompter interface {
	RequestPIN(ctx context.Context, info CardInfo, retries int) (string, error)
}

var (
	ErrNoCard       = i18n.Errorf("err.pgpcard.nocard")
	ErrCardBusy     = i18n.Errorf("err.pgpcard.busy")
	ErrPINBlocked   = i18n.Errorf("err.pgpcard.pinblocked")
	ErrNotSupported = i18n.Errorf("err.pgpcard.notsupported")
	ErrWrongCard    = i18n.Errorf("err.pgpcard.wrongcard")
	ErrNotVerified  = i18n.Errorf("err.pgpcard.notverified")
)

// PINError: falsche PIN mit Anzahl der verbleibenden Versuche.
type PINError struct{ Retries int }

func (e *PINError) Error() string {
	return fmt.Sprintf(i18n.T("err.pgpcard.pinwrong"), e.Retries)
}

// DefaultBackend liefert das PC/SC-Backend (alle Plattformen).
func DefaultBackend() Backend { return &pcscBackend{} }
