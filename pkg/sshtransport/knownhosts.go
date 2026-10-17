package sshtransport

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// HostKeyChangedError signalisiert einen Hostkey-Mismatch (möglicher MITM);
// wird nie automatisch repariert, die UI zeigt den Fingerprint-Vergleich.
type HostKeyChangedError struct {
	Host        string
	Fingerprint string
}

func (e *HostKeyChangedError) Error() string {
	return fmt.Sprintf("hostkey von %s hat sich geändert (neuer Fingerprint %s) – mögliche Man-in-the-Middle-Attacke; bei legitimem Serverwechsel den Eintrag in known_hosts löschen", e.Host, e.Fingerprint)
}

// ForgetHost entfernt alle known_hosts-Einträge für den Host (nach einer
// bewussten Nutzer-Bestätigung bei Hostkey-Wechsel). Der nächste Verbindungs-
// aufbau übernimmt den neuen Schlüssel wieder per TOFU.
func ForgetHost(path, host string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	norm := knownhosts.Normalize(host)
	var keep []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		matches := false
		for _, pattern := range strings.Split(fields[0], ",") {
			if pattern == norm || pattern == host {
				matches = true
				break
			}
		}
		if !matches {
			keep = append(keep, line)
		}
	}
	out := strings.Join(keep, "\n")
	if out != "" {
		out += "\n"
	}
	return os.WriteFile(path, []byte(out), 0o600)
}

// tofuHostKeyCallback implementiert accept-new: unbekannte Hosts werden beim
// ersten Kontakt in unsere eigene known_hosts-Datei übernommen (Fingerprint
// wird geloggt), ein abweichender Schlüssel ist ein harter Fehler.
func tofuHostKeyCallback(path string) (ssh.HostKeyCallback, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			return nil, err
		}
	}
	check, err := knownhosts.New(path)
	if err != nil {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := check(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			if len(keyErr.Want) > 0 {
				return &HostKeyChangedError{Host: hostname, Fingerprint: ssh.FingerprintSHA256(key)}
			}
			// Unbekannter Host: TOFU – Schlüssel übernehmen.
			f, ferr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
			if ferr != nil {
				return ferr
			}
			defer f.Close()
			line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
			if _, ferr := fmt.Fprintln(f, line); ferr != nil {
				return ferr
			}
			log.Printf("known_hosts: %s neu aufgenommen (Fingerprint %s)", hostname, ssh.FingerprintSHA256(key))
			return nil
		}
		return err
	}, nil
}
