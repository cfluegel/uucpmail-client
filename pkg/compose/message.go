// Package compose erzeugt ausgehende RFC-5322-Nachrichten und die
// UUCP-Spool-Variante mit Envelope-Zeile.
package compose

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

// Attachment ist eine anzuhängende Datei (MIMEType wird beim Hinzufügen
// erkannt: Endung → Inhalts-Sniffing → application/octet-stream).
type Attachment struct {
	Filename string
	Data     []byte
	MIMEType string
}

// Message beschreibt eine zu versendende Nachricht.
type Message struct {
	Identity    string // Absenderadresse (Envelope + From:)
	To          []string
	Cc          []string
	Bcc         []string // nur Envelope, nie als Header
	Subject     string
	Body        string // Markdown als Klartext
	NodeName    string // für die Message-ID
	Date        time.Time
	MessageID   string
	InReplyTo   string   // Message-ID des Originals (Antworten)
	References  []string // Thread-Kette, wird auf die letzten 20 gekappt
	Attachments []Attachment
	Sign        bool // OpenPGP: signieren
	Encrypt     bool // OpenPGP: verschlüsseln (immer inkl. encrypt-to-self)
}

// EnvelopeRecipients liefert alle Zustell-Adressen (To+Cc+Bcc), dedupliziert
// (case-insensitive) in stabiler Reihenfolge – das rmail-argv.
func (m *Message) EnvelopeRecipients() []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{m.To, m.Cc, m.Bcc} {
		for _, r := range list {
			key := strings.ToLower(strings.TrimSpace(r))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, strings.TrimSpace(r))
		}
	}
	return out
}

// Validate prüft die Pflichtfelder und Adress-Syntax.
func (m *Message) Validate() error {
	if m.Identity == "" {
		return fmt.Errorf("keine Absenderadresse gewählt")
	}
	if _, err := mail.ParseAddress(m.Identity); err != nil {
		return fmt.Errorf("ungültige Absenderadresse %q: %w", m.Identity, err)
	}
	if len(m.EnvelopeRecipients()) == 0 {
		return fmt.Errorf("kein Empfänger angegeben")
	}
	for _, list := range [][]string{m.To, m.Cc, m.Bcc} {
		for _, r := range list {
			if _, err := mail.ParseAddress(r); err != nil {
				return fmt.Errorf("ungültige Empfängeradresse %q: %w", r, err)
			}
		}
	}
	return nil
}

// Build erzeugt die versandfertige RFC-5322-Nachricht (CRLF, ohne
// Envelope-Zeile) – so wird sie im Store (Outbox/Sent) abgelegt.
// Ohne Anhänge einfaches text/plain, sonst multipart/mixed.
func (m *Message) Build(version string) []byte {
	outer, inner := m.BuildParts(version, false)
	return Assemble(outer, inner)
}

// BuildDraft erzeugt die Entwurfs-Variante: zusätzlich private Header
// (X-Uucpmail-Bcc, X-Uucpmail-PGP), damit BCC und PGP-Schalter einen
// Entwurfs-Roundtrip überleben. Entwürfe verlassen den Rechner nie;
// Build() schreibt diese Header grundsätzlich nicht.
func (m *Message) BuildDraft(version string) []byte {
	outer, inner := m.BuildParts(version, true)
	return Assemble(outer, inner)
}

// Assemble fügt äußere Header und inneres MIME-Entity zur Nachricht zusammen.
func Assemble(outerHdr, inner []byte) []byte {
	out := make([]byte, 0, len(outerHdr)+len(inner)+20)
	out = append(out, outerHdr...)
	out = append(out, "MIME-Version: 1.0\r\n"...)
	out = append(out, inner...)
	return out
}

// BuildParts liefert die äußeren RFC-5322-Header (ohne MIME-Header) und das
// innere MIME-Entity (eigene Content-Type/CTE-Header + Body) getrennt – die
// PGP-Schicht signiert/verschlüsselt genau das innere Entity.
func (m *Message) BuildParts(version string, draft bool) (outerHdr, inner []byte) {
	if m.Date.IsZero() {
		m.Date = time.Now()
	}
	if m.MessageID == "" {
		host := m.NodeName
		if host == "" {
			host = "client"
		}
		// Message-ID-Domain muss auflösbar sein (Spam-Scoring); der Node
		// wandert in den lokalen Teil.
		m.MessageID = fmt.Sprintf("<%d.%s.%s@uucpmail.de>", m.Date.UnixNano(), randTag(), host)
	}
	var sb strings.Builder
	write := func(k, v string) { sb.WriteString(k + ": " + v + "\r\n") }
	write("From", m.Identity)
	write("To", strings.Join(m.To, ", "))
	if len(m.Cc) > 0 {
		write("Cc", strings.Join(m.Cc, ", "))
	}
	write("Subject", encodeSubject(m.Subject))
	write("Date", m.Date.Format(time.RFC1123Z))
	write("Message-ID", m.MessageID)
	if m.InReplyTo != "" {
		write("In-Reply-To", m.InReplyTo)
	}
	if refs := m.References; len(refs) > 0 {
		if len(refs) > 20 {
			refs = refs[len(refs)-20:]
		}
		write("References", strings.Join(refs, " "))
	}
	write("X-Mailer", "github.com/cfluegel/uucpmail-client/"+version)
	if draft {
		if len(m.Bcc) > 0 {
			write("X-Uucpmail-Bcc", strings.Join(m.Bcc, ", "))
		}
		var flags []string
		if m.Sign {
			flags = append(flags, "sign")
		}
		if m.Encrypt {
			flags = append(flags, "encrypt")
		}
		if len(flags) > 0 {
			write("X-Uucpmail-PGP", strings.Join(flags, ","))
		}
	}
	return []byte(sb.String()), m.buildInner()
}

