package sshtransport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestForgetHost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	content := "uucp.uucpmail.de ssh-ed25519 AAAAalt\n" +
		"[andere.example.com]:2222 ssh-ed25519 AAAAbleibt\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	// Host wie im Callback übergeben (mit Port).
	if err := ForgetHost(path, "uucp.uucpmail.de:22"); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	if strings.Contains(string(out), "AAAAalt") {
		t.Errorf("alter Eintrag nicht entfernt: %q", out)
	}
	if !strings.Contains(string(out), "AAAAbleibt") {
		t.Errorf("fremder Eintrag verloren: %q", out)
	}
	// Nicht existierende Datei ist kein Fehler.
	if err := ForgetHost(filepath.Join(t.TempDir(), "fehlt"), "x"); err != nil {
		t.Fatal(err)
	}
}
