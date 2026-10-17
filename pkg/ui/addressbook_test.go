package ui

import "testing"

func TestAppendRecipient(t *testing.T) {
	cases := []struct {
		current, email, want string
	}{
		{"", "a@x.de", "a@x.de"},
		{"   ", "a@x.de", "a@x.de"},
		{"a@x.de", "b@x.de", "a@x.de, b@x.de"},
		{"a@x.de, ", "b@x.de", "a@x.de, b@x.de"},       // Trailing-Separator
		{"a@x.de;", "b@x.de", "a@x.de, b@x.de"},        // Semikolon-Rest
		{"a@x.de", "a@x.de", "a@x.de"},                 // exaktes Duplikat
		{"A@X.de, b@x.de", "a@x.de", "A@X.de, b@x.de"}, // Case-Duplikat
		{"a@x.de; b@x.de", "c@x.de", "a@x.de; b@x.de, c@x.de"},
		{"a@x.de", "", "a@x.de"},
	}
	for i, c := range cases {
		if got := appendRecipient(c.current, c.email); got != c.want {
			t.Errorf("fall %d: appendRecipient(%q,%q) = %q, erwartet %q",
				i, c.current, c.email, got, c.want)
		}
	}
}
