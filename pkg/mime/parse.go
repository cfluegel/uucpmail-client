// Package mailmime parst eingehende RFC-5322/MIME-Nachrichten für die Anzeige:
// Multipart-Bäume (alternative/mixed, verschachtelt), Content-Transfer-Encodings
// (quoted-printable, base64) und gängige Charsets. Fehler degradieren immer zu
// einem Teilergebnis bzw. zur Rohansicht – eine Mail geht nie verloren.
package mailmime

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
)

const (
	maxDepth = 10
	maxParts = 100
	// maxPartSize begrenzt dekodierte Einzelteile defensiv (Server-Limit ist 25 MB).
	maxPartSize = 64 * 1024 * 1024
)

// Part ist ein dekodiertes Blatt des MIME-Baums.
type Part struct {
	MediaType   string // z.B. "image/png"
	Filename    string
	Data        []byte // dekodiert (CTE; bei text/* auch Charset → UTF-8)
	Disposition string
}

// ParsedMessage ist das Anzeigemodell einer Mail.
type ParsedMessage struct {
	Header      mail.Header
	TextPlain   string // bester text/plain-Part ("" = keiner)
	TextHTML    string // bester text/html-Part ("" = keiner)
	Attachments []Part

	// OpenPGP-Strukturen (Erkennung hier, Krypto in internal/pgp / UI):
	Encrypted        bool   // multipart/encrypted oder Inline-PGP-MESSAGE
	EncryptedPayload []byte // Armor-Block unverändert
	Signed           bool   // multipart/signed
	SignedPart       []byte // EXAKTE Rohbytes des signierten Entity (Header+Body)
	Signature        []byte // abgetrennte Armor-Signatur
	Clearsigned      bool   // Inline BEGIN PGP SIGNED MESSAGE (SignedPart = Rohtext)
}

type walker struct {
	msg   *ParsedMessage
	parts int
}

// Parse zerlegt eine Rohnachricht. Ein Fehler kommt nur zurück, wenn schon der
// Header-Block unlesbar ist – dann zeigt die UI den Rohtext.
func Parse(raw []byte) (*ParsedMessage, error) {
	m, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("header nicht parsbar: %w", err)
	}
	pm := &ParsedMessage{Header: m.Header}
	body, err := io.ReadAll(io.LimitReader(m.Body, maxPartSize))
	if err != nil && len(body) == 0 {
		return pm, nil
	}
	w := &walker{msg: pm}

	// PGP/MIME auf oberster Ebene erkennen, BEVOR der generische Walk läuft.
	topType, topParams, _ := mime.ParseMediaType(m.Header.Get("Content-Type"))
	switch strings.ToLower(topType) {
	case "multipart/encrypted":
		if payload := extractEncryptedPayload(body, topParams["boundary"]); payload != nil {
			pm.Encrypted = true
			pm.EncryptedPayload = payload
			return pm, nil
		}
	case "multipart/signed":
		if signed, sig, ok := splitSignedRaw(body, topParams["boundary"]); ok {
			pm.Signed = true
			pm.SignedPart = signed
			pm.Signature = sig
			// Das signierte Entity für die Anzeige normal parsen.
			w.walkEntity(signed, 0)
			return pm, nil
		}
	}

	w.walkPart(m.Header.Get("Content-Type"), m.Header.Get("Content-Transfer-Encoding"),
		m.Header.Get("Content-Disposition"), bytes.NewReader(body), 0)
	detectInlinePGP(pm)
	return pm, nil
}

// walkEntity parst ein rohes MIME-Entity (eigener Header-Block + Body).
func (w *walker) walkEntity(raw []byte, depth int) {
	tp := textproto.NewReader(bufio.NewReader(bytes.NewReader(raw)))
	hdr, err := tp.ReadMIMEHeader()
	if err != nil {
		// Kein Header-Block: als nackten Text behandeln.
		w.walkPart("text/plain", "", "", bytes.NewReader(raw), depth)
		return
	}
	w.walkPart(hdr.Get("Content-Type"), hdr.Get("Content-Transfer-Encoding"),
		hdr.Get("Content-Disposition"), tp.R, depth)
}

// extractEncryptedPayload holt den Armor-Block aus multipart/encrypted
// (Part application/octet-stream; Normalisierung ist hier unkritisch).
func extractEncryptedPayload(body []byte, boundary string) []byte {
	if boundary == "" {
		return nil
	}
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	for i := 0; i < maxParts; i++ {
		p, err := mr.NextPart()
		if err != nil {
			return nil
		}
		ct, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		data, _ := io.ReadAll(io.LimitReader(p, maxPartSize))
		if strings.EqualFold(ct, "application/octet-stream") ||
			bytes.Contains(data, []byte("BEGIN PGP MESSAGE")) {
			if block := extractArmor(data, "PGP MESSAGE"); block != nil {
				return block
			}
		}
	}
	return nil
}

