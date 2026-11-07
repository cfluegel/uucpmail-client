package pgpcard

import (
	"encoding/json"
	"os"
	"time"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
)

// Algo-Werte im Stub.
const (
	AlgoRSA2048     = "rsa2048"
	AlgoRSA3072     = "rsa3072"
	AlgoRSA4096     = "rsa4096"
	AlgoEd25519Sign = "ed25519-sign-only" // fremde Ed25519-Karte: nur Signieren
)

// Stub beschreibt den kartengestützten OpenPGP-Schlüssel. Der komplette
// Schlüssel (Master+Subkeys, passphrase-verschlüsselt) bleibt zusätzlich im
// Secring; der Stub verknüpft ihn mit der Karte (Serial + Subkey-Fingerprints)
// und ist bei fremden (extern eingerichteten) Karten der einzige
// „Privat-Nachweis". Fingerprints: 40 Hex-Zeichen, Großschreibung.
type Stub struct {
	Version        int       `json:"version"`
	Serial         string    `json:"serial"`
	KeyFingerprint string    `json:"key_fingerprint"` // Master
	SignSubkey     string    `json:"sign_subkey_fpr"`
	DecryptSubkey  string    `json:"dec_subkey_fpr,omitempty"`  // leer bei ed25519-sign-only
	AuthSubkey     string    `json:"auth_subkey_fpr,omitempty"` // leer bei fremden Karten ohne AUT
	Algo           string    `json:"algo"`
	Created        time.Time `json:"created"`
}

// LoadStub liest den Stub; os.ErrNotExist wird durchgereicht.
func LoadStub(path string) (*Stub, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	st := &Stub{}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, i18n.Wrap(err, "err.pgpcard.stubcorrupt")
	}
	switch st.Algo {
	case AlgoRSA2048, AlgoRSA3072, AlgoRSA4096, AlgoEd25519Sign:
	default:
		return nil, i18n.Errorf("err.pgpcard.stubalgo", st.Algo)
	}
	if st.KeyFingerprint == "" || st.SignSubkey == "" || st.Serial == "" {
		return nil, i18n.Errorf("err.pgpcard.stubcorrupt")
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
