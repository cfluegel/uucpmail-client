package store

import (
	"testing"
	"time"
)

const testMail = "From: a@example.org\r\nTo: b@example.org\r\nSubject: Test\r\nDate: Mon, 01 Sep 2026 10:00:00 +0200\r\n\r\nHallo\r\n"

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMoveToTrashAndRestore(t *testing.T) {
	s := openTestStore(t)
	m, err := s.Add(Sent, []byte(testMail), "a@example.org", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MoveToTrash(Sent, m.ID); err != nil {
		t.Fatal(err)
	}
	tm, err := s.Get(Trash, m.ID)
	if err != nil {
		t.Fatalf("nicht im Papierkorb: %v", err)
	}
	if tm.OrigFolder != Sent || tm.DeletedAt.IsZero() {
		t.Fatalf("OrigFolder/DeletedAt nicht gesetzt: %+v", tm)
	}
	if err := s.MoveToTrash(Trash, m.ID); err == nil {
		t.Fatal("MoveToTrash aus dem Papierkorb muss fehlschlagen")
	}
	if err := s.RestoreFromTrash(m.ID); err != nil {
		t.Fatal(err)
	}
	rm, err := s.Get(Sent, m.ID)
	if err != nil {
		t.Fatalf("nicht wiederhergestellt: %v", err)
	}
	if rm.OrigFolder != "" || !rm.DeletedAt.IsZero() {
		t.Fatalf("Papierkorb-Felder nicht geleert: %+v", rm)
	}
	if raw, err := s.Raw(Sent, m.ID); err != nil || string(raw) != testMail {
		t.Fatalf("Rohdaten nach Roundtrip falsch: %v", err)
	}
}

func TestRestoreFallbackInbox(t *testing.T) {
	s := openTestStore(t)
	m, err := s.Add(Inbox, []byte(testMail), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MoveToTrash(Inbox, m.ID); err != nil {
		t.Fatal(err)
	}
	// OrigFolder künstlich leeren (Altbestand simulieren).
	tm, _ := s.Get(Trash, m.ID)
	tm.OrigFolder = ""
	if err := s.putMeta(tm); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreFromTrash(m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(Inbox, m.ID); err != nil {
		t.Fatalf("Fallback Posteingang fehlt: %v", err)
	}
}

func TestPurgeTrash(t *testing.T) {
	s := openTestStore(t)
	old, _ := s.Add(Inbox, []byte(testMail), "", true)
	fresh, _ := s.Add(Inbox, []byte(testMail), "", true)
	unstamped, _ := s.Add(Inbox, []byte(testMail), "", true)
	for _, m := range []*Meta{old, fresh, unstamped} {
		if err := s.MoveToTrash(Inbox, m.ID); err != nil {
			t.Fatal(err)
		}
	}
	// old altern lassen, unstamped den Stempel nehmen (reconcile-Import simulieren).
	om, _ := s.Get(Trash, old.ID)
	om.DeletedAt = time.Now().Add(-40 * 24 * time.Hour)
	if err := s.putMeta(om); err != nil {
		t.Fatal(err)
	}
	um, _ := s.Get(Trash, unstamped.ID)
	um.DeletedAt = time.Time{}
	if err := s.putMeta(um); err != nil {
		t.Fatal(err)
	}

	n, err := s.PurgeTrash(30 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("erwartet 1 gelöscht, war %d", n)
	}
	if _, err := s.Get(Trash, old.ID); err == nil {
		t.Fatal("alte Mail nicht gelöscht")
	}
	if _, err := s.Get(Trash, fresh.ID); err != nil {
		t.Fatal("frische Mail fälschlich gelöscht")
	}
	sm, err := s.Get(Trash, unstamped.ID)
	if err != nil {
		t.Fatal("ungestempelte Mail fälschlich gelöscht")
	}
	if sm.DeletedAt.IsZero() {
		t.Fatal("ungestempelte Mail wurde nicht gestempelt")
	}
}
