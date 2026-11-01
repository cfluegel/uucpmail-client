package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// resetDirCache setzt den once-Zustand für Tests zurück.
func resetDirCache() {
	dirOnce = sync.Once{}
	resolvedDir = ""
	portable = false
}

func withExeDir(t *testing.T, dir string) {
	t.Helper()
	orig := executableDir
	executableDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() {
		executableDir = orig
		resetDirCache()
	})
	resetDirCache()
}

func TestDirPortableWhenFolderExists(t *testing.T) {
	exeDir := t.TempDir()
	dataDir := filepath.Join(exeDir, PortableDirName)
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	withExeDir(t, exeDir)

	got, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if got != dataDir {
		t.Fatalf("Dir() = %q, erwartet %q", got, dataDir)
	}
	if !IsPortable() {
		t.Fatal("IsPortable() muss true sein")
	}
	// Alle Pfadhelfer müssen im portablen Ordner landen.
	for name, fn := range map[string]func() (string, error){
		"Path": Path, "KeyPath": KeyPath, "KnownHosts": KnownHostsPath,
		"Pubring": PubringPath, "Secring": SecringPath, "Contacts": ContactsPath,
		"MailDir": MailDir, "SpoolDir": SpoolDir,
	} {
		p, err := fn()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.HasPrefix(p, dataDir+string(filepath.Separator)) {
			t.Fatalf("%s = %q liegt nicht unter %q", name, p, dataDir)
		}
	}
}

func TestDirDefaultWhenNoPortableFolder(t *testing.T) {
	withExeDir(t, t.TempDir()) // kein uucpmail-data darin

	got, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "uucpmail")
	if got != want {
		t.Fatalf("Dir() = %q, erwartet %q", got, want)
	}
	if IsPortable() {
		t.Fatal("IsPortable() muss false sein")
	}
}

func TestPortableFileIsNotAFolder(t *testing.T) {
	exeDir := t.TempDir()
	// Eine DATEI namens uucpmail-data darf den portablen Modus nicht auslösen.
	if err := os.WriteFile(filepath.Join(exeDir, PortableDirName), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	withExeDir(t, exeDir)
	if _, err := Dir(); err != nil {
		t.Fatal(err)
	}
	if IsPortable() {
		t.Fatal("Datei statt Ordner darf nicht portabel schalten")
	}
}
