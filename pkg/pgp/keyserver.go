package pgp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
)

// DefaultKeyserver liefert Schlüssel vollständig (Identitäten und
// Fremd-Signaturen) – nötig für die Rücksendung an die UID-Adressen.
const DefaultKeyserver = "hkps://keyserver.ubuntu.com"

// maxKeyserverResponse begrenzt die Antwort (Schutz vor Riesen-Keys).
const maxKeyserverResponse = 5 << 20

// NormalizeFingerprint bereinigt eine Eingabe (Leerzeichen, „0x“, klein)
// und akzeptiert nur volle Fingerprints (v4: 40, v6: 64 Hex-Zeichen).
// Kurze Key-IDs sind fälschbar und werden abgelehnt.
func NormalizeFingerprint(s string) (string, error) {
	s = strings.ToUpper(strings.Join(strings.Fields(s), ""))
	s = strings.TrimPrefix(s, "0X")
	if len(s) != 40 && len(s) != 64 {
		return "", i18n.Errorf("err.pgp.badfpr")
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'A' || c > 'F') {
			return "", i18n.Errorf("err.pgp.badfpr")
		}
	}
	return s, nil
}

// NormalizeKeyRef akzeptiert einen vollen Fingerprint (40/64 Hex) oder eine
// lange Key-ID (16 Hex). Kurze Key-IDs (8 Hex) lehnt sie mit eigener
// Meldung ab: sie lassen sich mit wenig Aufwand fälschen.
func NormalizeKeyRef(s string) (string, error) {
	c := strings.ToUpper(strings.Join(strings.Fields(s), ""))
	c = strings.TrimPrefix(c, "0X")
	for _, r := range c {
		if (r < '0' || r > '9') && (r < 'A' || r > 'F') {
			return "", i18n.Errorf("err.pgp.badkeyref")
		}
	}
	switch len(c) {
	case 16, 40, 64:
		return c, nil
	case 8:
		return "", i18n.Errorf("err.pgp.shortkeyid")
	}
	return "", i18n.Errorf("err.pgp.badkeyref")
}

// keyIDOf liefert die lange Key-ID des Primärschlüssels (16 Hex, groß).
func keyIDOf(e *openpgp.Entity) string {
	return fmt.Sprintf("%016X", e.PrimaryKey.KeyId)
}

// KeyID liefert die lange Key-ID eines Schlüssels im Ring ("" = unbekannt).
func (k *Keyring) KeyID(fingerprint string) string {
	if e := k.findByFingerprint(k.public, fingerprint); e != nil {
		return keyIDOf(e)
	}
	return ""
}

// IsKeyNotOnServer meldet, ob FetchKey den Schlüssel auf dem Keyserver
// nicht gefunden hat (zur Unterscheidung von Netz- und anderen Fehlern).
func IsKeyNotOnServer(err error) bool {
	var e *i18n.Error
	return errors.As(err, &e) && e.Key == "err.keyserver.notfound"
}

// KeyserverURL prüft eine Keyserver-Angabe und liefert die https-Basis-URL.
// Nur verschlüsselte Verbindungen (hkps:// oder https://); leer = Standard.
func KeyserverURL(server string) (string, error) {
	server = strings.TrimSpace(server)
	if server == "" {
		server = DefaultKeyserver
	}
	u, err := url.Parse(server)
	if err != nil || u.Host == "" || (u.Scheme != "hkps" && u.Scheme != "https") ||
		u.RawQuery != "" || u.User != nil {
		return "", i18n.Errorf("err.keyserver.url", server)
	}
	u.Scheme = "https"
	return strings.TrimRight(u.String(), "/"), nil
}

// FetchKey holt den Schlüssel per HKP vom Keyserver; ref ist ein voller
// Fingerprint oder eine lange Key-ID (NormalizeKeyRef). Zurück kommt nur die
// Entity, deren Primärschlüssel exakt passt (armored) – liefert der Server
// etwas anderes, wird nichts übernommen; passen mehrere (Key-ID-Kollision),
// ebenfalls nicht.
// client nil = Standard-Client (Proxy aus der Umgebung, 20 s Zeitlimit).
func FetchKey(ctx context.Context, client *http.Client, server, ref string) ([]byte, error) {
	want, err := NormalizeKeyRef(ref)
	if err != nil {
		return nil, err
	}
	base, err := KeyserverURL(server)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	q := url.Values{"op": {"get"}, "options": {"mr"}, "search": {"0x" + want}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/pks/lookup?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, i18n.Wrap(err, "err.keyserver.net", base)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, i18n.Errorf("err.keyserver.notfound", want)
	case resp.StatusCode != http.StatusOK:
		return nil, i18n.Errorf("err.keyserver.status", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxKeyserverResponse+1))
	if err != nil {
		return nil, i18n.Wrap(err, "err.keyserver.net", base)
	}
	if len(body) > maxKeyserverResponse {
		return nil, i18n.Errorf("err.keyserver.toolarge")
	}
	el, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(body))
	if err != nil {
		return nil, i18n.Wrap(err, "err.pgp.badkeyfile")
	}
	var hits []*openpgp.Entity
	for _, e := range el {
		if fpr(e) == want || (len(want) == 16 && keyIDOf(e) == want) {
			hits = append(hits, e)
		}
	}
	switch len(hits) {
	case 0:
		return nil, i18n.Errorf("err.keyserver.mismatch")
	case 1:
		// Serialize schreibt nur öffentliche Pakete.
		return armorPublic(hits[0])
	}
	return nil, i18n.Errorf("err.keyserver.ambiguous", want)
}
