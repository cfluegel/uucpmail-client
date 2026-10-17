package mailmime

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

func TestParsePlainSinglepart(t *testing.T) {
	raw := crlf(`From: a@b.de
To: c@d.de
Subject: Hi
Content-Type: text/plain; charset=utf-8

Hallo Wölfe
`)
	pm, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pm.TextPlain, "Hallo Wölfe") || pm.TextHTML != "" || len(pm.Attachments) != 0 {
		t.Errorf("%+v", pm)
	}
}

func TestParseMissingContentType(t *testing.T) {
	raw := crlf(`From: a@b.de

nur text
`)
	pm, err := Parse([]byte(raw))
	if err != nil || !strings.Contains(pm.TextPlain, "nur text") {
		t.Fatalf("pm=%+v err=%v", pm, err)
	}
}

func TestParseAlternativePrefersPlainAndKeepsHTML(t *testing.T) {
	raw := crlf(`From: a@b.de
Content-Type: multipart/alternative; boundary="XX"

--XX
Content-Type: text/plain; charset=utf-8

Nur Text
--XX
Content-Type: text/html; charset=utf-8

<p>HTML <b>Teil</b></p>
--XX--
`)
	pm, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pm.TextPlain, "Nur Text") {
		t.Errorf("plain fehlt: %q", pm.TextPlain)
	}
	if !strings.Contains(pm.TextHTML, "<b>Teil</b>") {
		t.Errorf("html fehlt: %q", pm.TextHTML)
	}
}

func TestParseHTMLOnly(t *testing.T) {
	raw := crlf(`From: a@b.de
Content-Type: text/html; charset=utf-8

<h1>Nur HTML</h1>
`)
	pm, err := Parse([]byte(raw))
	if err != nil || pm.TextPlain != "" || !strings.Contains(pm.TextHTML, "Nur HTML") {
		t.Fatalf("pm=%+v err=%v", pm, err)
	}
}

func TestParseMixedWithBase64AttachmentAndQPLatin1(t *testing.T) {
	pdf := []byte("%PDF-1.4 fake content \x00\x01\x02")
	b64 := base64.StdEncoding.EncodeToString(pdf)
	// Base64 mit Zeilenumbrüchen mitten drin (realistische 76-Zeichen-Wraps).
	wrapped := b64[:20] + "\r\n" + b64[20:]
	raw := crlf(`From: a@b.de
Content-Type: multipart/mixed; boundary="MM"

--MM
Content-Type: text/plain; charset=iso-8859-1
Content-Transfer-Encoding: quoted-printable

Gr=FC=DFe aus K=F6ln
--MM
Content-Type: application/pdf; name="bericht.pdf"
Content-Disposition: attachment; filename="bericht.pdf"
Content-Transfer-Encoding: base64

`) + wrapped + crlf(`
--MM--
`)
	pm, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pm.TextPlain, "Grüße aus Köln") {
		t.Errorf("latin1/qp nicht dekodiert: %q", pm.TextPlain)
	}
	if len(pm.Attachments) != 1 {
		t.Fatalf("attachments: %d", len(pm.Attachments))
	}
	att := pm.Attachments[0]
	if att.Filename != "bericht.pdf" || att.MediaType != "application/pdf" {
		t.Errorf("meta: %+v", att)
	}
	if !bytes.Equal(att.Data, pdf) {
		t.Errorf("daten verändert: %q", att.Data)
	}
}

func TestParseNestedAlternativeInsideMixed(t *testing.T) {
	raw := crlf(`From: a@b.de
Content-Type: multipart/mixed; boundary="OUT"

--OUT
Content-Type: multipart/alternative; boundary="IN"

--IN
Content-Type: text/plain

innen text
--IN
Content-Type: text/html

<p>innen html</p>
--IN--
--OUT
Content-Type: image/png; name="x.png"
Content-Transfer-Encoding: base64

aGFsbG8=
--OUT--
`)
	pm, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pm.TextPlain, "innen text") || len(pm.Attachments) != 1 {
		t.Errorf("pm=%+v", pm)
	}
	if string(pm.Attachments[0].Data) != "hallo" {
		t.Errorf("png-daten: %q", pm.Attachments[0].Data)
	}
}