// splitSignedRaw zerlegt multipart/signed TEXTUELL (kein multipart.Reader –
// der würde Header normalisieren und die Signatur ungültig machen).
// Liefert die exakten Rohbytes von Teil 1 und die Armor-Signatur aus Teil 2.
func splitSignedRaw(body []byte, boundary string) (signed, sig []byte, ok bool) {
	if boundary == "" {
		return nil, nil, false
	}
	// Zeilenenden-tolerant: Mail-Gateways (rmail/Postfix) normalisieren
	// CRLF zu LF – die Boundary wird daher über "\n--boundary" gesucht und
	// ein eventuelles vorangehendes \r dem Delimiter zugeschlagen.
	delim := []byte("\n--" + boundary)
	full := append([]byte("\n"), body...)
	segments := bytes.Split(full, delim)
	// segments[0] = Präambel; danach je Segment: Rest der Boundary-Zeile
	// (Zeilenende oder "--" + Abschluss) + Part-Inhalt.
	var parts [][]byte
	for _, seg := range segments[1:] {
		if bytes.HasPrefix(seg, []byte("--")) {
			break // Abschluss-Boundary
		}
		if idx := bytes.IndexByte(seg, '\n'); idx >= 0 {
			part := seg[idx+1:]
			// Ein \r vor dem nächsten Delimiter gehört zum CRLF-Delimiter.
			part = bytes.TrimSuffix(part, []byte("\r"))
			parts = append(parts, part)
		}
	}
	if len(parts) < 2 {
		return nil, nil, false
	}
	sigBlock := extractArmor(parts[1], "PGP SIGNATURE")
	if sigBlock == nil {
		return nil, nil, false
	}
	return parts[0], sigBlock, true
}

// extractArmor schneidet einen Armor-Block (BEGIN…END) aus Rohdaten.
func extractArmor(data []byte, blockType string) []byte {
	begin := []byte("-----BEGIN " + blockType + "-----")
	end := []byte("-----END " + blockType + "-----")
	i := bytes.Index(data, begin)
	if i < 0 {
		return nil
	}
	j := bytes.Index(data[i:], end)
	if j < 0 {
		return nil
	}
	return data[i : i+j+len(end)]
}

// detectInlinePGP erkennt Inline-PGP in text/plain-Bodies.
func detectInlinePGP(pm *ParsedMessage) {
	if pm.TextPlain == "" {
		return
	}
	if block := extractArmor([]byte(pm.TextPlain), "PGP MESSAGE"); block != nil {
		pm.Encrypted = true
		pm.EncryptedPayload = block
		pm.TextPlain = ""
		return
	}
	if strings.Contains(pm.TextPlain, "-----BEGIN PGP SIGNED MESSAGE-----") {
		pm.Clearsigned = true
		pm.SignedPart = []byte(pm.TextPlain)
	}
}

// walkPart verarbeitet einen (Teil-)Body anhand seiner Header rekursiv.
func (w *walker) walkPart(ctHeader, cte, disposition string, body io.Reader, depth int) {
	if depth > maxDepth || w.parts >= maxParts {
		return
	}
	w.parts++

	mediaType, params, err := mime.ParseMediaType(ctHeader)
	if err != nil || mediaType == "" {
		// Kaputter/fehlender Content-Type: wie text/plain behandeln.
		mediaType, params = "text/plain", map[string]string{}
	}
	mediaType = strings.ToLower(mediaType)

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return
		}
		mr := multipart.NewReader(body, boundary)
		for {
			p, err := mr.NextPart()
			if err != nil {
				// io.EOF = sauberes Ende; andere Fehler (fehlende End-Boundary
				// u.ä.): bereits gesammelte Teile behalten.
				return
			}
			w.walkPart(p.Header.Get("Content-Type"), p.Header.Get("Content-Transfer-Encoding"),
				p.Header.Get("Content-Disposition"), p, depth+1)
			if w.parts >= maxParts {
				return
			}
		}
	}

	data, err := decodeCTE(body, cte)
	if err != nil && len(data) == 0 {
		return // nichts dekodierbar – Teil überspringen
	}

	dispType, dispParams, _ := mime.ParseMediaType(disposition)
	filename := partFilename(dispParams, params, mediaType)
	isAttachment := strings.EqualFold(dispType, "attachment") ||
		filename != "" ||
		(!strings.HasPrefix(mediaType, "text/") && mediaType != "message/rfc822")

	if mediaType == "message/rfc822" {
		// Eingebettete Mail: als Anhang behandeln (bewusst kein Rekursieren).
		if filename == "" {
			filename = "message.eml"
		}
		isAttachment = true
	}

	if isAttachment {
		if filename == "" {
			filename = fmt.Sprintf("attachment-%d%s", len(w.msg.Attachments)+1, extFor(mediaType))
		}
		w.msg.Attachments = append(w.msg.Attachments, Part{
			MediaType: mediaType, Filename: filename, Data: data, Disposition: dispType,
		})
		return
	}

	text := decodeCharset(data, params["charset"])
	switch mediaType {
	case "text/plain":
		if w.msg.TextPlain == "" {
			w.msg.TextPlain = text
		}
	case "text/html":
		if w.msg.TextHTML == "" {
			w.msg.TextHTML = text
		}
	default:
		// Sonstige text/*-Typen (text/markdown, …): wie plain behandeln.
		if w.msg.TextPlain == "" {
			w.msg.TextPlain = text
		}
	}
}

