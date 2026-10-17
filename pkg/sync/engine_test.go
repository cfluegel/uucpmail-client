package sync

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cfluegel/uucpmail-client/pkg/compose"
	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/store"
	"github.com/cfluegel/uucpmail-client/pkg/uucp"
	"github.com/cfluegel/uucpmail-client/test/fakeserver"
)

func newEngine(t *testing.T) *Engine {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "mail"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	spool, err := uucp.OpenSpool(filepath.Join(dir, "spool"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.NodeName = "u7a2f4c"
	cfg.Identities = []string{"test@uucpmail.de"}
	cfg.DefaultIdentity = "test@uucpmail.de"
	return &Engine{Cfg: cfg, Store: st, Spool: spool, Log: t.Logf}
}

func testMessage() *compose.Message {
	return &compose.Message{
		Identity: "test@uucpmail.de",
		To:       []string{"ziel@example.com"},
		Subject:  "Hallo Wölfe", // non-ASCII → RFC-2047-Pfad
		Body:     "# Überschrift\n\nText.",
		NodeName: "u7a2f4c",
	}
}

// run fährt Engine und fakeserver über net.Pipe gegeneinander.
func run(t *testing.T, e *Engine, srv *fakeserver.Server) (*Result, error, error) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Serve(serverConn) }()
	res, err := e.RunOn(clientConn)
	clientConn.Close()
	return res, err, <-srvErr
}

func TestSendSuccess(t *testing.T) {
	e := newEngine(t)
	meta, err := e.Enqueue(testMessage())
	if err != nil {
		t.Fatal(err)
	}
	srv := &fakeserver.Server{WantCaller: "u7a2f4c"}
	res, err, serr := run(t, e, srv)
	if err != nil || serr != nil {
		t.Fatalf("client err=%v server err=%v", err, serr)
	}
	if res.SentCount != 1 || res.FailedCount != 0 {
		t.Fatalf("res=%+v", res)
	}
	if len(srv.Received) != 2 {
		t.Fatalf("server empfing %d Dateien", len(srv.Received))
	}
	data, xfile := srv.Received[0], srv.Received[1]
	if !strings.HasPrefix(data.Name, "D.u7a2f4cC") || !strings.HasPrefix(xfile.Name, "X.u7a2f4cC") {
		t.Errorf("wire-namen: %q, %q", data.Name, xfile.Name)
	}
	if !bytes.HasPrefix(data.Data, []byte("From test@uucpmail.de ")) {
		t.Errorf("envelope-Zeile fehlt: %q", data.Data[:50])
	}
	if !strings.Contains(string(data.Data), "Subject: =?utf-8?q?") {
		t.Errorf("Subject nicht RFC-2047-kodiert")
	}
	x, err := uucp.ParseXFile(string(xfile.Data))
	if err != nil {
		t.Fatal(err)
	}
	if rcpts, err := x.RmailRecipients(); err != nil || rcpts[0] != "ziel@example.com" {
		t.Errorf("xfile: %+v err=%v", x, err)
	}
	// Outbox → Sent, Spool leer.
	if _, err := e.Store.Get(store.Sent, meta.ID); err != nil {
		t.Errorf("nachricht nicht in Sent: %v", err)
	}
	if jobs, _ := e.Spool.Jobs(); len(jobs) != 0 {
		t.Errorf("spool nicht leer: %d Jobs", len(jobs))
	}
}

func TestBccInEnvelopeNotHeaders(t *testing.T) {
	e := newEngine(t)
	msg := testMessage()
	msg.Cc = []string{"cc@example.com"}
	msg.Bcc = []string{"geheim@example.com"}
	if _, err := e.Enqueue(msg); err != nil {
		t.Fatal(err)
	}
	srv := &fakeserver.Server{WantCaller: "u7a2f4c"}
	_, err, serr := run(t, e, srv)
	if err != nil || serr != nil {
		t.Fatalf("client err=%v server err=%v", err, serr)
	}
	data, xfile := srv.Received[0], srv.Received[1]
	x, err := uucp.ParseXFile(string(xfile.Data))
	if err != nil {
		t.Fatal(err)
	}
	rcpts, err := x.RmailRecipients()
	if err != nil || len(rcpts) != 3 {
		t.Fatalf("rmail argv: %v err=%v", rcpts, err)
	}
	if !strings.Contains(strings.Join(rcpts, " "), "geheim@example.com") {
		t.Error("BCC fehlt im Envelope")
	}
	if strings.Contains(string(data.Data), "geheim@example.com") {
		t.Error("BCC steht in den Headern/Daten")
	}
	if !strings.Contains(string(data.Data), "Cc: cc@example.com") {
		t.Error("Cc-Header fehlt")
	}
}

