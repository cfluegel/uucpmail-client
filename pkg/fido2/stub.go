// Package fido2 implementiert SSH-Authentifizierung mit FIDO2-Security-Keys
// (OpenSSH sk-Schlüssel). Der private Schlüssel lebt im Token; lokal liegt
// nur ein Stub (Public Key + optionale Credential-ID) als JSON im
// Datenverzeichnis – im portablen Modus wandert er mit.
package fido2

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
)

// Von OpenSSH definierte sk-Algorithmen (PROTOCOL.u2f).
const (
	AlgoED25519 = "sk-ssh-ed25519@openssh.com"
	AlgoECDSA   = "sk-ecdsa-sha2-nistp256@openssh.com"
	// Application/RP-ID für SSH-Schlüssel (OpenSSH-Konvention).
	SSHApplication = "ssh:"
)

// Stub beschreibt einen Token-Schlüssel. CredentialID darf leer sein –
// dann wird der Resident Key per leerer AllowList vom Token entdeckt.
type Stub struct {
	Version      int       `json:"version"`
	Algorithm    string    `json:"algorithm"`
	Application  string    `json:"application"`
	CredentialID []byte    `json:"credential_id,omitempty"`
	PublicKey    []byte    `json:"public_key"` // SSH-Wire-Format
	Resident     bool      `json:"resident"`
	Created      time.Time `json:"created"`
	Comment      string    `json:"comment,omitempty"`
}

// LoadStub liest den Stub; os.ErrNotExist wird durchgereicht.
func LoadStub(path string) (*Stub, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	st := &Stub{}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("fido2-stub beschädigt: %w", err)
	}
	if st.Algorithm != AlgoED25519 && st.Algorithm != AlgoECDSA {
		return nil, fmt.Errorf("fido2-stub: unbekannter Algorithmus %q", st.Algorithm)
	}
	if len(st.PublicKey) == 0 {
		return nil, errors.New("fido2-stub: Public Key fehlt")
	}
	return st, nil
}

// Save schreibt den Stub atomar (tmp+rename, 0600).
func (st *Stub) Save(path string) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SSHPublicKey parst den gespeicherten Wire-Format-Schlüssel.
func (st *Stub) SSHPublicKey() (ssh.PublicKey, error) {
	return ssh.ParsePublicKey(st.PublicKey)
}
