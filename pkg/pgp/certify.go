package pgp

import (
	"bytes"
	"crypto"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
)

// Web of Trust: Zertifizieren fremder Schlüssel (Äquivalent zu
// „gpg --sign-key") und Austausch der Zertifizierungen per Mail. Nur
// exportierbare Zertifizierungen – nicht-exportierbare (lsign) kann
// go-crypto weder lesen noch schreiben; „nur lokal geprüft" führt der
// Client deshalb als eigenen Vermerk (verified.go).

// CertStatus ist das Prüfergebnis einer Fremd-Zertifizierung.
type CertStatus int

const (
	CertValid   CertStatus = iota // kryptografisch gegen bekannten Signierer geprüft
	CertUnknown                   // Signierer-Schlüssel nicht im Schlüsselbund
	CertInvalid                   // Signierer bekannt, Signatur passt nicht
)

// Certification beschreibt eine Fremd-Zertifizierung auf einer UID.
type Certification struct {
	UID         string
	SignerFpr   string // leer, wenn der Signierer unbekannt ist
	SignerKeyID string // 16 Hex-Zeichen
	SignerRef   string // für den Keyserver: voller Issuer-Fingerprint, sonst die Key-ID
	SignerName  string // aus dem Schlüsselbund, sonst leer
	Created     time.Time
	Status      CertStatus
	Revoked     bool // vom Signierer widerrufen
	Mine        bool // von einem eigenen Schlüssel ausgestellt
}

// ImportReport fasst einen Import öffentlicher Schlüssel zusammen.
type ImportReport struct {
	Keys       []KeyInfo // neu aufgenommene Schlüssel
	Merged     int       // neu übernommene Zertifizierungen auf bekannten Schlüsseln
	MergedOwn  bool      // davon betroffen: ein eigener Schlüssel
	MergedFprs []string  // Fingerprints der Schlüssel mit neuen Zertifizierungen
	OnlyOwn    bool      // bei ErrNoNewKeys: Datei enthielt nur eigene, unveränderte Schlüssel
}

var (
	errWoTOwnKey   = i18n.Errorf("err.wot.ownkey")
	errWoTNoUID    = i18n.Errorf("err.wot.nouid")
	errWoTExpired  = i18n.Errorf("err.wot.expired")
	errWoTNoCert   = i18n.Errorf("err.wot.nocertkey")
	errWoTLocked   = i18n.Errorf("err.wot.locked")
	errWoTRevoked  = i18n.Errorf("err.wot.revoked")
	errWoTNotFound = "err.pgp.keynotfound"
)

// Certify zertifiziert die angegebenen UIDs des fremden Schlüssels targetFpr
// mit dem entsperrten eigenen Schlüssel signer und speichert den Ring.
func (k *Keyring) Certify(targetFpr string, uids []string, signer *Entity) error {
	target := k.findByFingerprint(k.public, targetFpr)
	if target == nil {
		return i18n.Errorf(errWoTNotFound, targetFpr)
	}
	if k.findByFingerprint(k.private, targetFpr) != nil {
		return errWoTOwnKey
	}
	if len(uids) == 0 {
		return errWoTNoUID
	}
	now := time.Now()
	if target.Revoked(now) {
		return errWoTRevoked
	}
	if sig, _ := target.PrimarySelfSignature(); sig != nil && target.PrimaryKey.KeyExpired(sig, now) {
		return errWoTExpired
	}
	if signer == nil || signer.PrivateKey == nil {
		return errWoTNoCert
	}
	if signer.PrivateKey.Encrypted {
		return errWoTLocked
	}
	if _, ok := signer.CertificationKey(now); !ok {
		return errWoTNoCert
	}
	cfg := &packet.Config{DefaultHash: crypto.SHA256}
	for _, uid := range uids {
		ident, ok := target.Identities[uid]
		if !ok || ident.Revoked(now) {
			return errWoTNoUID
		}
		if err := target.SignIdentity(uid, signer, cfg); err != nil {
			return err
		}
	}
	return k.Save()
}

