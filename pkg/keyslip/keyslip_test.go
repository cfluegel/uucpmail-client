package keyslip

import (
	"bytes"
	"os"
	"testing"

	"fyne.io/fyne/v2/theme"
)

func testFonts() Fonts {
	return Fonts{
		Regular: theme.DefaultTextFont().Content(),
		Bold:    theme.DefaultTextBoldFont().Content(),
		Mono:    theme.DefaultTextMonospaceFont().Content(),
	}
}

func TestA4(t *testing.T) {
	info := Info{
		Name:        "Jürgen Müßig-Ölmann",
		Email:       "juergen.muessig-oelmann@beispiel-verein.example.org",
		KeyID:       "1EEB928B64FF2B4E",
		Types:       "DSA 1024 / ElGamal 2048",
		Fingerprint: "DA88AC5D21B2D55D42C67C811EEB928B64FF2B4E",
	}
	data, err := A4(info, testFonts())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatal("kein PDF")
	}
	if n := bytes.Count(data, []byte("/Type /Page\n")); n != 1 {
		t.Errorf("Seiten: %d, erwartet 1", n)
	}
	if PerPage < 20 {
		t.Errorf("nur %d Streifen pro Seite", PerPage)
	}
	if p := os.Getenv("KEYSLIP_OUT"); p != "" {
		_ = os.WriteFile(p, data, 0o600)
	}
}

func TestSplitFingerprint(t *testing.T) {
	got := splitFingerprint("DA88AC5D21B2D55D42C67C811EEB928B64FF2B4E")
	if got[0] != "DA88 AC5D 21B2 D55D 42C6" || got[1] != "7C81 1EEB 928B 64FF 2B4E" {
		t.Errorf("v4: %q", got)
	}
	v6 := splitFingerprint("0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF")
	if len(v6[0]) != 39 || len(v6[1]) != 39 {
		t.Errorf("v6: %q", v6)
	}
}
