// Package config verwaltet die persistente Client-Konfiguration unter
// os.UserConfigDir()/uucpmail (config.json, Schlüssel, known_hosts, Mail-Store, Spool).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ServiceProfile hält weiche Limits (Vorab-Warnungen). Die Durchsetzung liegt
// beim Server; Werte sind editierbar, damit künftige Tiers ohne Client-Update
// funktionieren.
type ServiceProfile struct {
	MaxMessageBytes int64 `json:"max_message_bytes"`
	WarnMsgsPerHour int   `json:"warn_msgs_per_hour"`
}

// ServiceStatus ist der zuletzt per uuctl abgefragte Dienststatus
// (Quota/Tarif und Verbrauch); Zero-Zeitstempel = noch nie abgefragt.
type ServiceStatus struct {
	Tier            string    `json:"tier,omitempty"`
	QuotaBytes      int64     `json:"quota_bytes,omitempty"`
	MaxMessages     int       `json:"max_messages,omitempty"`
	MaxMessageBytes int64     `json:"max_message_bytes,omitempty"`
	RetentionDays   int       `json:"retention_days,omitempty"`
	MaxAddresses    int       `json:"max_addresses,omitempty"`
	UsedBytes       int64     `json:"used_bytes,omitempty"`
	QueuedMessages  int       `json:"queued_messages,omitempty"`
	UsedAddresses   int       `json:"used_addresses,omitempty"`
	LastQuota       time.Time `json:"last_quota,omitempty"`
	LastUsage       time.Time `json:"last_usage,omitempty"`
}

type Config struct {
	NodeName        string         `json:"node_name"`
	ServerHost      string         `json:"server_host"`
	ServerPort      int            `json:"server_port"`
	Identities      []string       `json:"identities"`
	DefaultIdentity string         `json:"default_identity"`
	Language        string         `json:"language"`           // "de", "en" oder "" (= OS-Locale)
	Theme           string         `json:"theme"`              // "", "light", "dark" ("" = System)
	UIScale         float64        `json:"ui_scale"`           // Schriftgrößen-Faktor, 0/1 = 100 %
	DraftAutosave   int            `json:"draft_autosave_min"` // Minuten, 0 = Default (10)
	PGPCacheMin     int            `json:"pgp_cache_min"`      // Minuten, 0 = immer fragen
	ServiceProfile  ServiceProfile `json:"service_profile"`
	ServiceStatus   ServiceStatus  `json:"service_status,omitempty"`
}

// Defaults für den Free Tier von uucpmail.de.
func Default() *Config {
	return &Config{
		ServerHost:    "uucp.uucpmail.de",
		ServerPort:    22,
		DraftAutosave: 10,
		PGPCacheMin:   15,
		ServiceProfile: ServiceProfile{
			MaxMessageBytes: 10 * 1024 * 1024,
			WarnMsgsPerHour: 100,
		},
	}
}

// Dir liefert das Konfigurations-/Datenverzeichnis und legt es bei Bedarf an.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "uucpmail")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func KeyPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "id_ed25519"), nil
}

func KnownHostsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "known_hosts"), nil
}

// PubringPath/SecringPath: der eigene OpenPGP-Schlüsselbund (Armor-Dateien).
func PubringPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pubring.asc"), nil
}

func SecringPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "secring.asc"), nil
}

// ContactsPath: das minimale Adressbuch (contacts.json).
func ContactsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "contacts.json"), nil
}

func MailDir() (string, error)  { return subdir("mail") }
func SpoolDir() (string, error) { return subdir("spool") }

func subdir(name string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

// Load liest die Konfiguration; existiert keine, werden Defaults geliefert
// (ohne NodeName/Identitäten – der Erststart-Wizard bzw. die Settings füllen sie).
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, err
	}
	cfg := Default()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config.json beschädigt: %w", err)
	}
	return cfg, nil
}

func (c *Config) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Complete meldet, ob die Pflichtfelder für eine UUCP-Session gesetzt sind.
func (c *Config) Complete() bool {
	return c.NodeName != "" && c.ServerHost != "" && c.ServerPort > 0
}

// Identity liefert die zu verwendende Absenderadresse (explizit > Default > erste).
func (c *Config) Identity(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if c.DefaultIdentity != "" {
		return c.DefaultIdentity, nil
	}
	if len(c.Identities) > 0 {
		return c.Identities[0], nil
	}
	return "", errors.New("keine Absender-Identität konfiguriert")
}

// CheckIdentity liefert eine Warnung (kein Fehler), wenn die Adresse nicht zum
// Dienst passt – autoritativ prüft der Server.
func CheckIdentity(addr string) string {
	if !strings.HasSuffix(strings.ToLower(addr), "@uucpmail.de") {
		return fmt.Sprintf("Warnung: %q endet nicht auf @uucpmail.de – der Server wird den Absender vermutlich ablehnen (553)", addr)
	}
	return ""
}
