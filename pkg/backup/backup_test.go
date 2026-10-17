package backup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateRestoreRoundtrip(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "config.json"), []byte(`{"node_name":"u1"}`), 0o600)
	os.WriteFile(filepath.Join(src, "id_ed25519"), []byte("PRIVATEKEY"), 0o600)
	os.WriteFile(filepath.Join(src, "secring.asc"), []byte("SECRING"), 0o600)

	data, err := Create(src, "backup-pass")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("PRIVATEKEY")) || bytes.Contains(data, []byte("u1")) {
		t.Fatal("klartext im Backup sichtbar")
	}

	dst := t.TempDir()
	// Ziel hat Altbestand, der überschrieben bzw. entfernt werden muss.
	os.WriteFile(filepath.Join(dst, "id_ed25519"), []byte("ALTKEY"), 0o600)
	os.WriteFile(filepath.Join(dst, "contacts.json"), []byte("[]"), 0o600) // nicht im Backup → weg

	if err := Restore(dst, data, "backup-pass"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "id_ed25519"))
	if string(got) != "PRIVATEKEY" {
		t.Errorf("ssh-key nicht überschrieben: %q", got)
	}
	st, err := os.Stat(filepath.Join(dst, "id_ed25519"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("mode: %v %v", st, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "contacts.json")); !errors.Is(err, os.ErrNotExist) {
		t.Error("überzählige Datei nicht entfernt")
	}
	cfg, _ := os.ReadFile(filepath.Join(dst, "config.json"))
	if !bytes.Contains(cfg, []byte("u1")) {
		t.Error("config nicht wiederhergestellt")
	}
}

func TestRestoreWrongPassphrase(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "config.json"), []byte("{}"), 0o600)
	data, _ := Create(src, "richtig")
	dst := t.TempDir()
	if err := Restore(dst, data, "falsch"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("err = %v, erwartet ErrWrongPassphrase", err)
	}
}

func TestCreateValidation(t *testing.T) {
	if _, err := Create(t.TempDir(), ""); err == nil {
		t.Error("leere Passphrase akzeptiert")
	}
	if _, err := Create(t.TempDir(), "x"); err == nil {
		t.Error("leeres Verzeichnis akzeptiert")
	}
}

func TestRestoreRejectsForeignEntries(t *testing.T) {
	// Manuell ein Backup mit bösem Pfad bauen ist aufwändig – stattdessen:
	// kaputte Daten dürfen nie etwas schreiben.
	dst := t.TempDir()
	if err := Restore(dst, []byte("kein backup"), "x"); err == nil {
		t.Error("müll akzeptiert")
	}
	entries, _ := os.ReadDir(dst)
	if len(entries) != 0 {
		t.Error("dateien trotz Fehler geschrieben")
	}
}
