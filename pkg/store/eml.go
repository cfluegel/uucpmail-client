package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"mime"
	"net/mail"

	mailmime "github.com/cfluegel/uucpmail-client/pkg/mime"
	"time"
)

// metaFromEML extrahiert die Index-Felder aus den RFC-5322-Rohdaten.
// Parse-Fehler führen nie zum Verlust der Nachricht – dann bleiben die
// Felder leer und das Datum ist die Importzeit.
func metaFromEML(id, folder string, raw []byte) *Meta {
	meta := &Meta{ID: id, Folder: folder, Size: int64(len(raw)), Date: time.Now()}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		meta.Subject = "(nicht parsbare Nachricht)"
		return meta
	}
	meta.From = decodeHeader(msg.Header.Get("From"))
	meta.To = decodeHeader(msg.Header.Get("To"))
	meta.Cc = decodeHeader(msg.Header.Get("Cc"))
	meta.HasAttach = mailmime.HasAttachments(raw)
	meta.Subject = decodeHeader(msg.Header.Get("Subject"))
	if d, err := msg.Header.Date(); err == nil {
		meta.Date = d
	}
	return meta
}

func decodeHeader(s string) string {
	wd := new(mime.WordDecoder)
	if out, err := wd.DecodeHeader(s); err == nil {
		return out
	}
	return s
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "00000000"[:2*n]
	}
	return hex.EncodeToString(b)
}
