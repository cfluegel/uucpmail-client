package sync

import (
	"testing"
	"time"

	"github.com/cfluegel/uucpmail-client/pkg/config"
)

func TestParseUUCTLQuota(t *testing.T) {
	raw := []byte("From: uucpmail system <MAILER-DAEMON@uucpmail.de>\n" +
		"To: <christoph@uucpmail.de>\n" +
		"Subject: uucpmail: quota\n" +
		"Auto-Submitted: auto-replied\n\n" +
		"Tarif: free\n" +
		"Postfach-Quota: 52428800 Bytes\n" +
		"Max. Nachrichten in Queue: 250\n" +
		"Max. Nachrichtengröße: 10485760 Bytes\n" +
		"Aufbewahrung nicht abgeholter Mail: 30 Tage\n" +
		"Identitäten (max.): 5\n")
	var st config.ServiceStatus
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	if kind := parseUUCTLReply(raw, &st, now); kind != "quota" {
		t.Fatalf("kind = %q", kind)
	}
	if st.Tier != "free" || st.QuotaBytes != 52428800 || st.MaxMessages != 250 ||
		st.MaxMessageBytes != 10485760 || st.RetentionDays != 30 || st.MaxAddresses != 5 {
		t.Errorf("status: %+v", st)
	}
	if !st.LastQuota.Equal(now) || !st.LastUsage.IsZero() {
		t.Errorf("zeitstempel: %+v", st)
	}
}

func TestParseUUCTLUsage(t *testing.T) {
	raw := []byte("From: MAILER-DAEMON@uucpmail.de\n" +
		"Subject: uucpmail: usage\n\n" +
		"Tarif: free\n" +
		"Belegt: 1234 von 52428800 Bytes\n" +
		"Nachrichten in Queue: 3 von 250\n" +
		"Identitäten: 4 von 5\n")
	var st config.ServiceStatus
	if kind := parseUUCTLReply(raw, &st, time.Now()); kind != "usage" {
		t.Fatalf("kind = %q", kind)
	}
	if st.UsedBytes != 1234 || st.QuotaBytes != 52428800 || st.QueuedMessages != 3 ||
		st.UsedAddresses != 4 || st.MaxAddresses != 5 {
		t.Errorf("status: %+v", st)
	}
}

func TestParseUUCTLIgnoresNormalMail(t *testing.T) {
	cases := [][]byte{
		[]byte("From: wer@example.com\nSubject: uucpmail: quota\n\nTarif: fake\n"),  // falscher Absender
		[]byte("From: MAILER-DAEMON@uucpmail.de\nSubject: Undelivered Mail\n\nx\n"), // DSN
		[]byte("kein header"),
	}
	for i, raw := range cases {
		var st config.ServiceStatus
		if kind := parseUUCTLReply(raw, &st, time.Now()); kind != "" {
			t.Errorf("fall %d: fälschlich erkannt (%q)", i, kind)
		}
		if st.Tier != "" {
			t.Errorf("fall %d: status verändert", i)
		}
	}
}
