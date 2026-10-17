package uucp

import (
	"fmt"
	"strconv"
	"strings"
)

// SendRequest ist das geparste bzw. zu formatierende `S`-Transfer-Command
// (Datei von Master an Slave).
//
//	S <from> <to> <user> -<options> <temp> <mode> <notify> [<size>]
type SendRequest struct {
	From    string
	To      string
	User    string
	Options string // ohne führendes '-'
	Temp    string
	Mode    string
	Notify  string
	Size    int64 // -1 = unbekannt/nicht übertragen
}

// Format erzeugt die Wire-Repräsentation. withSize steuert das Taylor-
// Size-Feld (nur nach bestätigter Size-Negotiation anhängen); v103 kodiert
// die Größe dezimal statt 0x-hex (Alt-Taylor, uucico.c FEATURE_V103).
func (s SendRequest) Format(withSize, v103 bool) string {
	notify := s.Notify
	if notify == "" {
		notify = "\"\""
	}
	cmd := fmt.Sprintf("S %s %s %s -%s %s %s %s", s.From, s.To, s.User, s.Options, s.Temp, s.Mode, notify)
	if withSize && s.Size >= 0 {
		if v103 {
			cmd += fmt.Sprintf(" %d", s.Size)
		} else {
			cmd += fmt.Sprintf(" 0x%x", s.Size)
		}
	}
	return cmd
}

// ParseSendRequest parst ein empfangenes `S`-Command (Slave-Rolle).
func ParseSendRequest(cmd string) (*SendRequest, error) {
	fields := strings.Fields(cmd)
	if len(fields) < 5 || fields[0] != "S" {
		return nil, fmt.Errorf("ungültiges S-Command: %q", cmd)
	}
	req := &SendRequest{From: fields[1], To: fields[2], User: fields[3], Size: -1}
	rest := fields[4:]
	if strings.HasPrefix(rest[0], "-") {
		req.Options = strings.TrimPrefix(rest[0], "-")
		rest = rest[1:]
	}
	if len(rest) > 0 {
		req.Temp = rest[0]
	}
	if len(rest) > 1 {
		req.Mode = rest[1]
	}
	if len(rest) > 2 {
		req.Notify = rest[2]
	}
	if len(rest) > 3 {
		if n, err := strconv.ParseInt(strings.TrimPrefix(rest[3], "0x"), 16, 64); err == nil {
			req.Size = n
		}
	}
	return req, nil
}

// Antwort-Commands.
func FormatSY(restartOffset int64) string {
	if restartOffset > 0 {
		return fmt.Sprintf("SY 0x%x", restartOffset)
	}
	return "SY"
}

// ParseReply klassifiziert SY/SN/CY/CN/HY/HN/XY/XN-Antworten.
type Reply struct {
	Kind   byte   // 'S', 'C', 'H', 'X'
	OK     bool   // Y oder N
	Detail string // Rest (z.B. SN-Grund-Code, CY-Offset)
}

func ParseReply(cmd string) (*Reply, error) {
	if len(cmd) < 2 {
		return nil, fmt.Errorf("leere/zu kurze Antwort %q", cmd)
	}
	kind := cmd[0]
	switch kind {
	case 'S', 'C', 'H', 'X':
	default:
		return nil, fmt.Errorf("unerwartete Antwort %q", cmd)
	}
	var ok bool
	switch cmd[1] {
	case 'Y':
		ok = true
	case 'N':
		ok = false
	default:
		return nil, fmt.Errorf("unerwartete Antwort %q", cmd)
	}
	return &Reply{Kind: kind, OK: ok, Detail: strings.TrimSpace(cmd[2:])}, nil
}

// SNReason übersetzt die SN-Grund-Codes in verständlichen Text.
func SNReason(detail string) string {
	switch strings.TrimSpace(detail) {
	case "2":
		return "Server hat die Übertragung abgelehnt (Berechtigung/Policy)"
	case "4":
		return "Server hat keinen Platz (Quota/Storage)"
	case "6":
		return "Datei zu groß für den Server"
	case "7":
		return "Server kann die Datei gerade nicht annehmen (temporär)"
	case "8":
		return "Datei bereits übertragen (Duplikat)"
	case "9":
		return "Zieldatei kann nicht geöffnet werden"
	default:
		if detail == "" {
			return "Server hat die Übertragung abgelehnt"
		}
		return "Server hat die Übertragung abgelehnt (Code " + detail + ")"
	}
}
