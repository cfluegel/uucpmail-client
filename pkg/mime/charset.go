package mailmime

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
)

// decodeCharset wandelt Text-Bytes nach UTF-8. Unbekannte Charsets fallen auf
// Latin-1 zurück (verlustfrei byteweise abbildbar) – es schlägt nie fehl.
func decodeCharset(data []byte, charset string) string {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		if utf8.Valid(data) {
			return string(data)
		}
		return strings.ToValidUTF8(string(data), "�")
	case "iso-8859-1", "latin1", "latin-1":
		return decodeWith(charmap.ISO8859_1, data)
	case "iso-8859-15", "latin9":
		return decodeWith(charmap.ISO8859_15, data)
	case "windows-1252", "cp1252":
		return decodeWith(charmap.Windows1252, data)
	default:
		if utf8.Valid(data) {
			return string(data)
		}
		return decodeWith(charmap.ISO8859_1, data)
	}
}

func decodeWith(enc encoding.Encoding, data []byte) string {
	out, err := enc.NewDecoder().Bytes(data)
	if err != nil {
		return strings.ToValidUTF8(string(data), "�")
	}
	return string(out)
}