// Certifications listet alle Fremd-Zertifizierungen eines Schlüssels (aus
// dem Pubring), je UID, geprüft gegen die Signierer im Schlüsselbund.
func (k *Keyring) Certifications(fingerprint string) []Certification {
	e := k.findByFingerprint(k.public, fingerprint)
	if e == nil {
		return nil
	}
	var out []Certification
	for _, uid := range sortedUIDs(e) {
		ident := e.Identities[uid]
		for _, sig := range ident.Signatures {
			if !isCertType(sig.SigType) || sig.CheckKeyIdOrFingerprint(e.PrimaryKey) {
				continue
			}
			c := Certification{UID: uid, Created: sig.CreationTime, SignerKeyID: issuerKeyID(sig), Status: CertUnknown}
			c.SignerRef = c.SignerKeyID
			if n := len(sig.IssuerFingerprint); n == 20 || n == 32 {
				c.SignerRef = strings.ToUpper(fmt.Sprintf("%x", sig.IssuerFingerprint))
			}
			signer := k.signerOf(sig)
			if signer != nil {
				c.SignerFpr = fpr(signer)
				if prim := signer.PrimaryIdentity(); prim != nil && prim.UserId != nil {
					c.SignerName = prim.Name
				}
				c.Mine = k.findByFingerprint(k.private, c.SignerFpr) != nil
				if signer.PrimaryKey.VerifyUserIdSignature(uid, e.PrimaryKey, sig) == nil {
					c.Status = CertValid
				} else {
					c.Status = CertInvalid
				}
				c.Revoked = revokedBy(ident, signer, e, uid, sig.CreationTime)
			}
			out = append(out, c)
		}
	}
	return out
}

// ExportForUID liefert den Schlüssel targetFpr als Armor mit NUR der UID uid,
// deren Selbstsignaturen und den Zertifizierungen von signerFpr (caff-Prinzip:
// jede Adresse bekommt nur „ihre" Zertifizierung).
func (k *Keyring) ExportForUID(targetFpr, uid, signerFpr string) ([]byte, error) {
	e := k.findByFingerprint(k.public, targetFpr)
	if e == nil {
		return nil, i18n.Errorf(errWoTNotFound, targetFpr)
	}
	ident, ok := e.Identities[uid]
	if !ok {
		return nil, errWoTNoUID
	}
	signer := k.findByFingerprint(k.public, signerFpr)
	ic := *ident
	ic.Signatures = nil
	for _, sig := range ident.Signatures {
		if sig.CheckKeyIdOrFingerprint(e.PrimaryKey) || (signer != nil && sig.CheckKeyIdOrFingerprint(signer.PrimaryKey)) {
			ic.Signatures = append(ic.Signatures, sig)
		}
	}
	cp := *e
	cp.Identities = map[string]*openpgp.Identity{uid: &ic}
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		return nil, err
	}
	if err := cp.Serialize(w); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	buf.WriteString("\n")
	return buf.Bytes(), nil
}

// ImportPublicReport importiert öffentliche Schlüssel; bei bereits bekannten
// Schlüsseln werden neue Fremd-Zertifizierungen übernommen (Merge), statt den
// Import zu verwerfen. ErrNoNewKeys nur, wenn weder neue Schlüssel noch neue
// Zertifizierungen dabei waren.
func (k *Keyring) ImportPublicReport(armored []byte) (ImportReport, error) {
	var rep ImportReport
	el, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(armored))
	if err != nil {
		return rep, i18n.Wrap(err, "err.pgp.badkeyfile")
	}
	own := 0
	for _, e := range el {
		if k.findByFingerprint(k.private, fpr(e)) != nil {
			own++
		}
		if existing := k.findByFingerprint(k.public, fpr(e)); existing != nil {
			if n := k.mergeCerts(existing, e); n > 0 {
				rep.Merged += n
				rep.MergedFprs = append(rep.MergedFprs, fpr(existing))
				if k.findByFingerprint(k.private, fpr(existing)) != nil {
					rep.MergedOwn = true
				}
			}
			continue
		}
		pub := e
		if e.PrivateKey != nil {
			if pub, err = publicHalf(e); err != nil {
				return rep, err
			}
		}
		k.public = append(k.public, pub)
		rep.Keys = append(rep.Keys, infoOf(pub, false))
	}
	if len(rep.Keys) == 0 && rep.Merged == 0 {
		rep.OnlyOwn = own > 0 && own == len(el)
		return rep, ErrNoNewKeys
	}
	return rep, k.Save()
}

// mergeCerts übernimmt neue Fremd-Zertifizierungen von src auf vorhandene
// UIDs von dst. Neue UIDs, Subkeys und Selbstsignaturen bleiben außen vor.
// Ist der Signierer bekannt, muss die Zertifizierung gültig sein.
func (k *Keyring) mergeCerts(dst, src *Entity) int {
	n := 0
	for uid, sident := range src.Identities {
		dident, ok := dst.Identities[uid]
		if !ok {
			continue
		}
		have := map[string]bool{}
		for _, s := range dident.Signatures {
			have[sigKey(s)] = true
		}
		for _, sig := range sident.Signatures {
			if !isCertType(sig.SigType) || sig.CheckKeyIdOrFingerprint(dst.PrimaryKey) || have[sigKey(sig)] {
				continue
			}
			if signer := k.signerOf(sig); signer != nil &&
				signer.PrimaryKey.VerifyUserIdSignature(uid, dst.PrimaryKey, sig) != nil {
				continue
			}
			dident.Signatures = append(dident.Signatures, sig)
			have[sigKey(sig)] = true
			n++
		}
	}
	return n
}