// decodeCTE dekodiert den Content-Transfer-Encoding-Layer. Bei Fehlern mitten
// im Strom wird das bis dahin Dekodierte zurückgegeben.
func decodeCTE(r io.Reader, cte string) ([]byte, error) {
	limited := io.LimitReader(r, maxPartSize)
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "base64":
		// base64.NewDecoder toleriert \r\n; bei trailing garbage behalten wir
		// das bereits Dekodierte.
		data, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, newB64Filter(limited)))
		if err != nil && len(data) > 0 {
			return data, nil
		}
		return data, err
	case "quoted-printable":
		data, err := io.ReadAll(quotedprintable.NewReader(limited))
		if err != nil && len(data) > 0 {
			return data, nil
		}
		return data, err
	default: // 7bit, 8bit, binary, leer
		return io.ReadAll(limited)
	}
}

// b64Filter entfernt Whitespace aus dem Base64-Strom (robust gegen Einrückung).
type b64Filter struct{ r io.Reader }

func newB64Filter(r io.Reader) io.Reader { return &b64Filter{r} }

func (f *b64Filter) Read(p []byte) (int, error) {
	buf := make([]byte, len(p))
	for {
		n, err := f.r.Read(buf)
		out := 0
		for _, b := range buf[:n] {
			switch b {
			case '\r', '\n', ' ', '\t':
			default:
				p[out] = b
				out++
			}
		}
		if out > 0 || err != nil {
			return out, err
		}
	}
}

// partFilename ermittelt den Anhang-Dateinamen (RFC 2231/2047 via ParseMediaType
// bzw. WordDecoder bereits aufgelöst).
func partFilename(dispParams, ctParams map[string]string, mediaType string) string {
	name := dispParams["filename"]
	if name == "" {
		name = ctParams["name"]
	}
	if name == "" {
		return ""
	}
	if decoded, err := new(mime.WordDecoder).DecodeHeader(name); err == nil {
		name = decoded
	}
	// Pfadbestandteile entschärfen.
	name = strings.ReplaceAll(name, "\\", "/")
	if idx := strings.LastIndexByte(name, '/'); idx >= 0 {
		name = name[idx+1:]
	}
	return strings.TrimSpace(name)
}

func extFor(mediaType string) string {
	if exts, err := mime.ExtensionsByType(mediaType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

// HasAttachments prüft leichtgewichtig (ohne Bodies zu dekodieren), ob eine
// Mail Anhänge mitbringt – für die Index-Spalte.
func HasAttachments(raw []byte) bool {
	m, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return false
	}
	mediaType, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		return false
	}
	boundary := params["boundary"]
	if boundary == "" {
		return false
	}
	mr := multipart.NewReader(m.Body, boundary)
	for i := 0; i < maxParts; i++ {
		p, err := mr.NextPart()
		if err != nil {
			return false
		}
		disp, dp, _ := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
		if strings.EqualFold(disp, "attachment") || dp["filename"] != "" {
			return true
		}
		ct, cp, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		if cp["name"] != "" {
			return true
		}
		ctl := strings.ToLower(ct)
		if ct != "" && !strings.HasPrefix(ctl, "text/") && !strings.HasPrefix(ctl, "multipart/") {
			return true
		}
		_, _ = io.Copy(io.Discard, p)
	}
	return false
}
