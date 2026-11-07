// Package pgp kapselt die OpenPGP-Funktionen des Clients (reines Go via
// ProtonMail/go-crypto, keine externe GnuPG-Installation): eigener
// Schlüsselbund als Armor-Dateien, Signieren/Verschlüsseln/Entschlüsseln/
// Prüfen sowie ein Passphrase-Cache. Web of Trust: Zertifizieren fremder
// Schlüssel + lokale Prüfvermerke (certify.go/verified.go); keine
// automatische Gültigkeitsberechnung.
package pgp

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
)

// Aliase, damit Aufrufer (UI/Sealer) nicht selbst openpgp importieren müssen.
type (
	Entity     = openpgp.Entity
	EntityList = openpgp.EntityList
	Subkey     = openpgp.Subkey
)

// KeyInfo beschreibt einen Schlüssel für die UI. Verified (nur ListPublic):
// lokal als geprüft vermerkt oder von einem eigenen Schlüssel zertifiziert.
type KeyInfo struct {
	Fingerprint string // hex, Großschreibung
	Name        string
	Email       string   // primäre UID
	Emails      []string // alle UID-Adressen (ein Schlüssel kann mehrere Identitäten tragen)
	IsPrivate   bool
	Created     time.Time
	Expiry      time.Time // Zero-Wert = läuft nicht ab
	Verified    bool      // persönlich geprüft (Web of Trust)
}

// Keyring hält die geladenen Entities und die Dateipfade.
type Keyring struct {
	pubPath, secPath string
	public           openpgp.EntityList       // nur fremde/eigene Public-Hälften
	private          openpgp.EntityList       // eigene Schlüssel (verschlüsselt gespeichert)
	verifiedPath     string                   // Prüfvermerke (verified.go)
	verified         map[string]verifiedEntry // Fingerprint → lokaler Vermerk
}