func TestParseBrokenBoundaryKeepsPartial(t *testing.T) {
	raw := crlf(`From: a@b.de
Content-Type: multipart/mixed; boundary="BB"

--BB
Content-Type: text/plain

teil eins ok
--BB
Content-Type: text/plain

abgeschnitten ohne ende
`)
	pm, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pm.TextPlain, "teil eins ok") {
		t.Errorf("erster Teil verloren: %+v", pm)
	}
}

func TestParseRFC2231Filename(t *testing.T) {
	raw := crlf(`From: a@b.de
Content-Type: multipart/mixed; boundary="FF"

--FF
Content-Type: application/octet-stream
Content-Disposition: attachment; filename*=utf-8''gr%C3%BC%C3%9Fe.txt

data
--FF--
`)
	pm, err := Parse([]byte(raw))
	if err != nil || len(pm.Attachments) != 1 {
		t.Fatalf("pm=%+v err=%v", pm, err)
	}
	if pm.Attachments[0].Filename != "grüße.txt" {
		t.Errorf("filename: %q", pm.Attachments[0].Filename)
	}
}

func TestParseDepthBomb(t *testing.T) {
	// 20 verschachtelte multiparts – muss ohne Panic/Hängen durchlaufen.
	inner := "Content-Type: text/plain\r\n\r\ntief\r\n"
	for i := 0; i < 20; i++ {
		b := fmt.Sprintf("B%d", i)
		inner = fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n--%s\r\n%s--%s--\r\n", b, b, inner, b)
	}
	raw := "From: a@b.de\r\n" + inner
	if _, err := Parse([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}

func TestParseFilenameSanitized(t *testing.T) {
	raw := crlf(`From: a@b.de
Content-Type: multipart/mixed; boundary="SS"

--SS
Content-Type: application/octet-stream
Content-Disposition: attachment; filename="../../etc/passwd"

x
--SS--
`)
	pm, err := Parse([]byte(raw))
	if err != nil || len(pm.Attachments) != 1 {
		t.Fatalf("%v", err)
	}
	if pm.Attachments[0].Filename != "passwd" {
		t.Errorf("pfad nicht entschärft: %q", pm.Attachments[0].Filename)
	}
}

func TestHasAttachments(t *testing.T) {
	with := crlf(`From: a@b.de
Content-Type: multipart/mixed; boundary="AA"

--AA
Content-Type: text/plain

text
--AA
Content-Type: application/pdf; name="f.pdf"
Content-Disposition: attachment; filename="f.pdf"

x
--AA--
`)
	without := crlf(`From: a@b.de
Content-Type: text/plain

nur text
`)
	if !HasAttachments([]byte(with)) {
		t.Error("anhang nicht erkannt")
	}
	if HasAttachments([]byte(without)) {
		t.Error("falsch-positiv")
	}
}

func TestDecodeCharsetFallbacks(t *testing.T) {
	if got := decodeCharset([]byte{0x47, 0x72, 0xFC, 0xDF, 0x65}, "iso-8859-1"); got != "Grüße" {
		t.Errorf("latin1: %q", got)
	}
	// Unbekanntes Charset mit Nicht-UTF-8-Bytes: Latin-1-Fallback, gültiges UTF-8.
	if got := decodeCharset([]byte{0x47, 0xFC}, "x-unbekannt"); !utf8.ValidString(got) || got != "Gü" {
		t.Errorf("fallback: %q", got)
	}
}

func TestParseMultipartEncrypted(t *testing.T) {
	raw := crlf(`From: a@b.de
Content-Type: multipart/encrypted; protocol="application/pgp-encrypted"; boundary="EE"

--EE
Content-Type: application/pgp-encrypted

Version: 1
--EE
Content-Type: application/octet-stream

-----BEGIN PGP MESSAGE-----

hQEMAzTESTPAYLOAD==
-----END PGP MESSAGE-----
--EE--
`)
	pm, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !pm.Encrypted {
		t.Fatal("multipart/encrypted nicht erkannt")
	}
	if !bytes.HasPrefix(pm.EncryptedPayload, []byte("-----BEGIN PGP MESSAGE-----")) ||
		!bytes.HasSuffix(pm.EncryptedPayload, []byte("-----END PGP MESSAGE-----")) {
		t.Errorf("payload: %q", pm.EncryptedPayload)
	}
}

func TestParseMultipartSignedRawBytes(t *testing.T) {
	inner := "Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\nSignierter Text\r\nmit zwei Zeilen\r\n"
	raw := "From: a@b.de\r\n" +
		"Content-Type: multipart/signed; protocol=\"application/pgp-signature\"; micalg=\"pgp-sha256\"; boundary=\"SS\"\r\n\r\n" +
		"--SS\r\n" + inner +
		"\r\n--SS\r\n" +
		"Content-Type: application/pgp-signature\r\n\r\n" +
		"-----BEGIN PGP SIGNATURE-----\r\n\r\nfakesig==\r\n-----END PGP SIGNATURE-----\r\n" +
		"\r\n--SS--\r\n"
	pm, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !pm.Signed {
		t.Fatal("multipart/signed nicht erkannt")
	}
	// Das Herzstück: SignedPart muss BYTE-IDENTISCH zum inneren Entity sein.
	if string(pm.SignedPart) != inner {
		t.Errorf("SignedPart nicht byte-identisch:\ngot  %q\nwant %q", pm.SignedPart, inner)
	}
	if !bytes.Contains(pm.Signature, []byte("BEGIN PGP SIGNATURE")) {
		t.Errorf("signature: %q", pm.Signature)
	}
	// Anzeige-Parse des signierten Entity funktioniert weiter.
	if !strings.Contains(pm.TextPlain, "Signierter Text") {
		t.Errorf("textplain: %q", pm.TextPlain)
	}
}

func TestParseInlinePGPMessage(t *testing.T) {
	raw := crlf(`From: a@b.de
Content-Type: text/plain

Hier kommt was Verschlüsseltes:
-----BEGIN PGP MESSAGE-----

inlinepayload==
-----END PGP MESSAGE-----
Grüße
`)
	pm, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !pm.Encrypted || pm.TextPlain != "" {
		t.Errorf("inline-PGP nicht erkannt: enc=%v text=%q", pm.Encrypted, pm.TextPlain)
	}
	if !bytes.Contains(pm.EncryptedPayload, []byte("inlinepayload==")) {
		t.Errorf("payload: %q", pm.EncryptedPayload)
	}
}

func TestParseClearsigned(t *testing.T) {
	raw := crlf(`From: a@b.de
Content-Type: text/plain

-----BEGIN PGP SIGNED MESSAGE-----
Hash: SHA256

Klartext hier
-----BEGIN PGP SIGNATURE-----

sig==
-----END PGP SIGNATURE-----
`)
	pm, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !pm.Clearsigned || len(pm.SignedPart) == 0 {
		t.Errorf("clearsign nicht erkannt: %+v", pm)
	}
	if pm.Encrypted {
		t.Error("clearsign fälschlich als encrypted")
	}
}

func TestParseNonPGPUnaffected(t *testing.T) {
	raw := crlf(`From: a@b.de
Content-Type: text/plain

Ganz normale Mail ohne PGP.
`)
	pm, err := Parse([]byte(raw))
	if err != nil || pm.Encrypted || pm.Signed || pm.Clearsigned {
		t.Fatalf("pm=%+v err=%v", pm, err)
	}
}
