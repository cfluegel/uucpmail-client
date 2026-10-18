package uucp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Logf ist der Progress-Callback für UI/CLI (nil = still).
type Logf func(format string, args ...any)

// SessionResult fasst eine gefahrene UUCP-Session zusammen.
type SessionResult struct {
	Sent     []*Job   // erfolgreich übertragene Jobs (beide Dateien CY-bestätigt)
	Failed   []*Job   // abgelehnte Jobs (bleiben im Spool/der Outbox)
	Received []string // Pfade empfangener Dateien unter spool/incoming
}

// Session fährt die Transferphase (nach erfolgreichem Handshake):
// Master-Phase (eigene Jobs senden) → H → ggf. Rollentausch → Slave-Phase
// (Dateien empfangen) → Hangup. Shutdown (OO) macht der Aufrufer.
type Session struct {
	Wire      *Wire
	NodeName  string
	Spool     *Spool
	SizeNegot bool // Size-Feld im S-Command senden (nach -N-Negotiation)
	V103      bool // Größen dezimal statt hex kodieren (Alt-Taylor)
	Log       Logf
}

func (s *Session) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
	}
}

// Run führt die komplette Transferphase aus.
func (s *Session) Run() (*SessionResult, error) {
	res := &SessionResult{}

	jobs, err := s.Spool.Jobs()
	if err != nil {
		return res, err
	}
	for _, job := range jobs {
		if err := s.sendJob(job); err != nil {
			if isWireError(err) {
				_ = s.Spool.UpdateError(job, err)
				return res, fmt.Errorf("verbindung während Übertragung von Job %s abgebrochen: %w", job.ID, err)
			}
			s.logf("Job %s abgelehnt: %v", job.ID, err)
			_ = s.Spool.UpdateError(job, err)
			res.Failed = append(res.Failed, job)
			continue
		}
		what := strings.Join(job.Recipients, ", ")
		if job.Command != "" {
			what = job.Command
		}
		s.logf("Job %s an Server übergeben (%s) – Zustellung erfolgt asynchron", job.ID, what)
		res.Sent = append(res.Sent, job)
	}

	// Keine weitere Arbeit: Hangup anbieten.
	if err := s.Wire.WriteCommand("H"); err != nil {
		return res, err
	}
	reply, err := s.Wire.ReadCommand()
	if err != nil {
		return res, err
	}
	switch {
	case strings.HasPrefix(reply, "HY"):
		// Server hat auch nichts: Bestätigung senden, fertig.
		_ = s.Wire.WriteCommand("HY")
	case strings.HasPrefix(reply, "HN"):
		// Rollentausch: Server ist jetzt Master und sendet seine Jobs.
		if err := s.slavePhase(res); err != nil {
			return res, err
		}
	default:
		return res, fmt.Errorf("unerwartete Antwort auf H: %q", reply)
	}
	return res, nil
}

// transferError markiert eine serverseitige Ablehnung (Job bleibt liegen,
// Session läuft weiter) im Gegensatz zu Wire-/IO-Fehlern.
type transferError struct{ msg string }

func (e *transferError) Error() string { return e.msg }

func isWireError(err error) bool {
	var te *transferError
	return !errors.As(err, &te)
}

// sendJob überträgt Datendatei und Execute-Datei eines Jobs.
func (s *Session) sendJob(job *Job) error {
	seq := job.ID
	if len(seq) > 6 {
		seq = seq[:6]
	}
	dataName := fmt.Sprintf("D.%sC%s", s.NodeName, seq)
	xName := fmt.Sprintf("X.%sC%s", s.NodeName, seq)

	data, err := os.ReadFile(job.DataPath())
	if err != nil {
		return err
	}
	if err := s.sendFile(dataName, dataName, strings.NewReader(string(data)), int64(len(data))); err != nil {
		return fmt.Errorf("datendatei: %w", err)
	}

	command := job.Command
	if command == "" {
		command = "rmail " + strings.Join(job.Recipients, " ")
	}
	xContent := BuildXFile("uucpmail", s.NodeName, dataName, command)
	if err := s.sendFile("D."+xName[2:], xName, strings.NewReader(xContent), int64(len(xContent))); err != nil {
		return fmt.Errorf("execute-datei: %w", err)
	}
	return nil
}

