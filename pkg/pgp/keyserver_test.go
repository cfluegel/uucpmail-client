package pgp

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

func TestNormalizeFingerprint(t *testing.T) {
	const fp = "0123456789ABCDEF0123456789ABCDEF01234567"
	for _, in := range []string{
		fp,
		strings.ToLower(fp),
		"0x" + fp,
		"0123 4567 89AB CDEF 0123  4567 89AB CDEF 0123 4567",
	} {
		got, err := NormalizeFingerprint(in)
		if err != nil || got != fp {
			t.Errorf("%q → %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "89ABCDEF01234567", "0x89ABCDEF", fp[:39] + "G", fp + "00"} {
		if _, err := NormalizeFingerprint(in); err == nil {
			t.Errorf("%q: Fehler erwartet", in)
		}
	}
}

func TestKeyserverURL(t *testing.T) {
	cases := map[string]string{
		"":                             "https://keyserver.ubuntu.com",
		"hkps://keys.openpgp.org":      "https://keys.openpgp.org",
		"https://ks.example.org:8443/": "https://ks.example.org:8443",
	}
	for in, want := range cases {
		if got, err := KeyserverURL(in); err != nil || got != want {
			t.Errorf("%q → %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"http://ks.example.org", "hkp://ks.example.org", "keyserver.ubuntu.com", "https://", "https://u:p@ks.example.org"} {
		if _, err := KeyserverURL(in); err == nil {
			t.Errorf("%q: Fehler erwartet", in)
		}
	}
}

// fakeKeyserver liefert auf /pks/lookup den Block body (bzw. status).
func fakeKeyserver(t *testing.T, status int, body []byte, gotQuery *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pks/lookup" {
			http.NotFound(w, r)
			return
		}
		if gotQuery != nil {
			*gotQuery = r.URL.RawQuery
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchKey(t *testing.T) {
	a, _, aFpr, bFpr := wotPair(t)
	// Server liefert zwei Schlüssel (A und B) – übernommen wird nur B.
	var buf bytes.Buffer
	w, _ := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	for _, fp := range []string{aFpr, bFpr} {
		if err := a.FindPublicByFingerprint(fp).Serialize(w); err != nil {
			t.Fatal(err)
		}
	}
	_ = w.Close()
	var query string
	srv := fakeKeyserver(t, http.StatusOK, buf.Bytes(), &query)

	data, err := FetchKey(context.Background(), srv.Client(), srv.URL, strings.ToLower(bFpr))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "op=get") || !strings.Contains(query, "search=0x"+bFpr) {
		t.Errorf("Anfrage: %s", query)
	}
	k := newTestKeyring(t)
	rep, err := k.ImportPublicReport(data)
	if err != nil || len(rep.Keys) != 1 || rep.Keys[0].Fingerprint != bFpr {
		t.Fatalf("Import: %+v, %v", rep, err)
	}
	if bytes.Contains(data, []byte("PRIVATE")) {
		t.Fatal("privates Material im Ergebnis")
	}
}

func TestFetchKeyErrors(t *testing.T) {
	a, _, aFpr, bFpr := wotPair(t)
	apub, _ := a.ExportPublic(aFpr)
	ctx := context.Background()

	// Falscher Schlüssel geliefert → mismatch, nichts zurück.
	srv := fakeKeyserver(t, http.StatusOK, apub, nil)
	if data, err := FetchKey(ctx, srv.Client(), srv.URL, bFpr); err == nil || data != nil ||
		!strings.Contains(err.Error(), "nicht den angefragten") && !strings.Contains(err.Error(), "did not return") {
		t.Errorf("mismatch: %v", err)
	}
	// 404 → nicht gefunden.
	srv = fakeKeyserver(t, http.StatusNotFound, nil, nil)
	if _, err := FetchKey(ctx, srv.Client(), srv.URL, bFpr); !IsKeyNotOnServer(err) {
		t.Errorf("404: IsKeyNotOnServer erwartet, %v", err)
	}
	if IsKeyNotOnServer(errors.New("x")) {
		t.Error("fremder Fehler als nicht gefunden erkannt")
	}
	// Antwort über dem Limit → abgebrochen.
	srv = fakeKeyserver(t, http.StatusOK, bytes.Repeat([]byte("A"), maxKeyserverResponse+10), nil)
	if _, err := FetchKey(ctx, srv.Client(), srv.URL, bFpr); err == nil ||
		!strings.Contains(err.Error(), "5 MiB") {
		t.Errorf("Limit: %v", err)
	}
	// Unverschlüsselt → abgelehnt, ohne Verbindung.
	if _, err := FetchKey(ctx, nil, "http://127.0.0.1:1", bFpr); err == nil {
		t.Error("http: Fehler erwartet")
	}
}

func TestNormalizeKeyRef(t *testing.T) {
	for in, want := range map[string]string{
		"1eeb928b64ff2b4e":      "1EEB928B64FF2B4E",
		"0x1EEB 928B 64FF 2B4E": "1EEB928B64FF2B4E",
	} {
		if got, err := NormalizeKeyRef(in); err != nil || got != want {
			t.Errorf("%q → %q, %v", in, got, err)
		}
	}
	if _, err := NormalizeKeyRef("64FF2B4E"); err == nil || !strings.Contains(err.Error(), "8") {
		t.Errorf("kurze Key-ID: %v", err)
	}
	for _, in := range []string{"", "1EEB928B64FF2B4", "XYZ", "1EEB928B64FF2B4E00"} {
		if _, err := NormalizeKeyRef(in); err == nil {
			t.Errorf("%q: Fehler erwartet", in)
		}
	}
}

func TestFetchKeyByKeyID(t *testing.T) {
	a, _, _, bFpr := wotPair(t)
	bpub, _ := a.ExportPublic(bFpr)
	srv := fakeKeyserver(t, http.StatusOK, bpub, nil)
	kid := a.KeyID(bFpr)
	if len(kid) != 16 {
		t.Fatalf("KeyID: %q", kid)
	}
	data, err := FetchKey(context.Background(), srv.Client(), srv.URL, "0x"+strings.ToLower(kid))
	if err != nil {
		t.Fatal(err)
	}
	if rep, err := newTestKeyring(t).ImportPublicReport(data); err != nil || rep.Keys[0].Fingerprint != bFpr {
		t.Fatalf("Import: %+v, %v", rep, err)
	}
	// Zweimal derselbe Primärschlüssel → mehrdeutig, nichts übernommen.
	var buf bytes.Buffer
	w, _ := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	for i := 0; i < 2; i++ {
		_ = a.FindPublicByFingerprint(bFpr).Serialize(w)
	}
	_ = w.Close()
	srv = fakeKeyserver(t, http.StatusOK, buf.Bytes(), nil)
	if data, err := FetchKey(context.Background(), srv.Client(), srv.URL, kid); err == nil || data != nil {
		t.Errorf("Kollision: %v", err)
	}
}