func TestSendRejected(t *testing.T) {
	e := newEngine(t)
	meta, err := e.Enqueue(testMessage())
	if err != nil {
		t.Fatal(err)
	}
	srv := &fakeserver.Server{RejectUploads: "4"}
	res, err, serr := run(t, e, srv)
	if err != nil || serr != nil {
		t.Fatalf("client err=%v server err=%v", err, serr)
	}
	if res.FailedCount != 1 || res.SentCount != 0 {
		t.Fatalf("res=%+v", res)
	}
	// Nachricht bleibt in Outbox mit Fehlertext, Job bleibt im Spool.
	m, err := e.Store.Get(store.Outbox, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.LastError, "Platz") {
		t.Errorf("LastError = %q", m.LastError)
	}
	jobs, _ := e.Spool.Jobs()
	if len(jobs) != 1 || jobs[0].Attempts != 1 {
		t.Fatalf("jobs=%+v", jobs)
	}
}

func TestConnectionDropKeepsJob(t *testing.T) {
	e := newEngine(t)
	if _, err := e.Enqueue(testMessage()); err != nil {
		t.Fatal(err)
	}
	srv := &fakeserver.Server{DropAfterUploads: 1}
	_, err, _ := run(t, e, srv)
	if err == nil {
		t.Fatal("abgerissene Session nicht als Fehler gemeldet")
	}
	if jobs, _ := e.Spool.Jobs(); len(jobs) != 1 {
		t.Fatalf("job nach Abbruch verschwunden")
	}
	if msgs, _ := e.Store.List(store.Outbox); len(msgs) != 1 {
		t.Fatal("outbox-nachricht nach Abbruch verschwunden")
	}
}

func TestReceiveInbound(t *testing.T) {
	e := newEngine(t)
	mail := "From: wer@example.com\r\nTo: test@uucpmail.de\r\nSubject: Ping\r\nDate: Mon, 12 Oct 2026 10:00:00 +0200\r\n\r\nHallo!\r\n"
	srv := &fakeserver.Server{
		Outbound: []fakeserver.OutboundJob{{
			DataName: "D.uucpsrvC0001",
			XName:    "X.uucpsrvC0001",
			Data:     []byte("From wer@example.com Mon Oct 12 08:00:00 2026\n" + mail),
			Rcpt:     "test@uucpmail.de",
		}},
	}
	res, err, serr := run(t, e, srv)
	if err != nil || serr != nil {
		t.Fatalf("client err=%v server err=%v", err, serr)
	}
	if res.ReceivedCount != 1 {
		t.Fatalf("res=%+v", res)
	}
	msgs, err := e.Store.List(store.Inbox)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("inbox: %d err=%v", len(msgs), err)
	}
	m := msgs[0]
	if m.Read || m.Subject != "Ping" || m.EnvelopeFrom != "wer@example.com" {
		t.Errorf("meta: %+v", m)
	}
	raw, err := e.Store.Raw(store.Inbox, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != mail {
		t.Errorf("inhalt nicht byte-identisch:\n%q\n%q", raw, mail)
	}
	// Spool incoming muss leer sein.
	entries, _ := os.ReadDir(e.Spool.IncomingDir())
	if len(entries) != 0 {
		t.Errorf("incoming nicht leer: %d", len(entries))
	}
}

func TestUuxqtQuarantinesForbiddenCommand(t *testing.T) {
	e := newEngine(t)
	srv := &fakeserver.Server{
		Outbound: []fakeserver.OutboundJob{{
			DataName: "D.uucpsrvC0002",
			XName:    "X.uucpsrvC0002",
			Data:     []byte("egal"),
			XContent: "U uucp uucpsrv\nF D.uucpsrvC0002\nI D.uucpsrvC0002\nC rm -rf /\n",
		}},
	}
	res, err, serr := run(t, e, srv)
	if serr != nil {
		t.Fatalf("server err=%v", serr)
	}
	if err == nil {
		t.Fatal("verbotenes Kommando nicht als Fehler gemeldet")
	}
	if res.ReceivedCount != 0 {
		t.Fatalf("res=%+v", res)
	}
	q, _ := os.ReadDir(e.Spool.QuarantineDir())
	if len(q) != 1 {
		t.Fatalf("quarantäne: %d Einträge", len(q))
	}
	if msgs, _ := e.Store.List(store.Inbox); len(msgs) != 0 {
		t.Fatal("verbotener Job landete in der Inbox")
	}
}