// buildInner erzeugt das innere MIME-Entity (Header + Leerzeile + Body).
func (m *Message) buildInner() []byte {
	body := strings.ReplaceAll(m.Body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\n", "\r\n")
	if !strings.HasSuffix(body, "\r\n") {
		body += "\r\n"
	}

	var sb strings.Builder
	write := func(k, v string) { sb.WriteString(k + ": " + v + "\r\n") }

	if len(m.Attachments) == 0 {
		write("Content-Type", "text/plain; charset=utf-8")
		write("Content-Transfer-Encoding", "8bit")
		sb.WriteString("\r\n")
		sb.WriteString(body)
		return []byte(sb.String())
	}

	boundary := "uucpmail-" + randBoundary()
	write("Content-Type", fmt.Sprintf("multipart/mixed; boundary=%q", boundary))
	sb.WriteString("\r\n")

	mw := multipart.NewWriter(&sb)
	_ = mw.SetBoundary(boundary)
	textHdr := textproto.MIMEHeader{}
	textHdr.Set("Content-Type", "text/plain; charset=utf-8")
	textHdr.Set("Content-Transfer-Encoding", "8bit")
	if p, err := mw.CreatePart(textHdr); err == nil {
		_, _ = p.Write([]byte(body))
	}
	for _, att := range m.Attachments {
		mt := att.MIMEType
		if mt == "" {
			mt = "application/octet-stream"
		}
		name := encodeFilename(att.Filename)
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Type", fmt.Sprintf("%s; name=%q", mt, name))
		hdr.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		hdr.Set("Content-Transfer-Encoding", "base64")
		p, err := mw.CreatePart(hdr)
		if err != nil {
			continue
		}
		_, _ = p.Write(wrapBase64(att.Data))
	}
	_ = mw.Close()
	return []byte(sb.String())
}

// wrapBase64 kodiert Daten und bricht auf 76 Zeichen pro Zeile (CRLF) um.
func wrapBase64(data []byte) []byte {
	enc := base64.StdEncoding.EncodeToString(data)
	var out strings.Builder
	for len(enc) > 76 {
		out.WriteString(enc[:76])
		out.WriteString("\r\n")
		enc = enc[76:]
	}
	out.WriteString(enc)
	return []byte(out.String())
}

// encodeFilename RFC-2047-kodiert Nicht-ASCII-Dateinamen.
func encodeFilename(name string) string {
	for _, r := range name {
		if r > 126 || r < 32 {
			return mime.BEncoding.Encode("utf-8", name)
		}
	}
	return name
}

// EstimatedWireSize schätzt die Größe der gebauten Nachricht (für die
// Vorab-Warnung im Compose-Fenster, Base64-Overhead eingerechnet).
func (m *Message) EstimatedWireSize() int64 {
	size := int64(len(m.Body)) + 1024 // Header-Puffer
	for _, att := range m.Attachments {
		n := int64(len(att.Data))
		size += (n/57 + 1) * 78 // 57 Rohbytes → 76 Zeichen + CRLF
	}
	return size
}

func randBoundary() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// SpoolData stellt die Envelope-Zeile `From <identity> <date>` voran –
// das Server-rmail liest daraus den Envelope-Sender (Pflicht, kein
// From:-Header-Fallback).
func SpoolData(raw []byte, identity string, date time.Time) []byte {
	envelope := fmt.Sprintf("From %s %s\n", identity, date.UTC().Format(time.ANSIC))
	return append([]byte(envelope), raw...)
}

// StripEnvelope entfernt eine führende `From `-Zeile (eingehende Mails)
// und liefert den Envelope-Sender separat.
func StripEnvelope(raw []byte) (body []byte, envelopeFrom string) {
	if !strings.HasPrefix(string(raw), "From ") {
		return raw, ""
	}
	idx := strings.IndexByte(string(raw), '\n')
	if idx < 0 {
		return raw, ""
	}
	fields := strings.Fields(string(raw[:idx]))
	if len(fields) >= 2 {
		envelopeFrom = fields[1]
	}
	return raw[idx+1:], envelopeFrom
}

func encodeSubject(s string) string {
	for _, r := range s {
		if r > 126 || r < 32 {
			return mime.QEncoding.Encode("utf-8", s)
		}
	}
	return s
}

func randTag() string {
	return fmt.Sprintf("%04x", time.Now().UnixNano()&0xffff)
}
