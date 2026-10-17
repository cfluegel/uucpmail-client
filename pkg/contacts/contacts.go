// Package contacts ist das minimale Adressbuch des Clients: Vorname, Nachname,
// E-Mail und optional die Fingerprint-REFERENZ auf einen PGP-Schlüssel.
// Das Schlüsselmaterial selbst lebt ausschließlich im Keyring (pubring.asc) –
// die Verschlüsselung löst Empfänger weiterhin per E-Mail über den Keyring auf.
package contacts

import (
	"encoding/json"
	"errors"
	"net/mail"
	"os"
	"sort"
	"strings"
)

// Contact ist ein Adressbucheintrag (bewusst minimal).
type Contact struct {
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	Email          string `json:"email"`
	KeyFingerprint string `json:"key_fingerprint,omitempty"`
}

// Typisierte Fehler für die i18n-Zuordnung in der UI.
var (
	ErrInvalidEmail   = errors.New("ungültige E-Mail-Adresse")
	ErrDuplicateEmail = errors.New("kontakt mit dieser E-Mail-Adresse existiert bereits")
	ErrNameRequired   = errors.New("vor- oder Nachname wird benötigt")
	ErrNotFound       = errors.New("kontakt nicht gefunden")
)

// Book hält die Kontakte und persistiert nach contacts.json.
type Book struct {
	path     string
	contacts []Contact
}

// Load liest das Adressbuch; eine fehlende Datei ergibt ein leeres Buch.
func Load(path string) (*Book, error) {
	b := &Book{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &b.contacts); err != nil {
		return nil, err
	}
	return b, nil
}

// Save schreibt sortiert und atomar (tmp+rename, 0600).
func (b *Book) Save() error {
	b.sort()
	data, err := json.MarshalIndent(b.contacts, "", "  ")
	if err != nil {
		return err
	}
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.path)
}

func (b *Book) sort() {
	sort.SliceStable(b.contacts, func(i, j int) bool {
		li, lj := strings.ToLower(b.contacts[i].LastName), strings.ToLower(b.contacts[j].LastName)
		if li != lj {
			return li < lj
		}
		fi, fj := strings.ToLower(b.contacts[i].FirstName), strings.ToLower(b.contacts[j].FirstName)
		if fi != fj {
			return fi < fj
		}
		return b.contacts[i].Email < b.contacts[j].Email
	})
}

// List liefert eine sortierte Kopie.
func (b *Book) List() []Contact {
	b.sort()
	out := make([]Contact, len(b.contacts))
	copy(out, b.contacts)
	return out
}

// Validate prüft die Pflichtfelder und normalisiert die E-Mail (lowercase,
// reine Adresse ohne Anzeigename).
func Validate(c *Contact) error {
	addr, err := mail.ParseAddress(strings.TrimSpace(c.Email))
	if err != nil {
		return ErrInvalidEmail
	}
	c.Email = strings.ToLower(addr.Address)
	if strings.TrimSpace(c.FirstName) == "" && strings.TrimSpace(c.LastName) == "" {
		return ErrNameRequired
	}
	return nil
}

// Add legt einen neuen Kontakt an (E-Mail muss eindeutig sein) und speichert.
func (b *Book) Add(c Contact) error {
	if err := Validate(&c); err != nil {
		return err
	}
	if _, ok := b.FindByEmail(c.Email); ok {
		return ErrDuplicateEmail
	}
	b.contacts = append(b.contacts, c)
	return b.Save()
}

// Update ersetzt den Kontakt mit der alten E-Mail; eine Umbenennung auf die
// E-Mail eines anderen Kontakts wird abgelehnt.
func (b *Book) Update(oldEmail string, c Contact) error {
	if err := Validate(&c); err != nil {
		return err
	}
	idx := b.indexOf(oldEmail)
	if idx < 0 {
		return ErrNotFound
	}
	if !strings.EqualFold(c.Email, b.contacts[idx].Email) {
		if _, ok := b.FindByEmail(c.Email); ok {
			return ErrDuplicateEmail
		}
	}
	b.contacts[idx] = c
	return b.Save()
}

// Delete entfernt den Kontakt (der referenzierte Schlüssel bleibt im Keyring).
func (b *Book) Delete(email string) error {
	idx := b.indexOf(email)
	if idx < 0 {
		return ErrNotFound
	}
	b.contacts = append(b.contacts[:idx], b.contacts[idx+1:]...)
	return b.Save()
}

// FindByEmail sucht case-insensitive.
func (b *Book) FindByEmail(email string) (Contact, bool) {
	if idx := b.indexOf(email); idx >= 0 {
		return b.contacts[idx], true
	}
	return Contact{}, false
}

func (b *Book) indexOf(email string) int {
	want := strings.ToLower(strings.TrimSpace(email))
	for i, c := range b.contacts {
		if strings.ToLower(c.Email) == want {
			return i
		}
	}
	return -1
}
