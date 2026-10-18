// Package sync orchestriert eine UUCP-Session: Outbox-Jobs senden,
// eingehende Dateien empfangen und per eingebautem uuxqt in die Inbox legen.
package sync

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cfluegel/uucpmail-client/pkg/compose"
	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/sshtransport"
	"github.com/cfluegel/uucpmail-client/pkg/store"
	"github.com/cfluegel/uucpmail-client/pkg/uucp"
)

const Version = "1.1.0"

// Result fasst einen Sync-Lauf zusammen.
type Result struct {
	SentCount     int
	FailedCount   int
	ReceivedCount int
}

// Sealer macht aus einer Nachricht die signierte/verschlüsselte Wire-Form
// (implementiert von pgp.MailSealer; nil = PGP-Feature aus).
type Sealer interface {
	Seal(msg *compose.Message, outerHdr, inner []byte) ([]byte, error)
}

// Engine bündelt die Abhängigkeiten eines Sync-Laufs.
type Engine struct {
	Cfg   *config.Config
	Store *store.Store
	Spool *uucp.Spool
	PGP   Sealer // optional
	Log   uucp.Logf
}

func (e *Engine) logf(format string, args ...any) {
	if e.Log != nil {
		e.Log(format, args...)
	}
}

// Enqueue stellt eine komponierte Nachricht in Outbox und Spool ein.
func (e *Engine) Enqueue(msg *compose.Message) (*store.Meta, error) {
	if err := msg.Validate(); err != nil {
		return nil, err
	}
	if warn := config.CheckIdentity(msg.Identity); warn != "" {
		e.logf("%s", warn)
	}
	var raw []byte
	if msg.Sign || msg.Encrypt {
		if e.PGP == nil {
			return nil, fmt.Errorf("PGP ist nicht initialisiert")
		}
		outer, inner := msg.BuildParts(Version, false)
		var err error
		if raw, err = e.PGP.Seal(msg, outer, inner); err != nil {
			return nil, err
		}
	} else {
		raw = msg.Build(Version)
	}
	if max := e.Cfg.ServiceProfile.MaxMessageBytes; max > 0 && int64(len(raw)) > max {
		e.logf("Warnung: Nachricht ist %d Bytes und überschreitet das konfigurierte Limit von %d Bytes – der Server lehnt sie möglicherweise ab", len(raw), max)
	}
	meta, err := e.Store.Add(store.Outbox, raw, msg.Identity, true)
	if err != nil {
		return nil, err
	}
	job := &uucp.Job{
		Recipients: msg.EnvelopeRecipients(),
		Identity:   msg.Identity,
		StoreID:    meta.ID,
		MessageID:  msg.MessageID,
	}
	spoolData := compose.SpoolData(raw, msg.Identity, msg.Date)
	if err := e.Spool.Enqueue(spoolData, job); err != nil {
		_ = e.Store.Delete(store.Outbox, meta.ID)
		return nil, err
	}
	return meta, nil
}

// EnqueueControl reiht einen uuctl-Self-Service-Request ein (z.B. "GET quota").
// Reiner Steuerjob: keine Outbox-Kopie, keine Envelope-Zeile – uuctl liest
// die Request-Zeile roh von stdin; die Antwort kommt als normale Mail an die
// Node-Hauptadresse (zwei Syncs: einreichen, dann abholen).
func (e *Engine) EnqueueControl(request string) error {
	request = strings.TrimSpace(request)
	if request == "" {
		return fmt.Errorf("leerer Steuer-Request")
	}
	job := &uucp.Job{Command: "uuctl"}
	if err := e.Spool.Enqueue([]byte(request+"\n"), job); err != nil {
		return err
	}
	e.logf("Steuer-Anfrage eingereiht: %s", request)
	return nil
}

// Run verbindet per SSH und fährt die komplette UUCP-Session.
func (e *Engine) Run() (*Result, error) {
	conn, err := sshtransport.Dial(e.Cfg)
	if err != nil {
		return &Result{}, err
	}
	defer conn.Close()
	e.logf("verbunden mit %s:%d", e.Cfg.ServerHost, e.Cfg.ServerPort)
	return e.RunOn(conn)
}

// RunOn fährt Handshake, Transfer und Shutdown über eine bestehende
// Verbindung (testbar über net.Pipe) und verarbeitet empfangene Jobs.
func (e *Engine) RunOn(conn io.ReadWriter) (*Result, error) {
	res := &Result{}
	wire := uucp.NewWire(conn)
	hs, err := uucp.Handshake(wire, e.Cfg.NodeName)
	if err != nil {
		return res, err
	}
	e.logf("handshake ok (Gegenstelle %s, Protokoll %c)", hs.RemoteName, hs.Protocol)

	session := &uucp.Session{
		Wire:      wire,
		NodeName:  e.Cfg.NodeName,
		Spool:     e.Spool,
		SizeNegot: hs.SizeNegot,
		V103:      hs.V103,
		Log:       e.Log,
	}
	sres, serr := session.Run()
	uucp.Shutdown(wire)

	// Erfolgreich übertragene Jobs: Outbox → Sent, Spool-Job löschen.
	for _, job := range sres.Sent {
		if job.StoreID != "" {
			if _, err := e.Store.Get(store.Outbox, job.StoreID); err != nil {
				// Outbox-Kopie existiert nicht mehr (z.B. verwaister Alt-Job) –
				// übertragen wurde trotzdem, nur aufräumen.
				e.logf("hinweis: Outbox-Kopie zu Job %s fehlt bereits", job.ID)
			} else if err := e.Store.Move(store.Outbox, job.StoreID, store.Sent); err != nil {
				e.logf("konnte %s nicht nach Gesendet verschieben: %v", job.StoreID, err)
			}
		}
		if err := e.Spool.Complete(job); err != nil {
			e.logf("spool-aufräumen für Job %s fehlgeschlagen: %v", job.ID, err)
		}
		res.SentCount++
	}
	for _, job := range sres.Failed {
		if job.StoreID != "" {
			_ = e.Store.SetError(store.Outbox, job.StoreID, job.LastError)
		}
		res.FailedCount++
	}

	// Eingebautes uuxqt über alles, was im incoming-Spool liegt.
	n, qerr := e.RunUuxqt()
	res.ReceivedCount = n
	if serr != nil {
		return res, serr
	}
	return res, qerr
}

