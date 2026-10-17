// Package pgp kapselt die OpenPGP-Funktionen des Clients (reines Go via
// ProtonMail/go-crypto, keine externe GnuPG-Installation): eigener
// Schlüsselbund als Armor-Dateien, Signieren/Verschlüsseln/Entschlüsseln/
// Prüfen sowie ein Passphrase-Cache. Kein Trust-Modell in V1.
package pgp

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// Aliase, damit Aufrufer (UI/Sealer) nicht selbst openpgp importieren müssen.
type (
	Entity     = openpgp.Entity
	EntityList = openpgp.EntityList
)

// KeyInfo beschreibt einen Schlüssel für die UI. Verified ist in V1 ungenutzt,
// hält aber den Platz für ein späteres lokales Vertrauensmodell frei.
type KeyInfo struct {
	Fingerprint string // hex, Großschreibung
	Name        string
	Email       string   // primäre UID
	Emails      []string // alle UID-Adressen (ein Schlüssel kann mehrere Identitäten tragen)
	IsPrivate   bool
	Created     time.Time
	Expiry      time.Time // Zero-Wert = läuft nicht ab
	Verified    bool      // reserviert (V1: immer false)
}

// Keyring hält die geladenen Entities und die Dateipfade.
type Keyring struct {
	pubPath, secPath string
	public           openpgp.EntityList // nur fremde/eigene Public-Hälften
	private          openpgp.EntityList // eigene Schlüssel (verschlüsselt gespeichert)
}

// LoadKeyring liest pubring.asc/secring.asc; fehlende Dateien = leerer Ring.
func LoadKeyring(pubPath, secPath string) (*Keyring, error) {
	k := &Keyring{pubPath: pubPath, secPath: secPath}
	var err error
	if k.public, err = readRing(pubPath); err != nil {
		return nil, fmt.Errorf("pubring: %w", err)
	}
	if k.private, err = readRing(secPath); err != nil {
		return nil, fmt.Errorf("secring: %w", err)
	}
	return k, nil
}

func readRing(path string) (openpgp.EntityList, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && len(bytes.TrimSpace(data)) == 0) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return openpgp.ReadArmoredKeyRing(bytes.NewReader(data))
}

// Save schreibt beide Ringe (Armor, 0600, atomar via tmp+rename).
func (k *Keyring) Save() error {
	if err := writeRing(k.pubPath, openpgp.PublicKeyType, k.public, false); err != nil {
		return err
	}
	return writeRing(k.secPath, openpgp.PrivateKeyType, k.private, true)
}