func TestSendAndReceiveCombined(t *testing.T) {
	e := newEngine(t)
	if _, err := e.Enqueue(testMessage()); err != nil {
		t.Fatal(err)
	}
	srv := &fakeserver.Server{
		Outbound: []fakeserver.OutboundJob{{
			DataName: "D.uucpsrvC0003",
			XName:    "X.uucpsrvC0003",
			Data:     []byte("From a@b.de Mon Oct 12 08:00:00 2026\nFrom: a@b.de\r\n\r\nHi\r\n"),
			Rcpt:     "test@uucpmail.de",
		}},
	}
	res, err, serr := run(t, e, srv)
	if err != nil || serr != nil {
		t.Fatalf("client err=%v server err=%v", err, serr)
	}
	if res.SentCount != 1 || res.ReceivedCount != 1 {
		t.Fatalf("res=%+v", res)
	}
}

func TestRejectOnXFileKeepsJob(t *testing.T) {
	// Datendatei wird angenommen, erst die Execute-Datei abgelehnt (SN2):
	// Job muss mit Fehlertext liegen bleiben, nichts wandert nach Gesendet.
	e := newEngine(t)
	meta, err := e.Enqueue(testMessage())
	if err != nil {
		t.Fatal(err)
	}
	srv := &fakeserver.Server{RejectUploads: "2", AcceptUploads: 1}
	res, err, serr := run(t, e, srv)
	if err != nil || serr != nil {
		t.Fatalf("client err=%v server err=%v", err, serr)
	}
	if res.SentCount != 0 || res.FailedCount != 1 {
		t.Fatalf("res=%+v", res)
	}
	m, err := e.Store.Get(store.Outbox, meta.ID)
	if err != nil {
		t.Fatalf("outbox-kopie weg: %v", err)
	}
	if m.LastError == "" {
		t.Error("kein Fehlertext an der Outbox-Nachricht")
	}
	if jobs, _ := e.Spool.Jobs(); len(jobs) != 1 {
		t.Fatal("spool-job verschwunden")
	}
}

func TestConfirmFailureKeepsJob(t *testing.T) {
	// Übertragung wird mit CN5 quittiert: Job bleibt liegen.
	e := newEngine(t)
	meta, err := e.Enqueue(testMessage())
	if err != nil {
		t.Fatal(err)
	}
	srv := &fakeserver.Server{FailConfirm: true}
	res, err, serr := run(t, e, srv)
	if err != nil || serr != nil {
		t.Fatalf("client err=%v server err=%v", err, serr)
	}
	if res.SentCount != 0 || res.FailedCount != 1 {
		t.Fatalf("res=%+v", res)
	}
	m, _ := e.Store.Get(store.Outbox, meta.ID)
	if m == nil || !strings.Contains(m.LastError, "CN5") {
		t.Errorf("CN5 nicht im Fehlertext: %+v", m)
	}
}

func TestEnqueueRejectsInvalidRecipient(t *testing.T) {
	e := newEngine(t)
	msg := testMessage()
	msg.To = []string{"kaputt adresse ohne at"}
	if _, err := e.Enqueue(msg); err == nil {
		t.Fatal("ungültige Empfängeradresse nicht abgewiesen")
	}
	if msgs, _ := e.Store.List(store.Outbox); len(msgs) != 0 {
		t.Error("ungültige Mail landete trotzdem in der Outbox")
	}
}

type stubSealer struct{ called bool }

func (s *stubSealer) Seal(msg *compose.Message, outerHdr, inner []byte) ([]byte, error) {
	s.called = true
	return append([]byte("X-Sealed: yes\r\n"), append(outerHdr, inner...)...), nil
}

func TestEnqueueUsesSealer(t *testing.T) {
	e := newEngine(t)
	stub := &stubSealer{}
	e.PGP = stub
	msg := testMessage()
	msg.Sign = true
	meta, err := e.Enqueue(msg)
	if err != nil {
		t.Fatal(err)
	}
	if !stub.called {
		t.Fatal("Sealer nicht aufgerufen")
	}
	raw, _ := e.Store.Raw(store.Outbox, meta.ID)
	if !strings.HasPrefix(string(raw), "X-Sealed: yes") {
		t.Error("gesealte Bytes nicht im Store")
	}
	// Ohne Sealer → Fehler.
	e.PGP = nil
	msg2 := testMessage()
	msg2.Encrypt = true
	if _, err := e.Enqueue(msg2); err == nil {
		t.Error("Enqueue ohne Sealer nicht abgelehnt")
	}
}
