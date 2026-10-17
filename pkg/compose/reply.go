package compose

import (
	"fmt"
	"strings"
)

// replyPrefixes/forwardPrefixes: bekannte Betreff-Präfixe (case-insensitive),
// die vor dem Voranstellen des kanonischen Präfixes entfernt werden.
var subjectPrefixes = []string{"re:", "aw:", "antw:", "fwd:", "fw:", "wg:"}

// stripSubjectPrefixes entfernt wiederholt alle bekannten Präfixe.
func stripSubjectPrefixes(s string) string {
	for {
		trimmed := strings.TrimSpace(s)
		lower := strings.ToLower(trimmed)
		stripped := false
		for _, p := range subjectPrefixes {
			if strings.HasPrefix(lower, p) {
				s = trimmed[len(p):]
				stripped = true
				break
			}
		}
		if !stripped {
			return trimmed
		}
	}
}

// ReplySubject liefert "Re: <Betreff>" mit dedupliziertem Präfix.
func ReplySubject(orig string) string {
	return "Re: " + stripSubjectPrefixes(orig)
}

// ForwardSubject liefert "Fwd: <Betreff>" mit dedupliziertem Präfix.
func ForwardSubject(orig string) string {
	return "Fwd: " + stripSubjectPrefixes(orig)
}

// QuoteBody baut den zitierten Antwort-Body: attribution-Zeile (von der UI
// lokalisiert, z.B. "Am 14.10.2026 09:00 schrieb a@b.de:") + Original mit
// "> "-Präfix. Bestehende Zitate werden dadurch natürlich zu ">>".
// Der Cursor gehört vor das Zitat (Leerzeilen am Anfang).
func QuoteBody(attribution, text string) string {
	var sb strings.Builder
	sb.WriteString("\n\n")
	sb.WriteString(attribution)
	sb.WriteString("\n")
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		sb.WriteString("> ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

// ForwardBlock baut den Weiterleitungs-Body: Trennzeile, Original-Header
// (nur nicht-leere), Leerzeile, Originaltext unverändert.
func ForwardBlock(separator string, headers [][2]string, text string) string {
	var sb strings.Builder
	sb.WriteString("\n\n")
	sb.WriteString(separator)
	sb.WriteString("\n")
	for _, kv := range headers {
		if strings.TrimSpace(kv[1]) == "" {
			continue
		}
		fmt.Fprintf(&sb, "%s: %s\n", kv[0], kv[1])
	}
	sb.WriteString("\n")
	sb.WriteString(strings.ReplaceAll(text, "\r\n", "\n"))
	if !strings.HasSuffix(sb.String(), "\n") {
		sb.WriteString("\n")
	}
	return sb.String()
}

// AppendReference hängt eine Message-ID an die References-Kette an
// (dedupliziert, auf die letzten 20 gekappt).
func AppendReference(refs []string, msgID string) []string {
	if msgID == "" {
		return refs
	}
	for _, r := range refs {
		if r == msgID {
			return refs
		}
	}
	refs = append(refs, msgID)
	if len(refs) > 20 {
		refs = refs[len(refs)-20:]
	}
	return refs
}

// FilterOwn entfernt eigene Identitäten (case-insensitive) und Duplikate aus
// einer Adressliste – für "Allen antworten".
func FilterOwn(addrs []string, own []string) []string {
	ownSet := map[string]bool{}
	for _, o := range own {
		ownSet[strings.ToLower(strings.TrimSpace(o))] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, a := range addrs {
		key := strings.ToLower(strings.TrimSpace(addressOnly(a)))
		if key == "" || ownSet[key] || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, strings.TrimSpace(a))
	}
	return out
}

// addressOnly extrahiert die reine Adresse aus "Name <adr>"-Formen.
func addressOnly(s string) string {
	if i := strings.LastIndexByte(s, '<'); i >= 0 {
		if j := strings.IndexByte(s[i:], '>'); j > 0 {
			return s[i+1 : i+j]
		}
	}
	return s
}