func writeRing(path, blockType string, ents openpgp.EntityList, private bool) error {
	var buf bytes.Buffer
	if len(ents) > 0 {
		w, err := armor.Encode(&buf, blockType, nil)
		if err != nil {
			return err
		}
		for _, e := range ents {
			if private {
				// WithoutSigning: erhält bestehende Selbstsignaturen und
				// funktioniert auch mit passphrase-verschlüsselten Keys.
				if err := e.SerializePrivateWithoutSigning(w, nil); err != nil {
					return err
				}
			} else if err := e.Serialize(w); err != nil {
				return err
			}
		}
		if err := w.Close(); err != nil {
			return err
		}
		buf.WriteString("\n")
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// genConfig pinnt die Schlüsselerzeugung explizit: v4, Ed25519-Primär +
// X25519-ECDH-Subkey, SHA-256 – maximale Interoperabilität (GnuPG ≥2.1).
// lifetime 0 = Schlüssel läuft nicht ab.
func genConfig(lifetime time.Duration) *packet.Config {
	cfg := &packet.Config{
		Algorithm:   packet.PubKeyAlgoEdDSA,
		Curve:       packet.Curve25519,
		DefaultHash: crypto.SHA256,
	}
	if lifetime > 0 {
		cfg.KeyLifetimeSecs = uint32(lifetime / time.Second)
	}
	return cfg
}

// DefaultKeyLifetime ist die Standard-Gültigkeit neuer Schlüssel (2 Jahre).
const DefaultKeyLifetime = 2 * 365 * 24 * time.Hour

// Generate erzeugt ein neues Schlüsselpaar mit einer oder mehreren
// Identitäten (UIDs), begrenzter Gültigkeit (lifetime 0 = unbegrenzt),
// verschlüsselt den privaten Teil mit der Passphrase (sofern nicht leer)
// und speichert beide Ringe. emails[0] wird die primäre UID.
func (k *Keyring) Generate(name string, emails []string, passphrase string, lifetime time.Duration) (KeyInfo, error) {
	if len(emails) == 0 {
		return KeyInfo{}, errors.New("mindestens eine E-Mail-Adresse wird benötigt")
	}
	cfg := genConfig(lifetime)
	ent, err := openpgp.NewEntity(name, "", emails[0], cfg)
	if err != nil {
		return KeyInfo{}, err
	}
	for _, extra := range emails[1:] {
		if err := ent.AddUserId(name, "", extra, cfg); err != nil {
			return KeyInfo{}, fmt.Errorf("identität %s: %w", extra, err)
		}
	}
	if passphrase != "" {
		if err := encryptEntity(ent, passphrase); err != nil {
			return KeyInfo{}, err
		}
	}
	pub, err := publicHalf(ent)
	if err != nil {
		return KeyInfo{}, err
	}
	k.private = append(k.private, ent)
	k.public = append(k.public, pub)
	if err := k.Save(); err != nil {
		return KeyInfo{}, err
	}
	return infoOf(ent, true), nil
}

// encryptEntity verschlüsselt Primärschlüssel UND alle Subkeys (go-crypto-
// Falle: unverschlüsselte Subkeys führen zu verwirrenden Folgefehlern).
func encryptEntity(e *openpgp.Entity, passphrase string) error {
	pw := []byte(passphrase)
	if err := e.PrivateKey.Encrypt(pw); err != nil {
		return err
	}
	for _, sk := range e.Subkeys {
		if sk.PrivateKey != nil {
			if err := sk.PrivateKey.Encrypt(pw); err != nil {
				return err
			}
		}
	}
	return nil
}

// publicHalf liefert eine reine Public-Key-Kopie eines Entity.
func publicHalf(e *openpgp.Entity) (*openpgp.Entity, error) {
	var buf bytes.Buffer
	if err := e.Serialize(&buf); err != nil {
		return nil, err
	}
	el, err := openpgp.ReadKeyRing(&buf)
	if err != nil || len(el) == 0 {
		return nil, fmt.Errorf("public-kopie fehlgeschlagen: %w", err)
	}
	return el[0], nil
}

// ImportPrivate importiert eine Armor-Privatschlüsseldatei (Passphrase bleibt
// am Schlüssel; entsperrt wird erst bei Benutzung).
func (k *Keyring) ImportPrivate(armored []byte) (KeyInfo, error) {
	el, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(armored))
	if err != nil {
		return KeyInfo{}, fmt.Errorf("keine gültige Schlüsseldatei: %w", err)
	}
	var imported *openpgp.Entity
	for _, e := range el {
		if e.PrivateKey == nil {
			continue
		}
		if k.findByFingerprint(k.private, fpr(e)) != nil {
			return KeyInfo{}, fmt.Errorf("schlüssel %s ist bereits importiert", fpr(e))
		}
		pub, err := publicHalf(e)
		if err != nil {
			return KeyInfo{}, err
		}
		k.private = append(k.private, e)
		if k.findByFingerprint(k.public, fpr(e)) == nil {
			k.public = append(k.public, pub)
		}
		imported = e
	}
	if imported == nil {
		return KeyInfo{}, errors.New("die Datei enthält keinen privaten Schlüssel")
	}
	if err := k.Save(); err != nil {
		return KeyInfo{}, err
	}
	return infoOf(imported, true), nil
}

// ImportPublic importiert einen oder mehrere öffentliche Schlüssel.
func (k *Keyring) ImportPublic(armored []byte) ([]KeyInfo, error) {
	el, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(armored))
	if err != nil {
		return nil, fmt.Errorf("keine gültige Schlüsseldatei: %w", err)
	}
	var infos []KeyInfo
	for _, e := range el {
		if k.findByFingerprint(k.public, fpr(e)) != nil {
			continue // Duplikat still überspringen
		}
		pub := e
		if e.PrivateKey != nil {
			if pub, err = publicHalf(e); err != nil {
				return nil, err
			}
		}
		k.public = append(k.public, pub)
		infos = append(infos, infoOf(pub, false))
	}
	if len(infos) == 0 {
		return nil, errors.New("kein neuer Schlüssel in der Datei")
	}
	return infos, k.Save()
}

// ExportPublic liefert den öffentlichen Schlüssel als Armor.
func (k *Keyring) ExportPublic(fingerprint string) ([]byte, error) {
	e := k.findByFingerprint(k.public, fingerprint)
	if e == nil {
		return nil, fmt.Errorf("schlüssel %s nicht gefunden", fingerprint)
	}
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		return nil, err
	}
	if err := e.Serialize(w); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	buf.WriteString("\n")
	return buf.Bytes(), nil
}

// DeletePublic entfernt einen fremden öffentlichen Schlüssel. Die Public-
// Hälfte eines eigenen privaten Schlüssels ist nicht löschbar.
func (k *Keyring) DeletePublic(fingerprint string) error {
	if k.findByFingerprint(k.private, fingerprint) != nil {
		return errors.New("die Public-Hälfte eines eigenen Schlüssels kann nicht gelöscht werden")
	}
	for i, e := range k.public {
		if fpr(e) == strings.ToUpper(fingerprint) {
			k.public = append(k.public[:i], k.public[i+1:]...)
			return k.Save()
		}
	}
	return fmt.Errorf("schlüssel %s nicht gefunden", fingerprint)
}

