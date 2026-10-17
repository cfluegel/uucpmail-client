// Package uucp implementiert die vom uucpmail.de-Client benötigte Teilmenge
// des Taylor-UUCP-Wire-Protokolls: Initial-Handshake und Protokoll t über
// einen zuverlässigen Byte-Kanal (SSH).
package uucp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Handshake-Strings sind \x10…\x00 gerahmt (DLE … NUL).
const (
	dle = 0x10
	nul = 0x00
)

// maxHandshakeString begrenzt defensiv die Länge eines Handshake-Strings.
const maxHandshakeString = 256

// Wire bündelt den gepufferten Lese- und den Schreibkanal; derselbe Reader
// muss über Handshake und Protokoll t hinweg verwendet werden, damit keine
// gepufferten Bytes verloren gehen.
type Wire struct {
	R *bufio.Reader
	W io.Writer
}

func NewWire(rw io.ReadWriter) *Wire {
	return &Wire{R: bufio.NewReader(rw), W: rw}
}

// Typisierte Handshake-Fehler für verständliche UI-Meldungen.
var (
	ErrLocked     = errors.New("server meldet RLCK: Node ist bereits verbunden oder Session-Limit erreicht – später erneut versuchen")
	ErrUnknown    = errors.New("server kennt diesen Node nicht (»You are unknown to me«) – Registrierung/Serverkonfiguration prüfen")
	ErrBadSeq     = errors.New("server meldet RBADSEQ: Sequenznummern-Konflikt")
	ErrNoProtocol = errors.New("server bietet Protokoll t nicht an")
)

// WriteString sendet einen DLE-gerahmten Handshake-String.
func writeHandshakeString(w io.Writer, s string) error {
	buf := make([]byte, 0, len(s)+2)
	buf = append(buf, dle)
	buf = append(buf, s...)
	buf = append(buf, nul)
	_, err := w.Write(buf)
	return err
}

// readHandshakeString liest den nächsten DLE-gerahmten String und ignoriert
// Rauschen vor dem DLE (Taylor toleriert das ebenso).
func readHandshakeString(r *bufio.Reader) (string, error) {
	for {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == dle {
			break
		}
	}
	var sb strings.Builder
	for {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == nul || b == '\r' || b == '\n' {
			return sb.String(), nil
		}
		// Ein erneutes DLE startet den String neu (abgebrochener Frame).
		if b == dle {
			sb.Reset()
			continue
		}
		if sb.Len() >= maxHandshakeString {
			return "", fmt.Errorf("handshake-string länger als %d Bytes", maxHandshakeString)
		}
		sb.WriteByte(b)
	}
}

// HandshakeResult hält die ausgehandelten Eigenschaften.
type HandshakeResult struct {
	RemoteName string // z.B. "uucpsrv"
	Protocol   byte   // ausgehandeltes Protokoll, bei uns immer 't'
	SizeNegot  bool   // true, wenn der Server das Size-Feature bestätigt hat (ROKN…)
	V103       bool   // Alt-Taylor: Größen dezimal statt 0x-hex kodieren
}

// featureSizes ist das einzige Taylor-Feature-Bit, das wir anbieten
// (FEATURE_SIZES = 01). Kein RESTART (02, nicht implementiert), kein
// EXEC (04, wir wollen X.-Dateien statt E-Commands), kein QUOTES (040).
const featureSizes = 0o1

