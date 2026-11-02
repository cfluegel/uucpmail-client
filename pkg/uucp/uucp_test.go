package uucp

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
)

// pipePair liefert zwei verbundene Wire-Enden über net.Pipe.
func pipePair() (*Wire, *Wire, func()) {
	a, b := net.Pipe()
	return NewWire(a), NewWire(b), func() { a.Close(); b.Close() }
}

// readerWire baut einen Wire über einem reinen Reader (für Decodier-Tests).
func readerWire(r io.Reader) *Wire {
	return NewWire(struct {
		io.Reader
		io.Writer
	}{r, io.Discard})
}

func TestHandshakeOK(t *testing.T) {
	client, server, closeAll := pipePair()
	defer closeAll()

	errc := make(chan error, 1)
	go func() {
		caller, err := AnswerHandshake(server, "uucpsrv", "u7a2f4c")
		if err == nil && caller != "u7a2f4c" {
			t.Errorf("caller = %q", caller)
		}
		errc <- err
	}()
	res, err := Handshake(client, "u7a2f4c")
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("AnswerHandshake: %v", err)
	}
	if res.RemoteName != "uucpsrv" || res.Protocol != 't' || !res.SizeNegot || res.V103 {
		t.Errorf("unerwartetes Ergebnis: %+v", res)
	}
}

func TestHandshakeUnknownNode(t *testing.T) {
	client, server, closeAll := pipePair()
	defer closeAll()
	go AnswerHandshake(server, "uucpsrv", "andererknoten")
	_, err := Handshake(client, "u7a2f4c")
	if err != ErrUnknown {
		t.Fatalf("err = %v, erwartet ErrUnknown", err)
	}
}

func TestHandshakeErrors(t *testing.T) {
	cases := []struct {
		reply string
		want  error
	}{
		{"RLCK", ErrLocked},
		{"RBADSEQ", ErrBadSeq},
	}
	for _, c := range cases {
		client, server, closeAll := pipePair()
		go func(reply string) {
			writeHandshakeString(server.W, "Shere=uucpsrv")
			readHandshakeString(server.R)
			writeHandshakeString(server.W, reply)
		}(c.reply)
		_, err := Handshake(client, "u7a2f4c")
		if err != c.want {
			t.Errorf("reply %s: err = %v, erwartet %v", c.reply, err, c.want)
		}
		closeAll()
	}
}

func TestHandshakeNoProtocolT(t *testing.T) {
	client, server, closeAll := pipePair()
	defer closeAll()
	go func() {
		writeHandshakeString(server.W, "Shere=uucpsrv")
		readHandshakeString(server.R)
		writeHandshakeString(server.W, "ROK")
		writeHandshakeString(server.W, "Pg")
		readHandshakeString(server.R) // UN
	}()
	_, err := Handshake(client, "u7a2f4c")
	if !errors.Is(err, ErrNoProtocol) {
		t.Fatalf("err = %v, erwartet ErrNoProtocol", err)
	}
}

func TestCommandRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	w := &Wire{W: &buf}
	cmd := "S D.u7a2fC1a2b D.u7a2fC1a2b uucpmail -C D.u7a2fC1a2b 0666 \"\" 0x4d2"
	if err := w.WriteCommand(cmd); err != nil {
		t.Fatal(err)
	}
	if buf.Len()%tCmdBlock != 0 {
		t.Errorf("command nicht auf %d-Byte-Block gepolstert: %d", tCmdBlock, buf.Len())
	}
	r := readerWire(iotest.OneByteReader(&buf))
	got, err := r.ReadCommand()
	if err != nil {
		t.Fatal(err)
	}
	if got != cmd {
		t.Errorf("got %q", got)
	}
}

func TestLongCommandRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	w := &Wire{W: &buf}
	cmd := "S " + strings.Repeat("x", 1200) // > 2 Blöcke
	if err := w.WriteCommand(cmd); err != nil {
		t.Fatal(err)
	}
	r := readerWire(bytes.NewReader(buf.Bytes()))
	got, err := r.ReadCommand()
	if err != nil || got != cmd {
		t.Fatalf("got len=%d err=%v", len(got), err)
	}
}

func TestFileRoundtrip(t *testing.T) {
	payload := bytes.Repeat([]byte("uucpmail "), 700) // > 1 Record, nicht aligned
	var buf bytes.Buffer
	w := &Wire{W: &buf}
	n, err := w.SendFile(iotest.OneByteReader(bytes.NewReader(payload)))
	if err != nil || n != int64(len(payload)) {
		t.Fatalf("SendFile n=%d err=%v", n, err)
	}
	r := readerWire(iotest.HalfReader(bytes.NewReader(buf.Bytes())))
	var out bytes.Buffer
	rn, err := r.ReceiveFile(&out)
	if err != nil || rn != int64(len(payload)) {
		t.Fatalf("ReceiveFile n=%d err=%v", rn, err)
	}
	if !bytes.Equal(out.Bytes(), payload) {
		t.Error("payload verändert")
	}
}

func TestEmptyFileRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	w := &Wire{W: &buf}
	if n, err := w.SendFile(bytes.NewReader(nil)); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	r := readerWire(bytes.NewReader(buf.Bytes()))
	var out bytes.Buffer
	if n, err := r.ReceiveFile(&out); err != nil || n != 0 || out.Len() != 0 {
		t.Fatalf("n=%d err=%v len=%d", n, err, out.Len())
	}
}

func TestReceiveFileRecordLimit(t *testing.T) {
	raw := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	r := readerWire(bytes.NewReader(raw))
	if _, err := r.ReceiveFile(io.Discard); err == nil {
		t.Fatal("überlange Record-Länge nicht abgelehnt")
	}
}

func TestParseSendRequest(t *testing.T) {
	req, err := ParseSendRequest(`S D.srv123 X.srv123 uucp -C D.srv123 0666 "" 0x1a`)
	if err != nil {
		t.Fatal(err)
	}
	if req.From != "D.srv123" || req.To != "X.srv123" || req.User != "uucp" ||
		req.Options != "C" || req.Temp != "D.srv123" || req.Mode != "0666" || req.Size != 26 {
		t.Errorf("%+v", req)
	}
	// Format-Roundtrip
	if got := req.Format(true, false); got != `S D.srv123 X.srv123 uucp -C D.srv123 0666 "" 0x1a` {
		t.Errorf("Format: %q", got)
	}
	if got := req.Format(false, false); strings.Contains(got, "0x1a") {
		t.Errorf("Format ohne Size enthält Size: %q", got)
	}
	if got := req.Format(true, true); !strings.HasSuffix(got, `"" 26`) {
		t.Errorf("V103-Format nicht dezimal: %q", got)
	}
}

func TestParseReply(t *testing.T) {
	for _, c := range []struct {
		in   string
		kind byte
		ok   bool
	}{
		{"SY", 'S', true}, {"SN2", 'S', false}, {"CY", 'C', true},
		{"CN5", 'C', false}, {"HY", 'H', true}, {"HN", 'H', false}, {"XN", 'X', false},
	} {
		r, err := ParseReply(c.in)
		if err != nil || r.Kind != c.kind || r.OK != c.ok {
			t.Errorf("%s: %+v err=%v", c.in, r, err)
		}
	}
	if _, err := ParseReply("Q?"); err == nil {
		t.Error("ungültige Antwort akzeptiert")
	}
}

func TestSNReason(t *testing.T) {
	if !strings.Contains(SNReason("4"), "out of space") {
		t.Error("SN4 nicht erkannt")
	}
}

func TestValidNodeName(t *testing.T) {
	valid := []string{"u4f2410", "ub61af8", "node-1", "a.b_c", "U7A2F4C"}
	for _, s := range valid {
		if !ValidNodeName(s) {
			t.Errorf("ValidNodeName(%q) = false, erwartet true", s)
		}
	}
	invalid := []string{"", "u4f2410 ", " u4f2410", "u4f 2410", "u4f\t2410", "u4f2410\n", "näme", "a/b"}
	for _, s := range invalid {
		if ValidNodeName(s) {
			t.Errorf("ValidNodeName(%q) = true, erwartet false", s)
		}
	}
}

func TestUpdateErrorPersistsKey(t *testing.T) {
	sp, err := OpenSpool(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	job := &Job{Recipients: []string{"a@b"}}
	if err := sp.Enqueue([]byte("x"), job); err != nil {
		t.Fatal(err)
	}
	if err := sp.UpdateError(job, i18n.Errorf("err.uucp.rejected", "RX")); err != nil {
		t.Fatal(err)
	}
	jobs, err := sp.Jobs()
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs: %v %v", jobs, err)
	}
	if jobs[0].LastErrorKey != "err.uucp.rejected" || len(jobs[0].LastErrorArgs) != 1 || jobs[0].LastErrorArgs[0] != "RX" {
		t.Fatalf("Key/Args: %q %v", jobs[0].LastErrorKey, jobs[0].LastErrorArgs)
	}
	if got := jobs[0].LocalizedError(); got != `handshake rejected: "RX"` {
		t.Fatalf("LocalizedError: %q", got)
	}
	// Nicht-lokalisierter Fehler → nur Fallback-Text.
	if err := sp.UpdateError(job, errors.New("plain")); err != nil {
		t.Fatal(err)
	}
	jobs, _ = sp.Jobs()
	if jobs[0].LastErrorKey != "" || jobs[0].LocalizedError() != "plain" {
		t.Fatalf("Fallback: %q %q", jobs[0].LastErrorKey, jobs[0].LocalizedError())
	}
}