// sendFile fährt S → SY → Daten → CY für eine Datei.
func (s *Session) sendFile(from, to string, r *strings.Reader, size int64) error {
	req := SendRequest{
		From: from, To: to, User: "uucpmail",
		Options: "C", Temp: from, Mode: "0666", Size: size,
	}
	if err := s.Wire.WriteCommand(req.Format(s.SizeNegot, s.V103)); err != nil {
		return err
	}
	reply, err := s.Wire.ReadCommand()
	if err != nil {
		return err
	}
	rep, err := ParseReply(reply)
	if err != nil || rep.Kind != 'S' {
		return fmt.Errorf("unerwartete Antwort auf S-Command: %q", reply)
	}
	if !rep.OK {
		return &transferError{SNReason(rep.Detail)}
	}
	if _, err := s.Wire.SendFile(r); err != nil {
		return err
	}
	confirm, err := s.Wire.ReadCommand()
	if err != nil {
		return err
	}
	crep, err := ParseReply(confirm)
	if err != nil || crep.Kind != 'C' {
		return fmt.Errorf("unerwartete Übertragungsbestätigung: %q", confirm)
	}
	if !crep.OK {
		return &transferError{"Server konnte die Datei nicht ablegen (CN" + crep.Detail + ")"}
	}
	return nil
}

// slavePhase empfängt die vom Server gestagten Dateien, bis dieser H sendet.
func (s *Session) slavePhase(res *SessionResult) error {
	for {
		cmd, err := s.Wire.ReadCommand()
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(cmd, "S "):
			path, err := s.receiveFile(cmd)
			if err != nil {
				return err
			}
			if path != "" {
				res.Received = append(res.Received, path)
			}
		case strings.HasPrefix(cmd, "R "):
			// Dateianforderungen vom Server lehnen wir ab (kein Produkt-Feature).
			if err := s.Wire.WriteCommand("RN2"); err != nil {
				return err
			}
		case strings.HasPrefix(cmd, "X "):
			if err := s.Wire.WriteCommand("XN"); err != nil {
				return err
			}
		case strings.HasPrefix(cmd, "E "):
			// E-Commands (Exec ohne X.-Datei) bieten wir nicht an (kein
			// FEATURE_EXEC im Handshake) und lehnen sie defensiv ab.
			if err := s.Wire.WriteCommand("EN2"); err != nil {
				return err
			}
		case cmd == "H" || strings.HasPrefix(cmd, "H"):
			// Wir haben keine neue Arbeit: zustimmen und Bestätigung tolerant lesen.
			if err := s.Wire.WriteCommand("HY"); err != nil {
				return err
			}
			if confirm, err := s.Wire.ReadCommand(); err == nil && !strings.HasPrefix(confirm, "HY") {
				s.logf("unerwartete Hangup-Bestätigung: %q", confirm)
			}
			return nil
		default:
			return fmt.Errorf("unerwartetes Command in Slave-Phase: %q", cmd)
		}
	}
}

// receiveFile beantwortet ein S-Command des Servers und empfängt die Datei
// nach spool/incoming/<basename>.
func (s *Session) receiveFile(cmd string) (string, error) {
	req, err := ParseSendRequest(cmd)
	if err != nil {
		_ = s.Wire.WriteCommand("SN2")
		return "", nil
	}
	name := filepath.Base(strings.ReplaceAll(req.To, "\\", "/"))
	if name == "" || name == "." || name == ".." ||
		(!strings.HasPrefix(name, "D.") && !strings.HasPrefix(name, "X.")) {
		s.logf("unerwarteter Dateiname %q abgelehnt", req.To)
		if err := s.Wire.WriteCommand("SN2"); err != nil {
			return "", err
		}
		return "", nil
	}
	dst := filepath.Join(s.Spool.IncomingDir(), name)
	tmp := dst + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		if werr := s.Wire.WriteCommand("SN4"); werr != nil {
			return "", werr
		}
		return "", nil
	}
	if err := s.Wire.WriteCommand("SY"); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	n, err := s.Wire.ReceiveFile(f)
	closeErr := f.Close()
	if err != nil {
		os.Remove(tmp)
		return "", err // Wire-Fehler: Session ist nicht fortsetzbar
	}
	if closeErr != nil {
		os.Remove(tmp)
		if werr := s.Wire.WriteCommand("CN5"); werr != nil {
			return "", werr
		}
		return "", nil
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		if werr := s.Wire.WriteCommand("CN5"); werr != nil {
			return "", werr
		}
		return "", nil
	}
	if err := s.Wire.WriteCommand("CY"); err != nil {
		return "", err
	}
	s.logf("empfangen: %s (%d Bytes)", name, n)
	return dst, nil
}