// Handshake fährt den uucico-Initial-Handshake als Caller (Taylor 1.07,
// uucico.c fdo_call/faccept_call):
//
//	← Shere=<remote>
//	→ S<node> -Q0 -N01        (-N mit Oktal-Feature-Bits)
//	← ROKN<bits> / ROKN / ROK (oder RLCK/RLOGIN/RBADSEQ/RYou are unknown to me)
//	← P<protos>
//	→ Ut
func Handshake(w *Wire, nodeName string) (*HandshakeResult, error) {
	r := w.R
	res := &HandshakeResult{}

	here, err := readHandshakeString(r)
	if err != nil {
		return nil, fmt.Errorf("kein uucico-Banner empfangen: %w", err)
	}
	if !strings.HasPrefix(here, "Shere") {
		return nil, fmt.Errorf("unerwartetes Banner %q (erwartet Shere=…)", here)
	}
	if rest, ok := strings.CutPrefix(here, "Shere="); ok {
		res.RemoteName = rest
	}

	login := fmt.Sprintf("S%s -Q0 -N0%o", nodeName, featureSizes)
	if err := writeHandshakeString(w.W, login); err != nil {
		return nil, err
	}

	reply, err := readHandshakeString(r)
	if err != nil {
		return nil, fmt.Errorf("keine Antwort auf Login-String: %w", err)
	}
	switch {
	case strings.HasPrefix(reply, "ROKN"):
		// Feature-Bits hängen direkt an ROKN (oktal/hex/dezimal, strtol-Basis 0).
		rest := strings.TrimSpace(reply[4:])
		if rest == "" {
			// Alt-Taylor (V103): Größen werden dezimal übertragen.
			res.SizeNegot = true
			res.V103 = true
		} else if bits, err := strconv.ParseInt(rest, 0, 64); err == nil {
			res.SizeNegot = bits&featureSizes != 0
		}
	case strings.HasPrefix(reply, "ROK"):
		// Plain ROK (ggf. mit SVR4 " -R"): keine Größen-Negotiation nutzen.
	case strings.HasPrefix(reply, "RLCK"):
		return nil, ErrLocked
	case strings.HasPrefix(reply, "RLOGIN"):
		return nil, ErrUnknown
	case strings.HasPrefix(reply, "RBADSEQ"):
		return nil, ErrBadSeq
	case strings.HasPrefix(reply, "RYou are unknown"):
		return nil, ErrUnknown
	default:
		return nil, fmt.Errorf("handshake abgelehnt: %q", reply)
	}

	protos, err := readHandshakeString(r)
	if err != nil {
		return nil, fmt.Errorf("keine Protokollliste empfangen: %w", err)
	}
	if !strings.HasPrefix(protos, "P") || !strings.ContainsRune(protos[1:], 't') {
		if werr := writeHandshakeString(w.W, "UN"); werr != nil {
			return nil, werr
		}
		return nil, fmt.Errorf("%w (angeboten: %q)", ErrNoProtocol, protos)
	}
	if err := writeHandshakeString(w.W, "Ut"); err != nil {
		return nil, err
	}
	res.Protocol = 't'
	return res, nil
}

// AnswerHandshake fährt die Serverseite des Handshakes (für den fakeserver in Tests).
func AnswerHandshake(w *Wire, serverName string, wantCaller string) (string, error) {
	if err := writeHandshakeString(w.W, "Shere="+serverName); err != nil {
		return "", err
	}
	r := w.R
	login, err := readHandshakeString(r)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(login, "S") {
		return "", fmt.Errorf("unerwarteter Login-String %q", login)
	}
	fields := strings.Fields(login[1:])
	if len(fields) == 0 {
		return "", errors.New("leerer Login-String")
	}
	caller := fields[0]
	if wantCaller != "" && caller != wantCaller {
		_ = writeHandshakeString(w.W, "RYou are unknown to me")
		return caller, ErrUnknown
	}
	if err := writeHandshakeString(w.W, fmt.Sprintf("ROKN0%o", featureSizes)); err != nil {
		return caller, err
	}
	if err := writeHandshakeString(w.W, "Pt"); err != nil {
		return caller, err
	}
	use, err := readHandshakeString(r)
	if err != nil {
		return caller, err
	}
	if use != "Ut" {
		return caller, fmt.Errorf("client wählte %q statt Ut", use)
	}
	return caller, nil
}

// Shutdown sendet das abschließende "OOOOOO" und liest die Gegenseite tolerant
// mit kurzem Timeout (Verluste hier sind unkritisch).
func Shutdown(w *Wire) {
	// Taylor-Caller sendet die O-Kette doppelt (uucico.c ~1664).
	_ = writeHandshakeString(w.W, "OOOOOO")
	_ = writeHandshakeString(w.W, "OOOOOO")
	buf := make([]byte, 64)
	done := make(chan struct{})
	go func() { _, _ = w.R.Read(buf); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}
