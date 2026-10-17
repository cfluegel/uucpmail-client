// Package store ist der lokale Mail-Store: eine .eml-Datei pro Nachricht
// unter <maildir>/<Folder>/<id>.eml, dazu ein bbolt-Index mit Metadaten.
// Die .eml-Dateien sind die Quelle der Wahrheit; der Index ist rebuildbar.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

// Folder-Namen (stabil, UI übersetzt via i18n).
const (
	Inbox  = "Inbox"
	Drafts = "Drafts"
	Outbox = "Outbox"
	Sent   = "Sent"
)

var Folders = []string{Inbox, Drafts, Outbox, Sent}

// Meta sind die indexierten Eigenschaften einer Nachricht.
type Meta struct {
	ID           string    `json:"id"`
	Folder       string    `json:"folder"`
	From         string    `json:"from"`
	To           string    `json:"to"`
	Cc           string    `json:"cc,omitempty"`
	Subject      string    `json:"subject"`
	Date         time.Time `json:"date"`
	Read         bool      `json:"read"`
	Size         int64     `json:"size"`
	EnvelopeFrom string    `json:"envelope_from,omitempty"`
	HasAttach    bool      `json:"has_attachments,omitempty"`
	LastError    string    `json:"last_error,omitempty"` // z.B. SN-Ablehnung (Outbox)
}

type Store struct {
	dir string
	db  *bolt.DB
}

// Open öffnet den Store unter dir und legt Ordner/Index bei Bedarf an.
// Fehlt der Index oder ist er beschädigt, wird er aus den .eml-Dateien neu
// aufgebaut.
func Open(dir string) (*Store, error) {
	for _, f := range Folders {
		if err := os.MkdirAll(filepath.Join(dir, f), 0o700); err != nil {
			return nil, err
		}
	}
	db, err := bolt.Open(filepath.Join(dir, "index.db"), 0o600, &bolt.Options{Timeout: 3 * time.Second})
	if err != nil {
		// Beschädigten Index verwerfen und neu aufbauen.
		os.Remove(filepath.Join(dir, "index.db"))
		db, err = bolt.Open(filepath.Join(dir, "index.db"), 0o600, &bolt.Options{Timeout: 3 * time.Second})
		if err != nil {
			return nil, err
		}
	}
	// Der Index ist jederzeit aus den .eml-Dateien rekonstruierbar –
	// fsync pro Schreibzugriff lohnt sich nicht (spürbare UI-Latenz
	// z.B. beim Gelesen-Markieren).
	db.NoSync = true
	s := &Store{dir: dir, db: db}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		for _, f := range Folders {
			if _, err := tx.CreateBucketIfNotExists([]byte(f)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.reconcile(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// reconcile gleicht Index und Dateisystem ab (fehlende Dateien raus,
// unbekannte .eml-Dateien rein).
func (s *Store) reconcile() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, folder := range Folders {
			b := tx.Bucket([]byte(folder))
			onDisk := map[string]bool{}
			entries, err := os.ReadDir(filepath.Join(s.dir, folder))
			if err != nil {
				return err
			}
			for _, e := range entries {
				if id, ok := strings.CutSuffix(e.Name(), ".eml"); ok {
					onDisk[id] = true
				}
			}
			// Indexeinträge ohne Datei entfernen; Einträge mit noch
			// RFC-2047-kodierten Headern nachmigrieren (Read-Flag bleibt).
			var stale [][]byte
			var migrate []*Meta
			if err := b.ForEach(func(k, v []byte) error {
				if !onDisk[string(k)] {
					stale = append(stale, append([]byte(nil), k...))
					return nil
				}
				delete(onDisk, string(k))
				m := &Meta{}
				if err := json.Unmarshal(v, m); err == nil &&
					(strings.Contains(m.From, "=?") || strings.Contains(m.To, "=?") ||
						strings.Contains(m.Subject, "=?") || strings.Contains(m.Cc, "=?")) {
					if raw, err := os.ReadFile(s.path(folder, m.ID)); err == nil {
						fresh := metaFromEML(m.ID, folder, raw)
						fresh.Read = m.Read
						fresh.EnvelopeFrom = m.EnvelopeFrom
						fresh.LastError = m.LastError
						migrate = append(migrate, fresh)
					}
				}
				return nil
			}); err != nil {
				return err
			}
			for _, m := range migrate {
				if enc, err := json.Marshal(m); err == nil {
					if err := b.Put([]byte(m.ID), enc); err != nil {
						return err
					}
				}
			}
			for _, k := range stale {
				if err := b.Delete(k); err != nil {
					return err
				}
			}
			// Dateien ohne Indexeintrag nachindexieren.
			for id := range onDisk {
				raw, err := os.ReadFile(s.path(folder, id))
				if err != nil {
					continue
				}
				meta := metaFromEML(id, folder, raw)
				meta.Read = true // unbekannte Herkunft nicht als neu markieren
				enc, err := json.Marshal(meta)
				if err != nil {
					return err
				}
				if err := b.Put([]byte(id), enc); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *Store) path(folder, id string) string {
	return filepath.Join(s.dir, folder, id+".eml")
}

// NewID erzeugt eine zeitlich sortierbare eindeutige Nachrichten-ID.
func NewID() string {
	return fmt.Sprintf("%016x-%s", time.Now().UnixNano(), randHex(4))
}

// Add legt eine Nachricht (RFC-5322-Rohdaten ohne Envelope-Zeile) im Ordner ab.
func (s *Store) Add(folder string, raw []byte, envelopeFrom string, read bool) (*Meta, error) {
	id := NewID()
	if err := os.WriteFile(s.path(folder, id), raw, 0o600); err != nil {
		return nil, err
	}
	meta := metaFromEML(id, folder, raw)
	meta.EnvelopeFrom = envelopeFrom
	meta.Read = read
	if err := s.putMeta(meta); err != nil {
		os.Remove(s.path(folder, id))
		return nil, err
	}
	return meta, nil
}

func (s *Store) putMeta(meta *Meta) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		enc, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte(meta.Folder)).Put([]byte(meta.ID), enc)
	})
}

// Update überschreibt den Inhalt einer vorhandenen Nachricht (Entwürfe).
func (s *Store) Update(folder, id string, raw []byte) (*Meta, error) {
	if _, err := s.Get(folder, id); err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.path(folder, id), raw, 0o600); err != nil {
		return nil, err
	}
	meta := metaFromEML(id, folder, raw)
	meta.Read = true
	if err := s.putMeta(meta); err != nil {
		return nil, err
	}
	return meta, nil
}

