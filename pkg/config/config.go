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

	"sync"
	"time"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
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
	Language        string         `json:"language"`                // "de", "en" oder "" (= OS-Locale)
	Theme           string         `json:"theme"`                   // "", "light", "dark" ("" = System)
	UIScale         float64        `json:"ui_scale"`                // Schriftgrößen-Faktor, 0/1 = 100 %
	DraftAutosave   int            `json:"draft_autosave_min"`      // Minuten, 0 = Default (10)
	PGPCacheMin     int            `json:"pgp_cache_min"`           // Minuten, 0 = immer fragen
	PGPKeyserver    string         `json:"pgp_keyserver,omitempty"` // hkps://… für den Schlüsselabruf, leer = pgp.DefaultKeyserver
	TrashDays       int            `json:"trash_retention_days"`    // Papierkorb-Aufbewahrung in Tagen, 0 = nie leeren
	ScrollLines     int            `json:"scroll_lines"`            // Shift+Cursor-Sprungweite im Mailtext (Zeilen)
	SignatureOn     bool           `json:"signature_enabled"`       // Standard-Signatur in neue Mails einfügen
	SignatureReply  bool           `json:"signature_on_reply"`      // … auch bei Antworten/Weiterleitungen
	SignatureText   string         `json:"signature_text"`
	SSHKeyMode      string         `json:"ssh_key_mode,omitempty"` // "", "file" = Datei-Schlüssel; "fido2" = Security-Key
	PGPCard         bool           `json:"pgp_card,omitempty"`     // aktiver PGP-Schlüssel liegt auf OpenPGP-Smartcard (Stub: pgpcard_key.json)
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
		TrashDays:     30,
		ScrollLines:   10,
		ServiceProfile: ServiceProfile{
			MaxMessageBytes: 5 * 1024 * 1024,
			WarnMsgsPerHour: 100,
		},
	}
}

// PortableDirName: liegt ein Ordner dieses Namens neben dem Binary, läuft
// der Client portabel – alle Daten bleiben dort (USB-Stick-Szenario).
const PortableDirName = "uucpmail-data"

// executableDir ist als Variable testbar (os.Executable im Test unbrauchbar).
var executableDir = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe), nil
}

var (
	dirOnce     sync.Once
	resolvedDir string
	portable    bool
)

// resolveDir ermittelt das Datenverzeichnis einmalig: portabler Ordner
// neben dem Binary hat Vorrang, sonst os.UserConfigDir()/uucpmail.
func resolveDir() (string, error) {
	if exeDir, err := executableDir(); err == nil {
		p := filepath.Join(exeDir, PortableDirName)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			portable = true
			return p, nil
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "uucpmail"), nil
}

// Dir liefert das Konfigurations-/Datenverzeichnis und legt es bei Bedarf an.
func Dir() (string, error) {
	var onceErr error
	dirOnce.Do(func() {
		resolvedDir, onceErr = resolveDir()
	})
	if onceErr != nil {
		return "", onceErr
	}
	if resolvedDir == "" {
		return "", i18n.Errorf("err.config.nodir")
	}
	// MkdirAll-Modi sind auf FAT-Medien wirkungslos – unkritisch.
	if err := os.MkdirAll(resolvedDir, 0o700); err != nil {
		return "", err
	}
	return resolvedDir, nil
}

// IsPortable meldet, ob der portable Datenordner neben dem Binary aktiv ist.
func IsPortable() bool {
	_, _ = Dir()
	return portable
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

// FIDO2StubPath: Stub des FIDO2-SSH-Schlüssels (Public Key + Credential-ID).
func FIDO2StubPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "fido2_key.json"), nil
}

// PGPCardStubPath: Stub des kartengestützten OpenPGP-Schlüssels
// (Serial + Subkey-Fingerprints, pkg/pgpcard).
func PGPCardStubPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pgpcard_key.json"), nil
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

// PGPVerifiedPath: lokale Web-of-Trust-Prüfvermerke (pgp_verified.json).
func PGPVerifiedPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pgp_verified.json"), nil
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
		return nil, i18n.Wrap(err, "err.config.corrupt")
	}
	// Rand-Whitespace (z.B. aus der Registrierungsmail kopiert) macht den
	// Node-Namen im UUCP-S-Command wire-ungültig – still normalisieren.
	cfg.NodeName = strings.TrimSpace(cfg.NodeName)
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
	return "", i18n.Errorf("err.config.noidentity")
}

// CheckIdentity liefert eine Warnung (kein Fehler), wenn die Adresse nicht zum
// Dienst passt – autoritativ prüft der Server.
func CheckIdentity(addr string) string {
	if !strings.HasSuffix(strings.ToLower(addr), "@uucpmail.de") {
		return fmt.Sprintf(i18n.T("log.identity.warn"), addr)
	}
	return ""
}
