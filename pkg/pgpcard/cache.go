package pgpcard

import (
	"sync"
	"time"
)

// PINCache hält verifizierte Nutzer-PINs pro Karten-Serial für eine feste
// Dauer ab dem Entsperren (Muster pgp.Cache; keine gleitende Verlängerung).
// Gecacht wird NUR die PIN, nie eine Session – vor jeder Operation wird die
// Karte frisch geöffnet und die PIN erneut verifiziert (Karte kann gezogen
// oder getauscht worden sein). TTL 0 = nie cachen.
type PINCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time // injizierbar für Tests
	entries map[string]pinEntry
}

type pinEntry struct {
	pin       string
	expiresAt time.Time
}

func NewPINCache(ttl time.Duration) *PINCache {
	return &PINCache{ttl: ttl, now: time.Now, entries: map[string]pinEntry{}}
}

// SetTTL ändert die Dauer (wirkt auf künftige Put-Aufrufe).
func (c *PINCache) SetTTL(ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ttl = ttl
}

// Get liefert die gecachte PIN ("" wenn abgelaufen/unbekannt).
func (c *PINCache) Get(serial string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[serial]
	if !ok {
		return "", false
	}
	if c.now().After(e.expiresAt) {
		delete(c.entries, serial)
		return "", false
	}
	return e.pin, true
}

// Put legt eine verifizierte PIN ab (no-op bei TTL 0).
func (c *PINCache) Put(serial, pin string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ttl <= 0 {
		return
	}
	c.entries[serial] = pinEntry{pin: pin, expiresAt: c.now().Add(c.ttl)}
}

// Remove verwirft die PIN einer Karte (z.B. nach fehlgeschlagenem Verify).
func (c *PINCache) Remove(serial string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, serial)
}

// Clear verwirft alle PINs.
func (c *PINCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]pinEntry{}
}
