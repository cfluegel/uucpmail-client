package sync

import (
	"bytes"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cfluegel/uucpmail-client/pkg/config"
)

// uuctl-Antworten kommen als Mail von MAILER-DAEMON@… mit
// Subject "uucpmail: quota" bzw. "uucpmail: usage" (Format verifiziert am
// Server: mail-router handleUUCTL). Die Parser sind bewusst tolerant:
// unbekannte Zeilen werden ignoriert, fehlende Werte bleiben unverändert.

var (
	reQuota     = regexp.MustCompile(`(?m)^Postfach-Quota:\s*(\d+)\s*Bytes`)
	reMaxMsgs   = regexp.MustCompile(`(?m)^Max\. Nachrichten in Queue:\s*(\d+)`)
	reMaxSize   = regexp.MustCompile(`(?m)^Max\. Nachrichtengröße:\s*(\d+)\s*Bytes`)
	reRetention = regexp.MustCompile(`(?m)^Aufbewahrung[^:]*:\s*(\d+)\s*Tage`)
	reMaxAddr   = regexp.MustCompile(`(?m)^Identitäten \(max\.\):\s*(\d+)`)
	reTier      = regexp.MustCompile(`(?m)^Tarif:\s*(\S+)`)
	reUsed      = regexp.MustCompile(`(?m)^Belegt:\s*(\d+)\s*von\s*(\d+)\s*Bytes`)
	reQueued    = regexp.MustCompile(`(?m)^Nachrichten in Queue:\s*(\d+)\s*von\s*(\d+)`)
	reUsedAddr  = regexp.MustCompile(`(?m)^Identitäten:\s*(\d+)\s*von\s*(\d+)`)
)

// parseUUCTLReply erkennt und parst eine uuctl-Antwortmail in den Status.
// Rückgabe: Art ("quota"/"usage") oder "" wenn es keine uuctl-Antwort ist.
func parseUUCTLReply(raw []byte, st *config.ServiceStatus, now time.Time) string {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	from := strings.ToLower(msg.Header.Get("From"))
	subject := strings.ToLower(strings.TrimSpace(msg.Header.Get("Subject")))
	if !strings.Contains(from, "mailer-daemon@") {
		return ""
	}
	var kind string
	switch {
	case strings.HasPrefix(subject, "uucpmail: quota"):
		kind = "quota"
	case strings.HasPrefix(subject, "uucpmail: usage"):
		kind = "usage"
	default:
		return ""
	}
	var body bytes.Buffer
	if _, err := body.ReadFrom(msg.Body); err != nil && body.Len() == 0 {
		return ""
	}
	text := body.String()

	if m := reTier.FindStringSubmatch(text); m != nil {
		st.Tier = m[1]
	}
	switch kind {
	case "quota":
		setI64(&st.QuotaBytes, reQuota, text)
		setInt(&st.MaxMessages, reMaxMsgs, text)
		setI64(&st.MaxMessageBytes, reMaxSize, text)
		setInt(&st.RetentionDays, reRetention, text)
		setInt(&st.MaxAddresses, reMaxAddr, text)
		st.LastQuota = now
	case "usage":
		if m := reUsed.FindStringSubmatch(text); m != nil {
			st.UsedBytes, _ = strconv.ParseInt(m[1], 10, 64)
			st.QuotaBytes, _ = strconv.ParseInt(m[2], 10, 64)
		}
		if m := reQueued.FindStringSubmatch(text); m != nil {
			st.QueuedMessages, _ = strconv.Atoi(m[1])
			st.MaxMessages, _ = strconv.Atoi(m[2])
		}
		if m := reUsedAddr.FindStringSubmatch(text); m != nil {
			st.UsedAddresses, _ = strconv.Atoi(m[1])
			st.MaxAddresses, _ = strconv.Atoi(m[2])
		}
		st.LastUsage = now
	}
	return kind
}

func setI64(dst *int64, re *regexp.Regexp, text string) {
	if m := re.FindStringSubmatch(text); m != nil {
		if v, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			*dst = v
		}
	}
}

func setInt(dst *int, re *regexp.Regexp, text string) {
	if m := re.FindStringSubmatch(text); m != nil {
		if v, err := strconv.Atoi(m[1]); err == nil {
			*dst = v
		}
	}
}
