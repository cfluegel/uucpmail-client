package pgp

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
)

// Lokaler Prüfvermerk („Fingerprint persönlich geprüft") – Altbestand:
// neue Vermerke entstehen nicht mehr, vorhandene werden nur noch beim
// Löschen eines Schlüssels mit aufgeräumt.

type verifiedEntry struct {
	VerifiedAt time.Time `json:"verified_at"`
	Method     string    `json:"method"`
}

// LoadVerified liest die Prüfvermerke (fehlende Datei = keine Vermerke).
func (k *Keyring) LoadVerified(path string) error {
	k.verifiedPath = path
	k.verified = map[string]verifiedEntry{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &k.verified); err != nil {
		return i18n.Wrap(err, "err.wot.verifiedcorrupt")
	}
	return nil
}

// UnmarkVerified entfernt den lokalen Vermerk.
func (k *Keyring) UnmarkVerified(fingerprint string) error {
	delete(k.verified, strings.ToUpper(fingerprint))
	return k.saveVerified()
}

// IsVerified meldet, ob ein eigener Schlüssel diesen Schlüssel gültig
// zertifiziert hat. Alte „nur lokal“-Vermerke zählen nicht mehr (der
// lokale Vermerk ist als Nutzungsfall entfallen, 2026-11-07).
func (k *Keyring) IsVerified(fingerprint string) bool {
	return k.HasMyValidCert(fingerprint)
}

func (k *Keyring) saveVerified() error {
	if k.verifiedPath == "" {
		return nil // ohne Pfad (Tests): nur im Speicher
	}
	data, err := json.MarshalIndent(k.verified, "", "  ")
	if err != nil {
		return err
	}
	tmp := k.verifiedPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, k.verifiedPath)
}
