// Package fakeserver implementiert für Tests die Serverseite des
// uucpmail-Wire-Protokolls (Taylor-Handshake + Protokoll t) über eine
// beliebige io.ReadWriter-Verbindung (typisch net.Pipe) – CI ohne Docker.
package fakeserver

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/cfluegel/uucpmail-client/pkg/uucp"
)

// ReceivedFile ist eine vom Client hochgeladene Datei.
type ReceivedFile struct {
	Name string // Wire-Zielname (D.*/X.*)
	Data []byte
}

// OutboundJob ist ein Job, den der fakeserver nach dem Rollentausch an den
// Client ausliefert (wie `uux -n -r - <node>!rmail <addr>` auf dem Server).
type OutboundJob struct {
	DataName string // z.B. "D.uucpsrvC0001"
	XName    string // z.B. "X.uucpsrvC0001"
	Data     []byte // Spool-Mail (mit Envelope-Zeile)
	XContent string // Execute-Datei; leer = aus DataName+Rcpt generiert
	Rcpt     string
}

// Server ist ein konfigurierbarer Fake-uucpsrv.
type Server struct {
	Name       string // Default "uucpsrv"
	WantCaller string // erwarteter Node-Name ("" = jeden akzeptieren)
	// RejectUploads lehnt S-Commands des Clients mit diesem SN-Code ab ("" = annehmen).
	RejectUploads string
	// AcceptUploads: Anzahl Dateien, die trotz RejectUploads zuvor angenommen
	// werden (0 = alle ablehnen). Simuliert z.B. Ablehnung erst der X-Datei.
	AcceptUploads int
	// FailConfirm beantwortet die Übertragung mit CN5 statt CY.
	FailConfirm bool
	// Outbound wird nach dem Rollentausch ausgeliefert.
	Outbound []OutboundJob
	// DropAfterUploads bricht die Verbindung nach N angenommenen Dateien hart ab (-1 = nie).
	DropAfterUploads int

	Received []ReceivedFile
}

// Serve fährt eine komplette Session auf conn. Rückgabe nil bei normalem Ende.
func (s *Server) Serve(conn io.ReadWriteCloser) error {
	defer conn.Close()
	if s.Name == "" {
		s.Name = "uucpsrv"
	}
	if s.DropAfterUploads == 0 {
		s.DropAfterUploads = -1
	}
	w := uucp.NewWire(conn)
	if _, err := uucp.AnswerHandshake(w, s.Name, s.WantCaller); err != nil {
		return err
	}

	// Slave-Phase: Client (Master) sendet Jobs, bis H kommt.
	uploads := 0
	for {
		cmd, err := w.ReadCommand()
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(cmd, "S "):
			req, err := uucp.ParseSendRequest(cmd)
			if err != nil {
				return err
			}
			if s.RejectUploads != "" && uploads >= s.AcceptUploads {
				if err := w.WriteCommand("SN" + s.RejectUploads); err != nil {
					return err
				}
				continue
			}
			if err := w.WriteCommand("SY"); err != nil {
				return err
			}
			var buf bytes.Buffer
			if _, err := w.ReceiveFile(&buf); err != nil {
				return err
			}
			s.Received = append(s.Received, ReceivedFile{Name: req.To, Data: buf.Bytes()})
			uploads++
			if s.DropAfterUploads >= 0 && uploads >= s.DropAfterUploads {
				return fmt.Errorf("fakeserver: verbindung absichtlich abgebrochen")
			}
			confirm := "CY"
			if s.FailConfirm {
				confirm = "CN5"
			}
			if err := w.WriteCommand(confirm); err != nil {
				return err
			}
		case strings.HasPrefix(cmd, "H"):
			if len(s.Outbound) == 0 {
				if err := w.WriteCommand("HY"); err != nil {
					return err
				}
				_, _ = w.ReadCommand() // HY-Bestätigung des Clients
				return nil
			}
			// Rollentausch: wir werden Master.
			if err := w.WriteCommand("HN"); err != nil {
				return err
			}
			return s.masterPhase(w)
		default:
			return fmt.Errorf("fakeserver: unerwartetes Command %q", cmd)
		}
	}
}

// masterPhase liefert die Outbound-Jobs aus und beendet die Session.
func (s *Server) masterPhase(w *uucp.Wire) error {
	for _, job := range s.Outbound {
		xContent := job.XContent
		if xContent == "" {
			xContent = uucp.BuildXFile("uucp", s.Name, job.DataName, "rmail "+job.Rcpt)
		}
		files := []struct {
			name string
			data []byte
		}{
			{job.DataName, job.Data},
			{job.XName, []byte(xContent)},
		}
		for _, f := range files {
			req := uucp.SendRequest{
				From: f.name, To: f.name, User: "uucp",
				Options: "C", Temp: f.name, Mode: "0666", Size: int64(len(f.data)),
			}
			if err := w.WriteCommand(req.Format(true, false)); err != nil {
				return err
			}
			reply, err := w.ReadCommand()
			if err != nil {
				return err
			}
			if !strings.HasPrefix(reply, "SY") {
				return fmt.Errorf("fakeserver: client lehnte %s ab: %q", f.name, reply)
			}
			if _, err := w.SendFile(bytes.NewReader(f.data)); err != nil {
				return err
			}
			confirm, err := w.ReadCommand()
			if err != nil {
				return err
			}
			if !strings.HasPrefix(confirm, "CY") {
				return fmt.Errorf("fakeserver: übertragung von %s nicht bestätigt: %q", f.name, confirm)
			}
		}
	}
	if err := w.WriteCommand("H"); err != nil {
		return err
	}
	reply, err := w.ReadCommand()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(reply, "HY") {
		return fmt.Errorf("fakeserver: unerwartete Hangup-Antwort %q", reply)
	}
	return w.WriteCommand("HY")
}
