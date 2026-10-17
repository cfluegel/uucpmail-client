package compose

import (
	"bytes"

	mailmime "github.com/cfluegel/uucpmail-client/pkg/mime"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestBuildAndParse(t *testing.T) {
	m := &Message{
		Identity:   "test@uucpmail.de",
		To:         []string{"a@example.com"},
		Cc:         []string{"b@example.com"},
		Bcc:        []string{"geheim@example.com"},
		Subject:    "Hallo",
		InReplyTo:  "<orig@uucpmail.de>",
		References: []string{"<root@uucpmail.de>", "<orig@uucpmail.de>"},
		Body:       "# Titel\nText",
		NodeName:   "u7a2f4c",
		Date:       time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	raw := m.Build("test")
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header.Get("From") != "test@uucpmail.de" ||
		parsed.Header.Get("Subject") != "Hallo" ||
		!strings.Contains(parsed.Header.Get("Message-ID"), "u7a2f4c@uucpmail.de") {
		t.Errorf("header: %+v", parsed.Header)
	}
	if !strings.HasSuffix(string(raw), "# Titel\r\nText\r\n") {
		t.Errorf("body-ende: %q", raw[len(raw)-20:])
	}
	if parsed.Header.Get("Cc") != "b@example.com" {
		t.Errorf("Cc-Header: %q", parsed.Header.Get("Cc"))
	}
	if strings.Contains(string(raw), "geheim@example.com") {
		t.Error("BCC-Adresse ist in der Nachricht sichtbar")
	}
	if parsed.Header.Get("In-Reply-To") != "<orig@uucpmail.de>" ||
		!strings.Contains(parsed.Header.Get("References"), "<root@uucpmail.de>") {
		t.Errorf("threading-header: %+v", parsed.Header)
	}
	env := m.EnvelopeRecipients()
	if len(env) != 3 || env[2] != "geheim@example.com" {
		t.Errorf("envelope: %v", env)
	}
}

func TestEnvelopeRecipientsDedupe(t *testing.T) {
	m := &Message{
		To:  []string{"A@b.de", "c@d.de"},
		Cc:  []string{"a@B.de"},
		Bcc: []string{"c@d.de", "e@f.de"},
	}
	env := m.EnvelopeRecipients()
	if len(env) != 3 {
		t.Fatalf("env: %v", env)
	}
}

func TestValidateRejectsBadInput(t *testing.T) {
	bad := []*Message{
		{Identity: "", To: []string{"a@b.de"}},
		{Identity: "kein-at", To: []string{"a@b.de"}},
		{Identity: "a@b.de"},
		{Identity: "a@b.de", To: []string{"späces in addr"}},
		{Identity: "a@b.de", Cc: []string{"kaputt adresse"}, To: []string{"a@b.de"}},
	}
	for i, m := range bad {
		if err := m.Validate(); err == nil {
			t.Errorf("fall %d nicht abgelehnt", i)
		}
	}
}

func TestSpoolDataAndStripEnvelope(t *testing.T) {
	raw := []byte("From: a@b.de\r\n\r\nHi\r\n")
	date := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	spool := SpoolData(raw, "a@b.de", date)
	first := strings.SplitN(string(spool), "\n", 2)[0]
	if first != "From a@b.de Tue Oct 13 12:00:00 2026" {
		t.Errorf("envelope: %q", first)
	}
	body, env := StripEnvelope(spool)
	if env != "a@b.de" || !bytes.Equal(body, raw) {
		t.Errorf("strip: env=%q body=%q", env, body)
	}
	// Ohne Envelope-Zeile: unverändert.
	body2, env2 := StripEnvelope(raw)
	if env2 != "" || !bytes.Equal(body2, raw) {
		t.Error("StripEnvelope veränderte Nachricht ohne Envelope")
	}
}

func TestInsertSnippet(t *testing.T) {
	bold := MarkdownSnippet{Prefix: "**", Suffix: "**"}
	text, cur := InsertSnippet("hallo", 5, bold)
	if text != "hallo****" || cur != 7 {
		t.Errorf("bold: %q %d", text, cur)
	}
	h1 := MarkdownSnippet{Prefix: "# ", Line: true}
	text, cur = InsertSnippet("zeile1\nzeile2", 10, h1)
	if text != "zeile1\n# zeile2" || cur != 12 {
		t.Errorf("h1: %q %d", text, cur)
	}
}

func TestBuildMultipartRoundtrip(t *testing.T) {
	att := Attachment{Filename: "grüße.pdf", MIMEType: "application/pdf",
		Data: bytes.Repeat([]byte{0x25, 0x50, 0x44, 0x46, 0x00, 0xFF}, 50)}
	m := &Message{
		Identity:    "test@uucpmail.de",
		To:          []string{"a@example.com"},
		Subject:     "Anhang",
		Body:        "Siehe Anhang.",
		NodeName:    "u7a2f4c",
		Date:        time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC),
		Attachments: []Attachment{att},
	}
	raw := m.Build("test")
	pm, err := mailmime.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pm.TextPlain, "Siehe Anhang.") {
		t.Errorf("textpart: %q", pm.TextPlain)
	}
	if len(pm.Attachments) != 1 {
		t.Fatalf("attachments: %d", len(pm.Attachments))
	}
	got := pm.Attachments[0]
	if got.Filename != "grüße.pdf" || got.MediaType != "application/pdf" {
		t.Errorf("meta: %+v", got)
	}
	if !bytes.Equal(got.Data, att.Data) {
		t.Error("anhang-bytes verändert")
	}
}

func TestBuildDraftBccRoundtrip(t *testing.T) {
	m := &Message{
		Identity: "test@uucpmail.de",
		To:       []string{"a@example.com"},
		Bcc:      []string{"geheim@example.com"},
		Subject:  "Entwurf",
		Body:     "Text",
		NodeName: "u7a2f4c",
	}
	draft := m.BuildDraft("test")
	if !strings.Contains(string(draft), "X-Uucpmail-Bcc: geheim@example.com") {
		t.Error("BCC nicht im Entwurf")
	}
	sent := m.Build("test")
	if strings.Contains(string(sent), "X-Uucpmail-Bcc") ||
		strings.Contains(string(sent), "geheim@example.com") {
		t.Error("privater BCC-Header im Versand-Build")
	}
}

func TestEstimatedWireSize(t *testing.T) {
	m := &Message{Body: "x", Attachments: []Attachment{{Data: make([]byte, 5700)}}}
	est := m.EstimatedWireSize()
	if est < 7000 || est > 10000 {
		t.Errorf("estimate unplausibel: %d", est)
	}
}