// LoadKeyring liest pubring.asc/secring.asc; fehlende Dateien = leerer Ring.
// ErrNoNewKeys: die importierte Datei enthielt keinen neuen Schlüssel.
var ErrNoNewKeys = i18n.Errorf("err.pgp.nonewkey")

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
		secs := int64(lifetime / time.Second)
		// Clamp statt uint32-Wrap (ab ~136 Jahren käme sonst ein absurd
		// kurzes/falsches Ablaufdatum heraus).
		if secs > math.MaxUint32 {
			secs = math.MaxUint32
		}
		cfg.KeyLifetimeSecs = uint32(secs)
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
		return KeyInfo{}, i18n.Errorf("err.pgp.needemail")
	}
	cfg := genConfig(lifetime)
	ent, err := openpgp.NewEntity(name, "", emails[0], cfg)
	if err != nil {
		return KeyInfo{}, err
	}
	for _, extra := range emails[1:] {
		if err := ent.AddUserId(name, "", extra, cfg); err != nil {
			return KeyInfo{}, i18n.Wrap(err, "err.pgp.identity", extra)
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

// CardSubkeyLifetime: feste Gültigkeit der Karten-Subkeys (1 Jahr,
// Nutzervorgabe). Der Master bleibt erreichbar, Subkeys werden über neue
// Binding-Signaturen verlängert (das Kartenmaterial bleibt unverändert).
const CardSubkeyLifetime = 365 * 24 * time.Hour

// GenerateCardEntity erzeugt den kartengestützten Schlüssel: RSA-Master
// (Gültigkeit masterLifetime, 0 = unbegrenzt) mit drei RSA-Subkeys Sign,
// Encrypt und Authenticate (je CardSubkeyLifetime) – die Struktur für
// „Master lokal, Subkeys auf die Karte" (Slots SIG/DEC/AUT). Die Entity
// wird UNverschlüsselt zurückgegeben und noch nicht in den Ring übernommen:
// der Wizard braucht die rohen Subkey-Schlüssel für den Karten-Import und
// übernimmt sie danach über AdoptEntity.
func GenerateCardEntity(name string, emails []string, bits int, masterLifetime time.Duration) (*Entity, error) {
	if len(emails) == 0 {
		return nil, i18n.Errorf("err.pgp.needemail")
	}
	if bits != 2048 && bits != 3072 && bits != 4096 {
		return nil, i18n.Errorf("err.pgp.rsabits", bits)
	}
	masterCfg := rsaConfig(bits, masterLifetime)
	ent, err := openpgp.NewEntity(name, "", emails[0], masterCfg)
	if err != nil {
		return nil, err
	}
	for _, extra := range emails[1:] {
		if err := ent.AddUserId(name, "", extra, masterCfg); err != nil {
			return nil, i18n.Wrap(err, "err.pgp.identity", extra)
		}
	}
	// Den von NewEntity automatisch angelegten Encrypt-Subkey (ohne Ablauf)
	// verwerfen – alle drei Subkeys entstehen einheitlich mit 1 Jahr.
	ent.Subkeys = nil
	subCfg := rsaConfig(bits, CardSubkeyLifetime)
	if err := ent.AddEncryptionSubkey(subCfg); err != nil {
		return nil, err
	}
	if err := ent.AddSigningSubkey(subCfg); err != nil {
		return nil, err
	}
	// Auth-Subkey: go-crypto hat keinen eigenen Helfer – ein Sign-Subkey
	// wird auf FlagAuthenticate umgewidmet und neu gebunden (die Embedded-
	// Backsig ist nur für Sign-Subkeys nötig und entfällt).
	if err := ent.AddSigningSubkey(subCfg); err != nil {
		return nil, err
	}
	auth := &ent.Subkeys[len(ent.Subkeys)-1]
	auth.Sig.FlagSign = false
	auth.Sig.FlagAuthenticate = true
	auth.Sig.EmbeddedSignature = nil
	if err := auth.Sig.SignKey(auth.PublicKey, ent.PrivateKey, subCfg); err != nil {
		return nil, err
	}
	return ent, nil
}

// rsaConfig: Erzeugungs-Config für RSA-Schlüssel (v4, SHA-256), Pendant zu
// genConfig für die Kartenwelt.
func rsaConfig(bits int, lifetime time.Duration) *packet.Config {
	cfg := &packet.Config{
		Algorithm:   packet.PubKeyAlgoRSA,
		RSABits:     bits,
		DefaultHash: crypto.SHA256,
	}
	if lifetime > 0 {
		secs := int64(lifetime / time.Second)
		if secs > math.MaxUint32 {
			secs = math.MaxUint32
		}
		cfg.KeyLifetimeSecs = uint32(secs)
	}
	return cfg
}

// AdoptEntity verschlüsselt die (mit GenerateCardEntity erzeugte) Entity mit
// der Passphrase (sofern nicht leer) und übernimmt sie in beide Ringe + Save –
// das Gegenstück zum Schlussteil von Generate.
func (k *Keyring) AdoptEntity(ent *Entity, passphrase string) (KeyInfo, error) {
	if passphrase != "" {
		if err := encryptEntity(ent, passphrase); err != nil {
			return KeyInfo{}, err
		}
	}
	pub, err := publicHalf(ent)
	if err != nil {
		return KeyInfo{}, err
	}
	// An den RINGANFANG: der Karten-Key wird der aktive Schlüssel – alle
	// „erster Treffer"-Lookups (Signer, Encrypt-to-self, Schlüssel anhängen)
	// bevorzugen ihn damit gegenüber einem älteren Key derselben Adresse,
	// der zum Lesen alter Mails im Ring bleibt.
	k.private = append([]*openpgp.Entity{ent}, k.private...)
	k.public = append([]*openpgp.Entity{pub}, k.public...)
	if err := k.Save(); err != nil {
		return KeyInfo{}, err
	}
	return infoOf(ent, true), nil
}

// ExportPrivateArmored exportiert den privaten Schlüssel als Armor-Text
// (Pflicht-Backup im Karten-Wizard, VOR der Übertragung auf die Karte).
// Exportiert wird eine Deep-Copy, die mit der Passphrase verschlüsselt wird –
// die übergebene Entity bleibt unverändert (ihre rohen Subkeys werden danach
// noch für den Karten-Import gebraucht; packet.Encrypt löscht das Material).
func ExportPrivateArmored(ent *Entity, passphrase string) ([]byte, error) {
	var raw bytes.Buffer
	if err := ent.SerializePrivateWithoutSigning(&raw, nil); err != nil {
		return nil, err
	}
	copies, err := openpgp.ReadKeyRing(&raw)
	if err != nil {
		return nil, err
	}
	if len(copies) == 0 {
		return nil, i18n.Errorf("err.pgp.badblock")
	}
	cp := copies[0]
	if passphrase != "" {
		if err := encryptEntity(cp, passphrase); err != nil {
			return nil, err
		}
	}
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PrivateKeyType, nil)
	if err != nil {
		return nil, err
	}
	if err := cp.SerializePrivateWithoutSigning(w, nil); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	buf.WriteString("\n")
	return buf.Bytes(), nil
}

// ExtendSubkeys verlängert die genannten Subkeys des Schlüssels um lifetime
// ab jetzt (neue Binding-Selbstsignaturen; das Schlüsselmaterial – auch auf
// einer Karte – bleibt unverändert). unlocked ist die entsperrte Entity
// (pgp.Unlock); gearbeitet wird auf dem Ring-Original, die öffentliche
// Hälfte wird neu abgeleitet, danach Save. Der neue Public Key muss an
// Kontakte neu verteilt werden.
func (k *Keyring) ExtendSubkeys(fingerprint string, subFprs []string, lifetime time.Duration, unlocked *Entity) error {
	priv := k.findByFingerprint(k.private, fingerprint)
	if priv == nil {
		return i18n.Errorf("err.pgp.keynotfound", fingerprint)
	}
	want := map[string]bool{}
	for _, f := range subFprs {
		want[strings.ToUpper(f)] = true
	}
	secs64 := int64(lifetime / time.Second)
	if secs64 > math.MaxUint32 {
		secs64 = math.MaxUint32
	}
	secs := uint32(secs64) // #nosec G115 -- oben geclampt
	now := time.Now()
	changed := 0
	for i := range priv.Subkeys {
		sk := &priv.Subkeys[i]
		if !want[fmt.Sprintf("%X", sk.PublicKey.Fingerprint)] {
			continue
		}
		sk.Sig.CreationTime = now
		sk.Sig.KeyLifetimeSecs = &secs
		// Die Embedded-Backsig (nur Sign-Subkeys) bleibt unverändert: sie neu
		// zu erzeugen bräuchte den privaten Subkey (liegt auf der Karte);
		// die alte bindet Sub→Primär ohne eigenes Ablaufdatum und bleibt gültig.
		if err := sk.Sig.SignKey(sk.PublicKey, unlocked.PrivateKey, nil); err != nil {
			return err
		}
		changed++
	}
	if changed == 0 {
		return i18n.Errorf("err.pgp.keynotfound", fingerprint)
	}
	pub, err := publicHalf(priv)
	if err != nil {
		return err
	}
	for i, e := range k.public {
		if fmt.Sprintf("%X", e.PrimaryKey.Fingerprint) == strings.ToUpper(fingerprint) {
			k.public[i] = pub
			break
		}
	}
	return k.Save()
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
		return nil, i18n.Wrap(err, "err.pgp.copyfail")
	}
	return el[0], nil
}

