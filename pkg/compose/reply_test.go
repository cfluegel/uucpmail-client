package compose

import (
	"strings"
	"testing"
)

func TestSubjectPrefixDedupe(t *testing.T) {
	cases := []struct{ in, reply, fwd string }{
		{"Hallo", "Re: Hallo", "Fwd: Hallo"},
		{"Re: Hallo", "Re: Hallo", "Fwd: Hallo"},
		{"Re: Re: Hallo", "Re: Hallo", "Fwd: Hallo"},
		{"AW: aw: Hallo", "Re: Hallo", "Fwd: Hallo"},
		{"Fwd: WG: Hallo", "Re: Hallo", "Fwd: Hallo"},
		{"RE: FW: aw: Hallo", "Re: Hallo", "Fwd: Hallo"},
		{"  Re:   Hallo  ", "Re: Hallo", "Fwd: Hallo"},
		{"", "Re: ", "Fwd: "},
		{"Regal kaufen", "Re: Regal kaufen", "Fwd: Regal kaufen"}, // "Re" ohne ':' bleibt
	}
	for _, c := range cases {
		if got := ReplySubject(c.in); got != c.reply {
			t.Errorf("ReplySubject(%q) = %q, erwartet %q", c.in, got, c.reply)
		}
		if got := ForwardSubject(c.in); got != c.fwd {
			t.Errorf("ForwardSubject(%q) = %q, erwartet %q", c.in, got, c.fwd)
		}
	}
}

func TestQuoteBody(t *testing.T) {
	got := QuoteBody("Am 14.10.2026 schrieb a@b.de:", "Zeile1\r\nZeile2\n> altes Zitat")
	want := "\n\nAm 14.10.2026 schrieb a@b.de:\n> Zeile1\n> Zeile2\n> > altes Zitat\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestForwardBlock(t *testing.T) {
	got := ForwardBlock("---------- Weitergeleitete Nachricht ----------",
		[][2]string{{"Von", "a@b.de"}, {"CC", ""}, {"Betreff", "Test"}},
		"Original\r\nText")
	if !strings.Contains(got, "---------- Weitergeleitete Nachricht ----------") ||
		!strings.Contains(got, "Von: a@b.de") ||
		!strings.Contains(got, "Betreff: Test") ||
		!strings.Contains(got, "Original\nText") {
		t.Errorf("got: %q", got)
	}
	if strings.Contains(got, "CC:") {
		t.Error("leerer Header nicht übersprungen")
	}
}

func TestAppendReference(t *testing.T) {
	refs := AppendReference(nil, "<a@x>")
	refs = AppendReference(refs, "<b@x>")
	refs = AppendReference(refs, "<a@x>") // Duplikat
	if len(refs) != 2 || refs[1] != "<b@x>" {
		t.Errorf("refs: %v", refs)
	}
	var many []string
	for i := 0; i < 30; i++ {
		many = AppendReference(many, strings.Repeat("x", i+1))
	}
	if len(many) != 20 {
		t.Errorf("cap: %d", len(many))
	}
}

func TestFilterOwn(t *testing.T) {
	got := FilterOwn(
		[]string{"A@b.de", "Chris <christoph@uucpmail.de>", "c@d.de", "a@B.DE"},
		[]string{"christoph@uucpmail.de"})
	if len(got) != 2 || got[0] != "A@b.de" || got[1] != "c@d.de" {
		t.Errorf("got: %v", got)
	}
}
