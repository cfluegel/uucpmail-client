// Package backup erstellt und importiert verschlüsselte Sicherungen der
// Client-Identität: Konfiguration, SSH-Schlüssel, known_hosts, PGP-Schlüsselbund
// und Kontakte – NICHT der Mail-Bestand. Format: tar, symmetrisch OpenPGP-
// verschlüsselt (wie `gpg -c`, mit gpg kompatibel entschlüsselbar).
package backup

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// Files ist die feste Whitelist der gesicherten Dateien (relativ zum
// Config-Verzeichnis). Beim Import wird NUR diese Liste akzeptiert –
// Pfad-Tricks in manipulierten Archiven laufen ins Leere.
var Files = []string{
	"config.json",
	"id_ed25519",
	"id_ed25519.pub",
	"known_hosts",
	"pubring.asc",
	"secring.asc",
	"contacts.json",
}

// ErrWrongPassphrase signalisiert eine falsche Backup-Passphrase.
var ErrWrongPassphrase = errors.New("falsche Passphrase oder beschädigtes Backup")

func allowed(name string) bool {
	for _, f := range Files {
		if name == f {
			return true
		}
	}
	return false
}

// Create liest die vorhandenen Dateien aus dir (fehlende werden übersprungen)
// und liefert das verschlüsselte Backup.
func Create(dir, passphrase string) ([]byte, error) {
	if strings.TrimSpace(passphrase) == "" {
		return nil, errors.New("leere Passphrase ist nicht erlaubt")
	}
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	count := 0
	for _, name := range Files {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: time.Now(),
		}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
		count++
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, errors.New("keine zu sichernden Dateien gefunden")
	}

	var out bytes.Buffer
	cfg := &packet.Config{DefaultCipher: packet.CipherAES256}
	w, err := openpgp.SymmetricallyEncrypt(&out, []byte(passphrase), nil, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(tarBuf.Bytes()); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Restore entschlüsselt das Backup und ÜBERSCHREIBT die Whitelist-Dateien in
// dir. Nicht im Backup enthaltene Whitelist-Dateien werden gelöscht, damit
// der Zielzustand exakt dem gesicherten Client entspricht.
func Restore(dir string, data []byte, passphrase string) error {
	attempts := 0
	prompt := func(keys []openpgp.Key, symmetric bool) ([]byte, error) {
		attempts++
		if attempts > 1 {
			return nil, ErrWrongPassphrase
		}
		return []byte(passphrase), nil
	}
	md, err := openpgp.ReadMessage(bytes.NewReader(data), nil, prompt, nil)
	if err != nil {
		return ErrWrongPassphrase
	}
	plain, err := io.ReadAll(io.LimitReader(md.UnverifiedBody, 64<<20))
	if err != nil {
		return ErrWrongPassphrase // MDC-Fehler bei falscher Passphrase
	}

	// Erst vollständig entpacken und validieren, dann schreiben.
	restored := map[string][]byte{}
	tr := tar.NewReader(bytes.NewReader(plain))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("backup beschädigt: %w", err)
		}
		name := filepath.Base(filepath.Clean(hdr.Name))
		if !allowed(name) {
			return fmt.Errorf("unerwarteter Eintrag im Backup: %q", hdr.Name)
		}
		content, err := io.ReadAll(io.LimitReader(tr, 16<<20))
		if err != nil {
			return err
		}
		restored[name] = content
	}
	if len(restored) == 0 {
		return errors.New("backup enthält keine Dateien")
	}

	for _, name := range Files {
		target := filepath.Join(dir, name)
		content, ok := restored[name]
		if !ok {
			// Zielzustand = Backup-Zustand: überzählige Dateien entfernen.
			if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		tmp := target + ".tmp"
		if err := os.WriteFile(tmp, content, 0o600); err != nil {
			return err
		}
		if err := os.Rename(tmp, target); err != nil {
			return err
		}
	}
	return nil
}