// ImportPrivate importiert eine Armor-Privatschlüsseldatei (Passphrase bleibt
// am Schlüssel; entsperrt wird erst bei Benutzung).
func (k *Keyring) ImportPrivate(armored []byte) (KeyInfo, error) {
	el, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(armored))
	if err != nil {
		return KeyInfo{}, i18n.Wrap(err, "err.pgp.badkeyfile")
	}
	var imported *openpgp.Entity
	for _, e := range el {
		if e.PrivateKey == nil {
			continue
		}
		if k.findByFingerprint(k.private, fpr(e)) != nil {
			return KeyInfo{}, i18n.Errorf("err.pgp.dupkey", fpr(e))
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
		return KeyInfo{}, i18n.Errorf("err.pgp.noprivindata")
	}
	if err := k.Save(); err != nil {
		return KeyInfo{}, err
	}
	return infoOf(imported, true), nil
}

// ImportPublic importiert einen oder mehrere öffentliche Schlüssel. Neue
// Zertifizierungen auf bereits bekannten Schlüsseln werden dabei übernommen
// (siehe ImportPublicReport); ErrNoNewKeys, wenn kein NEUER Schlüssel dabei war.
func (k *Keyring) ImportPublic(armored []byte) ([]KeyInfo, error) {
	rep, err := k.ImportPublicReport(armored)
	if err != nil {
		return nil, err
	}
	if len(rep.Keys) == 0 {
		return nil, ErrNoNewKeys
	}
	return rep.Keys, nil
}

// ExportPublic liefert den öffentlichen Schlüssel als Armor.
func (k *Keyring) ExportPublic(fingerprint string) ([]byte, error) {
	e := k.findByFingerprint(k.public, fingerprint)
	if e == nil {
		return nil, i18n.Errorf("err.pgp.keynotfound", fingerprint)
	}
	return armorPublic(e)
}

// armorPublic serialisiert eine öffentliche Entity als Armor-Block.
func armorPublic(e *openpgp.Entity) ([]byte, error) {
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
		return i18n.Errorf("err.pgp.ownpubdelete")
	}
	for i, e := range k.public {
		if fpr(e) == strings.ToUpper(fingerprint) {
			k.public = append(k.public[:i], k.public[i+1:]...)
			if err := k.Save(); err != nil {
				return err
			}
			// Prüfvermerk mit dem Schlüssel entfernen (sonst gälte ein später
			// erneut importierter Schlüssel ungeprüft als „geprüft").
			if _, ok := k.verified[strings.ToUpper(fingerprint)]; ok {
				return k.UnmarkVerified(fingerprint)
			}
			return nil
		}
	}
	return i18n.Errorf("err.pgp.keynotfound", fingerprint)
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
		return i18n.Errorf("err.pgp.keynotfound", fingerprint)
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
		info := infoOf(e, false)
		info.Verified = k.IsVerified(info.Fingerprint)
		out = append(out, info)
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

// FindPublicByFingerprint liefert die öffentliche Hälfte zum Fingerprint
// (Basis für das Karten-Binding, pkg/pgpcard.BindEntity).
func (k *Keyring) FindPublicByFingerprint(fingerprint string) *openpgp.Entity {
	return k.findByFingerprint(k.public, fingerprint)
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
