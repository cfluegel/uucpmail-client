package compose

import (
	"bytes"

	"net/mail"
	"strings"
	"testing"
	"time"

	mailmime "github.com/cfluegel/uucpmail-client/pkg/mime"
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

func TestRFC822AttachmentUnencoded(t *testing.T) {
	inner := "From: x@example.org\nSubject: Innen\n\nHallo Wörld\n"
	m := &Message{
		Identity: "test@uucpmail.de",
		To:       []string{"a@example.com"},
		Subject:  "Fwd: 2 Nachrichten",
		Body:     "siehe Anhang",
		NodeName: "u7a2f4c",
		Date:     time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
		Attachments: []Attachment{
			{Filename: "Innen.eml", Data: []byte(inner), MIMEType: "message/rfc822"},
			{Filename: "bin.dat", Data: []byte{0, 1, 2}, MIMEType: "application/octet-stream"},
		},
	}
	raw := string(m.Build("test"))
	if !strings.Contains(raw, "Content-Type: message/rfc822") {
		t.Fatal("message/rfc822-Part fehlt")
	}
	// Die angehängte Mail muss unkodiert (8bit, CRLF) im Part stehen.
	if !strings.Contains(raw, "Subject: Innen") || !strings.Contains(raw, "Hallo Wörld") {
		t.Fatal("rfc822-Anhang nicht unkodiert enthalten")
	}
	// 8bit muss zweimal vorkommen: Textpart + rfc822-Anhang (MIMEHeader
	// sortiert Headerzeilen alphabetisch, daher nicht positionsbasiert prüfen).
	if strings.Count(raw, "Content-Transfer-Encoding: 8bit") != 2 {
		t.Fatal("rfc822-Anhang nicht als 8bit deklariert")
	}
	// Der Binär-Anhang bleibt base64.
	if !strings.Contains(raw, "Content-Transfer-Encoding: base64") {
		t.Fatal("base64-Anhang fehlt")
	}
}

func TestEnvelopeRecipientsStripDisplayNames(t *testing.T) {
	m := &Message{
		Identity: "me@uucpmail.de",
		// Exakt der Server-Fall aus EXCHANGE 2026-10-31.
		To: []string{"Andrei Rachita <andrei@rachita.net>", "andrei.rachita@uucpmail.de"},
		Cc: []string{"=?UTF-8?Q?Artur_Szyma=C5=84ski?= <arthur@skonet.pl>"},
		// Namensform und nackte Form derselben Adresse → ein Empfänger.
		Bcc: []string{"Jemand <andrei@rachita.net>"},
	}
	got := m.EnvelopeRecipients()
	want := []string{"andrei@rachita.net", "andrei.rachita@uucpmail.de", "arthur@skonet.pl"}
	if len(got) != len(want) {
		t.Fatalf("Empfänger: %v, erwartet %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Empfänger[%d] = %q, erwartet %q (alle: %v)", i, got[i], want[i], got)
		}
	}
	for _, r := range got {
		if strings.ContainsAny(r, " <>\"") {
			t.Fatalf("rmail-Argument enthält verbotene Zeichen: %q", r)
		}
	}
}

func TestBuildEncodesDisplayNameHeaders(t *testing.T) {
	m := &Message{
		Identity: "me@uucpmail.de",
		To:       []string{"Artur Szymański <arthur@skonet.pl>"},
		Subject:  "Test",
		Body:     "x",
		NodeName: "u7a2f4c",
		Date:     time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC),
	}
	raw := m.Build("test")
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	toHeader := parsed.Header.Get("To")
	for _, r := range toHeader {
		if r > 126 {
			t.Fatalf("To-Header enthält rohe Nicht-ASCII-Bytes: %q", toHeader)
		}
	}
	addrs, err := mail.ParseAddressList(toHeader)
	if err != nil || len(addrs) != 1 {
		t.Fatalf("To-Header nicht parsebar: %q (%v)", toHeader, err)
	}
	if addrs[0].Address != "arthur@skonet.pl" || addrs[0].Name != "Artur Szymański" {
		t.Fatalf("Roundtrip-Verlust: %+v", addrs[0])
	}
}
