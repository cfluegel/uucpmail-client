// Package pgpmime baut die PGP/MIME-Container (RFC 3156). Die Multiparts
// werden bewusst manuell zusammengesetzt (kein multipart.Writer): der
// signierte Teil muss BYTE-EXAKT zwischen den Boundaries stehen, sonst ist
// jede Signatur ungültig.
package pgpmime

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// WrapSigned erzeugt das multipart/signed-Entity (Header + Body) um ein
// inneres MIME-Entity und seine abgetrennte Armor-Signatur.
func WrapSigned(inner, armoredSig []byte) []byte {
	b := "pgpsig-" + randBoundary()
	var out []byte
	out = appendStr(out, fmt.Sprintf(
		"Content-Type: multipart/signed; protocol=\"application/pgp-signature\"; micalg=\"pgp-sha256\"; boundary=%q\r\n\r\n", b))
	out = appendStr(out, "--"+b+"\r\n")
	out = append(out, inner...) // byte-exakt; endet laut compose immer mit \r\n
	out = appendStr(out, "\r\n--"+b+"\r\n")
	out = appendStr(out, "Content-Type: application/pgp-signature; name=\"signature.asc\"\r\n")
	out = appendStr(out, "Content-Description: OpenPGP digital signature\r\n\r\n")
	out = append(out, armoredSig...)
	out = appendStr(out, "\r\n--"+b+"--\r\n")
	return out
}

// SignedPayload liefert exakt die Bytes, über die die Signatur gebildet wird
// (= das innere Entity, wie es WrapSigned zwischen die Boundaries setzt).
func SignedPayload(inner []byte) []byte { return inner }

// WrapEncrypted erzeugt das multipart/encrypted-Entity um eine Armor-Message.
func WrapEncrypted(armoredMsg []byte) []byte {
	b := "pgpenc-" + randBoundary()
	var out []byte
	out = appendStr(out, fmt.Sprintf(
		"Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"; boundary=%q\r\n\r\n", b))
	out = appendStr(out, "--"+b+"\r\n")
	out = appendStr(out, "Content-Type: application/pgp-encrypted\r\n\r\n")
	out = appendStr(out, "Version: 1\r\n")
	out = appendStr(out, "\r\n--"+b+"\r\n")
	out = appendStr(out, "Content-Type: application/octet-stream; name=\"encrypted.asc\"\r\n")
	out = appendStr(out, "Content-Description: OpenPGP encrypted message\r\n\r\n")
	out = append(out, armoredMsg...)
	out = appendStr(out, "\r\n--"+b+"--\r\n")
	return out
}

func appendStr(b []byte, s string) []byte { return append(b, s...) }

func randBoundary() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
