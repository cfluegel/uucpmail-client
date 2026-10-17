package contacts

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newBook(t *testing.T) *Book {
	t.Helper()
	b, err := Load(filepath.Join(t.TempDir(), "contacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	b := newBook(t)
	if len(b.List()) != 0 {
		t.Fatal("leeres Buch erwartet")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		c    Contact
		want error
	}{
		{Contact{FirstName: "A", Email: "a@b.de"}, nil},
		{Contact{LastName: "B", Email: "Name <X@B.DE>"}, nil}, // Anzeigename + Case
		{Contact{FirstName: "A", Email: "kein-at"}, ErrInvalidEmail},
		{Contact{Email: "a@b.de"}, ErrNameRequired},
	}
	for i, tc := range cases {
		err := Validate(&tc.c)
		if !errors.Is(err, tc.want) && err != tc.want {
			t.Errorf("fall %d: err = %v, erwartet %v", i, err, tc.want)
		}
	}
	// Normalisierung
	c := Contact{FirstName: "A", Email: "Chris <CHRIS@Example.COM>"}
	if err := Validate(&c); err != nil || c.Email != "chris@example.com" {
		t.Errorf("normalisierung: %q err=%v", c.Email, err)
	}
}

func TestAddDuplicateAndSort(t *testing.T) {
	b := newBook(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(b.Add(Contact{FirstName: "Zoe", LastName: "Meier", Email: "zoe@x.de"}))
	must(b.Add(Contact{FirstName: "Anna", LastName: "meier", Email: "anna@x.de"}))
	must(b.Add(Contact{FirstName: "Ben", LastName: "Abel", Email: "ben@x.de"}))
	if err := b.Add(Contact{FirstName: "Dup", Email: "ZOE@x.de"}); !errors.Is(err, ErrDuplicateEmail) {
		t.Fatalf("duplikat nicht abgelehnt: %v", err)
	}
	l := b.List()
	if l[0].LastName != "Abel" || l[1].FirstName != "Anna" || l[2].FirstName != "Zoe" {
		t.Errorf("sortierung: %+v", l)
	}
}

func TestUpdateDeleteFind(t *testing.T) {
	b := newBook(t)
	b.Add(Contact{FirstName: "A", Email: "a@x.de"})
	b.Add(Contact{FirstName: "B", Email: "b@x.de"})

	// Update inkl. E-Mail-Wechsel
	if err := b.Update("a@x.de", Contact{FirstName: "A2", Email: "a2@x.de", KeyFingerprint: "ABCD"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.FindByEmail("a@x.de"); ok {
		t.Error("alte adresse noch vorhanden")
	}
	got, ok := b.FindByEmail("A2@X.DE")
	if !ok || got.FirstName != "A2" || got.KeyFingerprint != "ABCD" {
		t.Errorf("find: %+v ok=%v", got, ok)
	}
	// Umbenennung auf fremde Adresse → Duplikat
	if err := b.Update("a2@x.de", Contact{FirstName: "A2", Email: "b@x.de"}); !errors.Is(err, ErrDuplicateEmail) {
		t.Fatalf("kollision nicht erkannt: %v", err)
	}
	// Update auf dieselbe Adresse (nur Felder ändern) ist erlaubt
	if err := b.Update("a2@x.de", Contact{FirstName: "A3", Email: "a2@x.de"}); err != nil {
		t.Fatal(err)
	}
	// Delete
	if err := b.Delete("b@x.de"); err != nil {
		t.Fatal(err)
	}
	if err := b.Delete("b@x.de"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("doppel-delete: %v", err)
	}
	if len(b.List()) != 1 {
		t.Errorf("rest: %d", len(b.List()))
	}
}

func TestPersistenceRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "contacts.json")
	b, _ := Load(path)
	b.Add(Contact{FirstName: "Chris", LastName: "F", Email: "c@x.de", KeyFingerprint: "FPR1"})

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode: %o", st.Mode().Perm())
	}
	b2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := b2.FindByEmail("c@x.de")
	if !ok || got.KeyFingerprint != "FPR1" || got.LastName != "F" {
		t.Errorf("roundtrip: %+v", got)
	}
}
