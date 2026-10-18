package uucp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Job ist ein ausgehender rmail-Auftrag im lokalen Spool.
// Verzeichnislayout: <spool>/out/<JobID>/{data,meta.json}
type Job struct {
	ID         string    `json:"id"`
	Recipients []string  `json:"recipients"`
	Command    string    `json:"command,omitempty"` // leer = "rmail <Recipients>"
	Identity   string    `json:"identity"`          // Envelope-Sender
	StoreID    string    `json:"store_id"`          // ID der Outbox-Kopie im Mail-Store
	MessageID  string    `json:"message_id"`        // RFC-5322 Message-ID
	Created    time.Time `json:"created"`
	Attempts   int       `json:"attempts"`
	LastError  string    `json:"last_error,omitempty"`

	dir string
}

// Spool verwaltet die Jobverzeichnisse.
type Spool struct {
	root string // <spool>
}

func OpenSpool(root string) (*Spool, error) {
	for _, d := range []string{"out", "incoming", "quarantine"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			return nil, err
		}
	}
	return &Spool{root: root}, nil
}

func (s *Spool) OutDir() string        { return filepath.Join(s.root, "out") }
func (s *Spool) IncomingDir() string   { return filepath.Join(s.root, "incoming") }
func (s *Spool) QuarantineDir() string { return filepath.Join(s.root, "quarantine") }

// NewJobID erzeugt eine kurze eindeutige Job-ID (hex).
func NewJobID() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// Enqueue legt einen neuen Job mit der Spool-Mail (inkl. führender
// `From <identity> <date>`-Envelope-Zeile) an.
func (s *Spool) Enqueue(data []byte, job *Job) error {
	if job.ID == "" {
		job.ID = NewJobID()
	}
	if job.Created.IsZero() {
		job.Created = time.Now()
	}
	dir := filepath.Join(s.OutDir(), job.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "data"), data, 0o600); err != nil {
		return err
	}
	job.dir = dir
	return s.writeMeta(job)
}

func (s *Spool) writeMeta(job *Job) error {
	meta, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(job.dir, "meta.json.tmp")
	if err := os.WriteFile(tmp, meta, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(job.dir, "meta.json"))
}

// UpdateError vermerkt einen fehlgeschlagenen Versuch.
func (s *Spool) UpdateError(job *Job, cause error) error {
	job.Attempts++
	job.LastError = cause.Error()
	return s.writeMeta(job)
}

// Complete entfernt einen erfolgreich übertragenen Job.
func (s *Spool) Complete(job *Job) error {
	return os.RemoveAll(job.dir)
}

// DataPath liefert den Pfad zur Spool-Mail eines Jobs.
func (j *Job) DataPath() string { return filepath.Join(j.dir, "data") }

// Jobs liest alle anstehenden Jobs, älteste zuerst.
func (s *Spool) Jobs() ([]*Job, error) {
	entries, err := os.ReadDir(s.OutDir())
	if err != nil {
		return nil, err
	}
	var jobs []*Job
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(s.OutDir(), e.Name())
		meta, err := os.ReadFile(filepath.Join(dir, "meta.json"))
		if errors.Is(err, os.ErrNotExist) {
			continue // unvollständig angelegter Job
		}
		if err != nil {
			return nil, err
		}
		job := &Job{}
		if err := json.Unmarshal(meta, job); err != nil {
			return nil, fmt.Errorf("meta.json in %s beschädigt: %w", dir, err)
		}
		job.dir = dir
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, k int) bool { return jobs[i].Created.Before(jobs[k].Created) })
	return jobs, nil
}