// RunUuxqt verarbeitet alle vollständigen X./D.-Paare im incoming-Spool:
// X-Datei parsen, nur rmail zulassen, D-Datei als Inbox-Mail ablegen.
// Unvollständige Paare bleiben für den nächsten Lauf liegen; unzulässige
// Jobs wandern in die Quarantäne. Idempotent.
func (e *Engine) RunUuxqt() (int, error) {
	dir := e.Spool.IncomingDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	count := 0
	var firstErr error
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "X.") {
			continue
		}
		xPath := filepath.Join(dir, name)
		if err := e.execOne(xPath); err != nil {
			if errors.Is(err, errIncomplete) {
				continue // D-Datei folgt in einer späteren Session
			}
			e.logf("uuxqt: %s → Quarantäne (%v)", name, err)
			_ = os.Rename(xPath, filepath.Join(e.Spool.QuarantineDir(), name))
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		count++
	}
	return count, firstErr
}

var errIncomplete = errors.New("zugehörige Datendatei fehlt noch")

func (e *Engine) execOne(xPath string) error {
	content, err := os.ReadFile(xPath)
	if err != nil {
		return err
	}
	x, err := uucp.ParseXFile(string(content))
	if err != nil {
		return err
	}
	rcpts, err := x.RmailRecipients()
	if err != nil {
		return err
	}
	dataName := x.Input
	if dataName == "" && len(x.Files) > 0 {
		dataName = x.Files[0]
	}
	if dataName == "" {
		return fmt.Errorf("execute-datei ohne Eingabedatei")
	}
	dataPath := filepath.Join(e.Spool.IncomingDir(), filepath.Base(dataName))
	raw, err := os.ReadFile(dataPath)
	if errors.Is(err, os.ErrNotExist) {
		return errIncomplete
	}
	if err != nil {
		return err
	}
	body, envFrom := compose.StripEnvelope(raw)
	if _, err := e.Store.Add(store.Inbox, body, envFrom, false); err != nil {
		return err
	}
	e.logf("neue Nachricht für %s (%d Bytes)", strings.Join(rcpts, ", "), len(body))
	// uuctl-Antworten zusätzlich in den persistierten Dienststatus übernehmen.
	if kind := parseUUCTLReply(body, &e.Cfg.ServiceStatus, time.Now()); kind != "" {
		if kind == "quota" && e.Cfg.ServiceStatus.MaxMessageBytes > 0 {
			// Senke: weiche Größenwarnung dem Server-Limit nachziehen.
			e.Cfg.ServiceProfile.MaxMessageBytes = e.Cfg.ServiceStatus.MaxMessageBytes
		}
		if err := e.Cfg.Save(); err != nil {
			e.logf("dienststatus speichern: %v", err)
		} else {
			e.logf("Dienststatus aktualisiert (%s, Tarif %s)", kind, e.Cfg.ServiceStatus.Tier)
		}
	}
	_ = os.Remove(dataPath)
	return os.Remove(xPath)
}

// CancelByStoreID entfernt wartende Spool-Jobs, die zur gelöschten
// Outbox-Nachricht gehören (verhindert verwaiste Sendungen).
func (e *Engine) CancelByStoreID(storeID string) error {
	jobs, err := e.Spool.Jobs()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.StoreID == storeID {
			if err := e.Spool.Complete(j); err != nil {
				return err
			}
		}
	}
	return nil
}

// QueueStatus beschreibt die wartenden Jobs für die CLI/UI.
func (e *Engine) QueueStatus() (string, error) {
	jobs, err := e.Spool.Jobs()
	if err != nil {
		return "", err
	}
	if len(jobs) == 0 {
		return "Outbox/Spool ist leer.\n", nil
	}
	var sb strings.Builder
	for _, j := range jobs {
		target := strings.Join(j.Recipients, ", ")
		if j.Command != "" {
			target = "[" + j.Command + "]"
		}
		fmt.Fprintf(&sb, "%s  %s  an %s  (Versuche: %d)",
			j.Created.Format(time.DateTime), j.ID, target, j.Attempts)
		if j.LastError != "" {
			fmt.Fprintf(&sb, "  Fehler: %s", j.LastError)
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}
