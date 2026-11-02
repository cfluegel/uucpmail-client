package i18n

import (
	"errors"
	"testing"
)

// TestTablesHaveSameKeys stellt sicher, dass beide Sprachen exakt
// dieselben Schlüssel haben (kein stilles Fallback auf Englisch).
func TestTablesHaveSameKeys(t *testing.T) {
	de, en := tables["de"], tables["en"]
	for k := range de {
		if _, ok := en[k]; !ok {
			t.Errorf("Schlüssel %q fehlt in en", k)
		}
	}
	for k := range en {
		if _, ok := de[k]; !ok {
			t.Errorf("Schlüssel %q fehlt in de", k)
		}
	}
}

func TestErrorRendersLazilyPerLanguage(t *testing.T) {
	tables["de"]["test.err"] = "kaputt: %s"
	tables["en"]["test.err"] = "broken: %s"
	defer delete(tables["de"], "test.err")
	defer delete(tables["en"], "test.err")

	sentinel := Errorf("test.err", "X")
	orig := lang
	defer func() { lang = orig }()

	lang = "en"
	if got := sentinel.Error(); got != "broken: X" {
		t.Fatalf("en: %q", got)
	}
	lang = "de"
	if got := sentinel.Error(); got != "kaputt: X" {
		t.Fatalf("de: %q", got)
	}
}

func TestErrorWrapAndIs(t *testing.T) {
	tables["en"]["test.inner"] = "inner"
	tables["en"]["test.outer"] = "outer"
	defer delete(tables["en"], "test.inner")
	defer delete(tables["en"], "test.outer")

	inner := Errorf("test.inner")
	wrapped := Wrap(inner, "test.outer")
	if !errors.Is(wrapped, inner) {
		t.Fatal("errors.Is über Wrap kaputt")
	}
	var ie *Error
	if !errors.As(wrapped, &ie) || ie.Key != "test.outer" {
		t.Fatalf("errors.As liefert %v", ie)
	}
	orig := lang
	lang = "en"
	defer func() { lang = orig }()
	if got := wrapped.Error(); got != "outer: inner" {
		t.Fatalf("got %q", got)
	}
}
