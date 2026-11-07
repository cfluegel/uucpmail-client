package pgpcard

import (
	"context"
	"crypto"
	"crypto/rsa"
	"fmt"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// FakeCard ist eine In-Memory-Karte für Tests (kein Build-Tag nötig – wird
// nur aus Tests referenziert und sonst vom Linker entfernt).
type FakeCard struct {
	Serial       string
	PIN          string
	AdminPIN     string
	PINRetries   int
	AdminRetries int
	RSABits      []int
	KDFSupported bool // Karte kann KDF
	KDFActive    bool // KDF eingeschaltet

	keys    map[Slot]*rsa.PrivateKey
	created map[Slot]time.Time
}

// NewFakeCard liefert eine leere Karte mit Werks-PINs und 3 Versuchen.
func NewFakeCard(serial string) *FakeCard {
	return &FakeCard{
		Serial:       serial,
		PIN:          DefaultPIN,
		AdminPIN:     DefaultAdminPIN,
		PINRetries:   3,
		AdminRetries: 3,
		RSABits:      []int{2048, 3072, 4096},
		keys:         map[Slot]*rsa.PrivateKey{},
		created:      map[Slot]time.Time{},
	}
}

// Key liefert den importierten Slot-Schlüssel (Testinspektion).
func (f *FakeCard) Key(slot Slot) *rsa.PrivateKey { return f.keys[slot] }

func (f *FakeCard) info(reader string) CardInfo {
	info := CardInfo{
		Serial:       f.Serial,
		Manufacturer: "Fake",
		Reader:       reader,
		Slots:        map[Slot]SlotInfo{},
		RSABits:      append([]int(nil), f.RSABits...),
		Version:      "3.4",
		PINRetries:   f.PINRetries,
		AdminRetries: f.AdminRetries,
		MaxPINLen:    64,
		KDFSupported: f.KDFSupported,
		KDFActive:    f.KDFActive,
		CanReset:     true,
	}
	for _, slot := range []Slot{SlotSign, SlotDecrypt, SlotAuth} {
		key, ok := f.keys[slot]
		if !ok {
			info.Slots[slot] = SlotInfo{}
			continue
		}
		created := f.created[slot]
		pk := packet.NewRSAPublicKey(created, &key.PublicKey)
		info.Slots[slot] = SlotInfo{
			Present:     true,
			Fingerprint: FingerprintHex(pk.Fingerprint),
			Algo:        fmt.Sprintf("rsa%d", key.N.BitLen()),
			Created:     created,
		}
	}
	return info
}

// FakeBackend bedient Backend mit höchstens einer Karte.
type FakeBackend struct {
	Card    *FakeCard // nil = keine Karte gefunden
	OpenErr error     // simulierter Fehler (z.B. ErrCardBusy)
}

func (b *FakeBackend) Cards(context.Context) ([]CardInfo, error) {
	if b.OpenErr != nil {
		return nil, b.OpenErr
	}
	if b.Card == nil {
		return nil, ErrNoCard
	}
	return []CardInfo{b.Card.info("Fake Reader")}, nil
}

func (b *FakeBackend) Open(_ context.Context, serial string) (Session, error) {
	if b.OpenErr != nil {
		return nil, b.OpenErr
	}
	if b.Card == nil {
		return nil, ErrNoCard
	}
	if serial != "" && serial != b.Card.Serial {
		return nil, ErrWrongCard
	}
	return &fakeSession{card: b.Card}, nil
}

type fakeSession struct {
	card    *FakeCard
	pinOK   bool
	adminOK bool
	closed  bool
}

func (s *fakeSession) Info() CardInfo { return s.card.info("Fake Reader") }

func (s *fakeSession) VerifyPIN(pin string) error {
	return verify(pin, s.card.PIN, &s.card.PINRetries, &s.pinOK)
}

func (s *fakeSession) VerifyAdminPIN(pin string) error {
	return verify(pin, s.card.AdminPIN, &s.card.AdminRetries, &s.adminOK)
}

func verify(pin, want string, retries *int, ok *bool) error {
	if *retries <= 0 {
		return ErrPINBlocked
	}
	if pin != want {
		*retries--
		*ok = false
		if *retries <= 0 {
			return ErrPINBlocked
		}
		return &PINError{Retries: *retries}
	}
	*retries = 3
	*ok = true
	return nil
}

func (s *fakeSession) Signer(crypto.PublicKey) (crypto.Signer, error) {
	return s.slotKey(SlotSign, s.pinOK)
}

func (s *fakeSession) Decrypter(crypto.PublicKey) (crypto.Decrypter, error) {
	return s.slotKey(SlotDecrypt, s.pinOK)
}

// slotKey: *rsa.PrivateKey implementiert crypto.Signer UND crypto.Decrypter –
// genau wie der Karten-Schlüssel des PC/SC-Backends.
func (s *fakeSession) slotKey(slot Slot, verified bool) (*rsa.PrivateKey, error) {
	if !verified {
		return nil, ErrNotVerified
	}
	key, ok := s.card.keys[slot]
	if !ok {
		return nil, ErrNotSupported
	}
	return key, nil
}

func (s *fakeSession) ImportKey(slot Slot, priv crypto.PrivateKey, created time.Time) error {
	if !s.adminOK {
		return ErrNotVerified
	}
	key, ok := priv.(*rsa.PrivateKey)
	if !ok {
		return ErrNotSupported
	}
	s.card.keys[slot] = key
	s.card.created[slot] = created
	return nil
}

func (s *fakeSession) ChangePIN(current, next string) error {
	if err := verify(current, s.card.PIN, &s.card.PINRetries, &s.pinOK); err != nil {
		return err
	}
	if len(next) < MinPINLen {
		return ErrNotSupported
	}
	s.card.PIN = next
	return nil
}

func (s *fakeSession) ChangeAdminPIN(current, next string) error {
	if err := verify(current, s.card.AdminPIN, &s.card.AdminRetries, &s.adminOK); err != nil {
		return err
	}
	if len(next) < MinAdminPINLen {
		return ErrNotSupported
	}
	s.card.AdminPIN = next
	return nil
}

func (s *fakeSession) ResetPIN(adminPIN, newPIN string) error {
	if len(newPIN) < MinPINLen {
		return ErrNotSupported
	}
	if err := s.VerifyAdminPIN(adminPIN); err != nil {
		return err
	}
	s.card.PIN, s.card.PINRetries = newPIN, 3
	return nil
}

func (s *fakeSession) SetupKDF(pin, adminPIN string) error {
	if !s.card.KDFSupported || s.card.KDFActive {
		return ErrNotSupported
	}
	if err := s.VerifyAdminPIN(adminPIN); err != nil {
		return err
	}
	if pin != s.card.PIN {
		return &PINError{Retries: s.card.PINRetries}
	}
	s.card.KDFActive = true
	return nil
}

func (s *fakeSession) FactoryReset() error {
	kdf := s.card.KDFSupported
	*s.card = *NewFakeCard(s.card.Serial)
	s.card.KDFSupported = kdf
	return nil
}

func (s *fakeSession) Close() error {
	s.closed = true
	s.pinOK, s.adminOK = false, false
	return nil
}