// HasMyValidCert meldet, ob ein eigener Schlüssel diesen Schlüssel gültig
// (und nicht widerrufen) zertifiziert hat.
func (k *Keyring) HasMyValidCert(fingerprint string) bool {
	for _, c := range k.Certifications(fingerprint) {
		if c.Mine && c.Status == CertValid && !c.Revoked {
			return true
		}
	}
	return false
}

// PrimaryUID liefert die primäre Identität des Schlüssels (leer, wenn
// unbekannt).
func (k *Keyring) PrimaryUID(fingerprint string) string {
	e := k.findByFingerprint(k.public, fingerprint)
	if e == nil {
		return ""
	}
	if id := e.PrimaryIdentity(); id != nil {
		return id.Name
	}
	return ""
}

// CanEncryptTo meldet, ob der Schlüssel einen gültigen Verschlüsselungs-
// Unterschlüssel hat (Voraussetzung für die verschlüsselte Rücksendung).
func (k *Keyring) CanEncryptTo(fingerprint string) bool {
	e := k.findByFingerprint(k.public, fingerprint)
	if e == nil {
		return false
	}
	_, ok := e.EncryptionKey(time.Now())
	return ok
}

// UIDs liefert die (nicht widerrufenen) UIDs eines Pubring-Schlüssels,
// sortiert, primäre zuerst.
func (k *Keyring) UIDs(fingerprint string) []string {
	e := k.findByFingerprint(k.public, fingerprint)
	if e == nil {
		return nil
	}
	now := time.Now()
	var out []string
	for _, uid := range sortedUIDs(e) {
		if !e.Identities[uid].Revoked(now) {
			out = append(out, uid)
		}
	}
	return out
}

// UIDEmail liefert die E-Mail-Adresse einer UID des Schlüssels (leer, wenn
// die UID keine Adresse trägt).
func (k *Keyring) UIDEmail(fingerprint, uid string) string {
	e := k.findByFingerprint(k.public, fingerprint)
	if e == nil {
		return ""
	}
	if id, ok := e.Identities[uid]; ok && id.UserId != nil {
		return id.UserId.Email
	}
	return ""
}

// signerOf sucht den Aussteller einer Signatur im Pubring.
func (k *Keyring) signerOf(sig *packet.Signature) *Entity {
	for _, e := range k.public {
		if sig.CheckKeyIdOrFingerprint(e.PrimaryKey) {
			return e
		}
	}
	return nil
}

// revokedBy prüft, ob signer seine Zertifizierung dieser UID nach certTime
// widerrufen hat (gültige Certification-Revocation 0x30).
func revokedBy(ident *openpgp.Identity, signer, target *Entity, uid string, certTime time.Time) bool {
	for _, s := range ident.Signatures {
		if s.SigType != packet.SigTypeCertificationRevocation || !s.CheckKeyIdOrFingerprint(signer.PrimaryKey) {
			continue
		}
		if !s.CreationTime.Before(certTime) &&
			signer.PrimaryKey.VerifyUserIdSignature(uid, target.PrimaryKey, s) == nil {
			return true
		}
	}
	return false
}

func isCertType(t packet.SignatureType) bool {
	switch t {
	case packet.SigTypeGenericCert, packet.SigTypePersonaCert, packet.SigTypeCasualCert,
		packet.SigTypePositiveCert, packet.SigTypeCertificationRevocation:
		return true
	}
	return false
}

func issuerKeyID(sig *packet.Signature) string {
	if sig.IssuerKeyId != nil {
		return fmt.Sprintf("%016X", *sig.IssuerKeyId)
	}
	if n := len(sig.IssuerFingerprint); n >= 8 {
		return strings.ToUpper(fmt.Sprintf("%x", sig.IssuerFingerprint[n-8:]))
	}
	return "?"
}

// sigKey identifiziert eine Signatur für die Duplikaterkennung.
func sigKey(sig *packet.Signature) string {
	return fmt.Sprintf("%d/%d/%s", sig.SigType, sig.CreationTime.Unix(), issuerKeyID(sig))
}

// sortedUIDs liefert die UIDs eines Entity, primäre zuerst, sonst alphabetisch.
func sortedUIDs(e *Entity) []string {
	var uids []string
	for uid := range e.Identities {
		uids = append(uids, uid)
	}
	sort.Strings(uids)
	if prim := e.PrimaryIdentity(); prim != nil {
		for i, u := range uids {
			if u == prim.Name && i > 0 {
				uids = append([]string{u}, append(uids[:i:i], uids[i+1:]...)...)
				break
			}
		}
	}
	return uids
}