// DeletePrivate entfernt einen eigenen Schlüssel SAMT Public-Hälfte aus dem
// Keyring. Achtung: daran verschlüsselte Mails sind danach unlesbar – die UI
// muss das mit deutlicher Warnung bestätigen lassen.
func (k *Keyring) DeletePrivate(fingerprint string) error {
	want := strings.ToUpper(strings.TrimSpace(fingerprint))
	found := false
	for i, e := range k.private {
		if fpr(e) == want {
			k.private = append(k.private[:i], k.private[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("schlüssel %s nicht gefunden", fingerprint)
	}
	for i, e := range k.public {
		if fpr(e) == want {
			k.public = append(k.public[:i], k.public[i+1:]...)
			break
		}
	}
	return k.Save()
}

// ListPublic liefert nur fremde Schlüssel (eigene erscheinen unter ListPrivate).
func (k *Keyring) ListPublic() []KeyInfo {
	var out []KeyInfo
	for _, e := range k.public {
		if k.findByFingerprint(k.private, fpr(e)) != nil {
			continue
		}
		out = append(out, infoOf(e, false))
	}
	return out
}

func (k *Keyring) ListPrivate() []KeyInfo {
	var out []KeyInfo
	for _, e := range k.private {
		out = append(out, infoOf(e, true))
	}
	return out
}

// FindPublicByEmail sucht case-insensitive über die UID-E-Mail.
func (k *Keyring) FindPublicByEmail(email string) *openpgp.Entity {
	return findByEmail(k.public, email)
}

func (k *Keyring) FindPrivateByEmail(email string) *openpgp.Entity {
	return findByEmail(k.private, email)
}

// FindPrivateByFingerprint liefert den eigenen Schlüssel zum Fingerprint.
func (k *Keyring) FindPrivateByFingerprint(fingerprint string) *openpgp.Entity {
	return k.findByFingerprint(k.private, fingerprint)
}

// PublicEntities liefert alle Public Keys (für Signaturprüfung).
func (k *Keyring) PublicEntities() openpgp.EntityList { return k.public }

// PrivateEntities liefert alle eigenen Schlüssel (für Entschlüsselung).
func (k *Keyring) PrivateEntities() openpgp.EntityList { return k.private }

// MissingRecipients liefert alle Adressen ohne passenden Public Key.
func (k *Keyring) MissingRecipients(addrs []string) []string {
	var missing []string
	for _, a := range addrs {
		if k.FindPublicByEmail(a) == nil {
			missing = append(missing, a)
		}
	}
	return missing
}

func findByEmail(ents openpgp.EntityList, email string) *openpgp.Entity {
	want := strings.ToLower(strings.TrimSpace(addressOnly(email)))
	for _, e := range ents {
		for _, id := range e.Identities {
			if id.UserId != nil && strings.ToLower(id.UserId.Email) == want {
				return e
			}
		}
	}
	return nil
}

// addressOnly extrahiert die Adresse aus "Name <adr>"-Formen.
func addressOnly(s string) string {
	if i := strings.LastIndexByte(s, '<'); i >= 0 {
		if j := strings.IndexByte(s[i:], '>'); j > 0 {
			return s[i+1 : i+j]
		}
	}
	return s
}

func (k *Keyring) findByFingerprint(ents openpgp.EntityList, fingerprint string) *openpgp.Entity {
	want := strings.ToUpper(strings.TrimSpace(fingerprint))
	for _, e := range ents {
		if fpr(e) == want {
			return e
		}
	}
	return nil
}

// FingerprintOf liefert den Fingerprint eines Entity (für UI-Verknüpfungen).
func FingerprintOf(e *Entity) string { return fpr(e) }

func fpr(e *openpgp.Entity) string {
	return strings.ToUpper(fmt.Sprintf("%x", e.PrimaryKey.Fingerprint))
}

func infoOf(e *openpgp.Entity, private bool) KeyInfo {
	info := KeyInfo{
		Fingerprint: fpr(e),
		IsPrivate:   private,
		Created:     e.PrimaryKey.CreationTime,
	}
	if prim := e.PrimaryIdentity(); prim != nil && prim.UserId != nil {
		info.Name = prim.UserId.Name
		info.Email = prim.UserId.Email
		if sig := prim.SelfSignature; sig != nil && sig.KeyLifetimeSecs != nil && *sig.KeyLifetimeSecs > 0 {
			info.Expiry = e.PrimaryKey.CreationTime.Add(
				time.Duration(*sig.KeyLifetimeSecs) * time.Second)
		}
	}
	for _, id := range e.Identities {
		if id.UserId != nil {
			if info.Email == "" {
				info.Name, info.Email = id.UserId.Name, id.UserId.Email
			}
			info.Emails = append(info.Emails, id.UserId.Email)
		}
	}
	sort.Strings(info.Emails)
	return info
}
