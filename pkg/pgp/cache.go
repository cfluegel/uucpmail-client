package pgp

import (
	"sync"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
)

// Cache hält entsperrte private Schlüssel für eine feste Dauer ab dem
// Entsperren (keine gleitende Verlängerung – vorhersagbares Ablaufverhalten).
// TTL 0 = nie cachen (immer nach der Passphrase fragen).
type Cache struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time // injizierbar für Tests
	entries map[string]cacheEntry
}

type cacheEntry struct {
	entity    *openpgp.Entity
	expiresAt time.Time
}

func NewCache(ttl time.Duration) *Cache {
	return &Cache{ttl: ttl, now: time.Now, entries: map[string]cacheEntry{}}
}

// SetTTL ändert die Dauer (wirkt auf künftige Put-Aufrufe).
func (c *Cache) SetTTL(ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ttl = ttl
}

// Get liefert den entsperrten Schlüssel oder nil (abgelaufen/unbekannt).
func (c *Cache) Get(fingerprint string) *openpgp.Entity {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[fingerprint]
	if !ok {
		return nil
	}
	if c.now().After(e.expiresAt) {
		delete(c.entries, fingerprint)
		return nil
	}
	return e.entity
}

// Put legt einen entsperrten Schlüssel ab (no-op bei TTL 0).
func (c *Cache) Put(fingerprint string, e *openpgp.Entity) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ttl <= 0 {
		return
	}
	c.entries[fingerprint] = cacheEntry{entity: e, expiresAt: c.now().Add(c.ttl)}
}

// Remove verwirft den entsperrten Schlüssel zu einem Fingerprint.
func (c *Cache) Remove(fingerprint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, fingerprint)
}

// Clear verwirft alle entsperrten Schlüssel (z.B. bei TTL-Änderung).
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]cacheEntry{}
}