// Get liefert die Metadaten einer Nachricht.
func (s *Store) Get(folder, id string) (*Meta, error) {
	var meta *Meta
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket([]byte(folder)).Get([]byte(id))
		if v == nil {
			return fmt.Errorf("nachricht %s/%s nicht im Index", folder, id)
		}
		meta = &Meta{}
		return json.Unmarshal(v, meta)
	})
	return meta, err
}

// Raw liefert die .eml-Rohdaten.
func (s *Store) Raw(folder, id string) ([]byte, error) {
	return os.ReadFile(s.path(folder, id))
}

// List liefert die Nachrichten eines Ordners, neueste zuerst.
func (s *Store) List(folder string) ([]*Meta, error) {
	var out []*Meta
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(folder)).ForEach(func(k, v []byte) error {
			m := &Meta{}
			if err := json.Unmarshal(v, m); err != nil {
				return err
			}
			out = append(out, m)
			return nil
		})
	})
	sort.Slice(out, func(i, k int) bool { return out[i].Date.After(out[k].Date) })
	return out, err
}

// UnreadCount zählt ungelesene Nachrichten im Ordner.
func (s *Store) UnreadCount(folder string) (int, error) {
	n := 0
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(folder)).ForEach(func(k, v []byte) error {
			m := &Meta{}
			if err := json.Unmarshal(v, m); err != nil {
				return err
			}
			if !m.Read {
				n++
			}
			return nil
		})
	})
	return n, err
}

// SetRead setzt das Gelesen-Flag.
func (s *Store) SetRead(folder, id string, read bool) error {
	meta, err := s.Get(folder, id)
	if err != nil {
		return err
	}
	meta.Read = read
	return s.putMeta(meta)
}

// SetError vermerkt einen Übertragungsfehler (Outbox-Anzeige).
func (s *Store) SetError(folder, id, msg string) error {
	meta, err := s.Get(folder, id)
	if err != nil {
		return err
	}
	meta.LastError = msg
	return s.putMeta(meta)
}

// Move verschiebt eine Nachricht in einen anderen Ordner (z.B. Outbox→Sent).
func (s *Store) Move(fromFolder, id, toFolder string) error {
	meta, err := s.Get(fromFolder, id)
	if err != nil {
		return err
	}
	if err := os.Rename(s.path(fromFolder, id), s.path(toFolder, id)); err != nil {
		return err
	}
	meta.Folder = toFolder
	meta.LastError = ""
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket([]byte(fromFolder)).Delete([]byte(id)); err != nil {
			return err
		}
		enc, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte(toFolder)).Put([]byte(id), enc)
	})
}

// Delete entfernt eine Nachricht endgültig.
func (s *Store) Delete(folder, id string) error {
	if err := os.Remove(s.path(folder, id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(folder)).Delete([]byte(id))
	})
}
