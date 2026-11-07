package pgpcard

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	iso "cunicu.li/go-iso7816"
	pcscdrv "cunicu.li/go-iso7816/drivers/pcsc"
	opc "cunicu.li/go-openpgp-card"
	"github.com/ebfe/scard"
)

// pcscBackend spricht Karten über PC/SC an (macOS: PCSC.framework, Windows:
// winscard.dll – beides eingebaut; Linux: libpcsclite + laufender pcscd).
// Einziger Ort, der die Kartenbibliothek (cunicu.li/go-openpgp-card) kennt.
type pcscBackend struct{}

var slotRefs = map[Slot]opc.KeyRef{
	SlotSign:    opc.KeySign,
	SlotDecrypt: opc.KeyDecrypt,
	SlotAuth:    opc.KeyAuthn,
}

func (b *pcscBackend) Cards(_ context.Context) ([]CardInfo, error) {
	sctx, err := scard.EstablishContext()
	if err != nil {
		// Typisch Linux ohne pcscd: Dienst nicht erreichbar.
		return nil, fmt.Errorf("%w (%v)", ErrNoCard, err)
	}
	defer func() { _ = sctx.Release() }()

	readers, err := sctx.ListReaders()
	if err != nil || len(readers) == 0 {
		return nil, ErrNoCard
	}
	var infos []CardInfo
	var lastErr error
	for _, r := range readers {
		card, err := openOnReader(sctx, r)
		if err != nil {
			lastErr = err
			continue
		}
		infos = append(infos, buildInfo(card, r))
		_ = card.Close()
	}
	if len(infos) == 0 {
		if lastErr != nil {
			return nil, mapCardErr(lastErr)
		}
		return nil, ErrNoCard
	}
	return infos, nil
}

func (b *pcscBackend) Open(_ context.Context, serial string) (Session, error) {
	sctx, err := scard.EstablishContext()
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrNoCard, err)
	}
	readers, err := sctx.ListReaders()
	if err != nil || len(readers) == 0 {
		_ = sctx.Release()
		return nil, ErrNoCard
	}
	found := false
	var lastErr error
	for _, r := range readers {
		card, err := openOnReader(sctx, r)
		if err != nil {
			lastErr = err
			continue
		}
		info := buildInfo(card, r)
		if serial == "" || info.Serial == serial {
			return &pcscSession{sctx: sctx, card: card, info: info}, nil
		}
		found = true
		_ = card.Close()
	}
	_ = sctx.Release()
	if found {
		return nil, ErrWrongCard
	}
	if lastErr != nil {
		return nil, mapCardErr(lastErr)
	}
	return nil, ErrNoCard
}

// compatCard umhüllt den PC/SC-Transport und gleicht Abweichungen zwischen
// Karten und go-openpgp-card aus (GnuPG toleriert jeweils beide Formen):
//   - Karten wie ZeitControl v2.x und 3.4 liefern die konstruierten DOs
//     6E/65/7A/FA bei GET DATA OHNE das äußere Tag; die Bibliothek erwartet
//     es (sonst „missing tag"). Die Antwort wird nachverpackt.
//   - Spec 3.x legt das Extended-Length-DO 7F66 direkt in 6E; die
//     Bibliothek sucht es nur im Container 73 – es wird dorthin verschoben.
//     v2-Karten haben es gar nicht – dann werden Standardwerte eingesetzt.
//   - FA: siehe fixAlgInfo bzw. synthAlgInfo.
//
// Verkettete Antworten (SW 61xx) bleiben unberührt – die betroffenen DOs
// passen in eine einzelne (ggf. erweiterte) Antwort.
type compatCard struct {
	iso.PCSCCard
	curAttrs map[byte][]byte // C1/C2/C3: aktuelle Algorithmus-Attribute der Slots
}

func (c *compatCard) Transmit(cmd []byte) ([]byte, error) {
	cmd = rewriteImportCRT(cmd)
	resp, err := c.PCSCCard.Transmit(cmd)
	if err != nil || len(cmd) < 4 || len(resp) < 2 {
		return resp, err
	}
	if cmd[1] != 0xCA || cmd[2] != 0x00 { // nur GET DATA
		return resp, err
	}
	if cmd[3] == 0xFA && len(resp) > 2 && resp[len(resp)-2] == 0x90 && resp[len(resp)-1] == 0x00 {
		return append(fixAlgInfo(resp[:len(resp)-2]), 0x90, 0x00), nil
	}
	// Algorithm-Information-DO (FA): existiert erst ab Card-Spec 3.4. Meldet
	// die Karte 6A88 (referenced data not found), wird die Antwort syntheti-
	// siert: je Slot (C1/C2/C3) die Standard-RSA-Längen 2048/3072/4096 mit
	// dem TATSÄCHLICHEN Exponenten-/Importformat der Karte (aus der zuvor
	// gelesenen 6E-Antwort, Fallback 32-Bit-Exponent / Standardformat). Die
	// Karte bleibt die letzte Instanz: eine wirklich nicht unterstützte Länge
	// lehnt sie beim Setzen der Attribute bzw. beim Import ab. Kodierung so,
	// dass sie NACH dem YubiKey-Workaround der Bibliothek (algInfo[3] -= 2)
	// korrekt parst: erstes Längenbyte (Index 3) vorab um 2 erhöht.
	if cmd[3] == 0xFA && len(resp) >= 2 && resp[len(resp)-2] == 0x6A && resp[len(resp)-1] == 0x88 {
		body := c.synthAlgInfo()
		if len(body) == 0 || len(body) > 253 {
			return resp, err // nichts Brauchbares – Original-6A88 durchreichen
		}
		synth := append([]byte{0xFA, byte(len(body))}, body...) // #nosec G115 -- len(body)<=253 (oben geprüft)
		synth[3] += 2
		return append(synth, 0x90, 0x00), nil
	}
	tag := cmd[3]
	if tag != 0x6E && tag != 0x65 && tag != 0x7A {
		return resp, err
	}
	data, sw1, sw2 := resp[:len(resp)-2], resp[len(resp)-2], resp[len(resp)-1]
	if sw1 != 0x90 || sw2 != 0x00 || len(data) == 0 {
		return resp, err
	}
	changed := true // fehlendes äußeres Tag wird in jedem Fall ergänzt
	if data[0] == tag {
		l, n := decodeBERLen(data[1:])
		if n == 0 || 1+n+l != len(data) {
			return resp, err
		}
		data, changed = data[1+n:], false
	}
	if tag == 0x6E {
		c.captureAttrs(data) // für eine evtl. folgende FA-Synthese
		// Ohne 7F66 im Container 73 setzt die Bibliothek MaxResponseLength=0
		// und alle Folge-GET-DATAs scheitern mit „wrong length".
		if moved, ok := relocateLengthInfo(data); ok {
			data, changed = moved, true // Spec 3.x: 7F66 direkt in 6E
		} else if !bytes.Contains(data, []byte{0x7F, 0x66, 0x08}) {
			data, changed = injectLengthInfo(data, defaultLengthInfo), true // v2: fehlt ganz
		}
	}
	if !changed {
		return resp, err
	}
	wrapped := append([]byte{tag}, berLength(len(data))...)
	wrapped = append(wrapped, data...)
	return append(wrapped, sw1, sw2), nil
}

// fixAlgInfo bereitet die Algorithm-Information (FA, Spec 3.4) für die
// Bibliothek auf: Diese zieht pauschal 2 vom Byte [3] ab (Workaround für
// YubiKeys, deren FA-Länge um 2 zu groß ist). Spec-konforme Antworten –
// mit stimmiger Länge oder ganz ohne äußeres Tag (ZeitControl 3.4) – werden
// mit Tag versehen und Byte [3] vorab um 2 erhöht (modulo 256, die
// Bibliothek rechnet ebenso), damit sie nach dem Abzug korrekt parsen.
// Unstimmige Längen (YubiKey) bleiben unverändert.
func fixAlgInfo(data []byte) []byte {
	var out []byte
	if data[0] == 0xFA {
		l, n := decodeBERLen(data[1:])
		if n == 0 || 1+n+l != len(data) {
			return data
		}
		out = append([]byte(nil), data...)
	} else {
		out = append([]byte{0xFA}, berLength(len(data))...)
		out = append(out, data...)
	}
	if len(out) >= 4 {
		out[3] += 2
	}
	return out
}

// rewriteImportCRT ersetzt im Schlüssel-Import (PUT DATA odd, INS DB,
// P1P2 3FFF, mit der Extended Header List DO 4D in den Daten) die nicht-leere
// Control-Reference-Template-Form „B6 03 84 01 01" (bzw. B8/A4) durch die
// leere „B6 00". go-openpgp-card sendet die YubiKey-tolerierte Langform mit
// dem Key-Reference-DO (84 01 xx), die in den Import-Header laut Spec NICHT
// gehört; ältere Karten (ZeitControl v2.x) lehnen sie mit 6A80 ab. Die leere
// Form ist die von GnuPG benutzte und von allen Karten (YubiKey eingeschlos-
// sen) akzeptierte Variante – daher unbedingt, nicht kartenspezifisch.
// Sowohl die 4D-Länge als auch das äußere Lc werden neu kodiert.
func rewriteImportCRT(cmd []byte) []byte {
	if len(cmd) < 7 || cmd[1] != 0xDB || cmd[2] != 0x3F || cmd[3] != 0xFF {
		return cmd
	}
	// Äußeres Lc (hier immer erweitert: 00 hi lo) und Datenteil.
	if cmd[4] != 0x00 {
		return cmd
	}
	lc := int(cmd[5])<<8 | int(cmd[6])
	if 7+lc > len(cmd) {
		return cmd
	}
	data := cmd[7 : 7+lc]
	// DO 4D mit erweiterter Länge (82 hi lo).
	if len(data) < 4 || data[0] != 0x4D || data[1] != 0x82 {
		return cmd
	}
	inner := data[4:] // Wert der 4D (beginnt mit der CRT)
	if len(inner) < 5 {
		return cmd
	}
	crtTag := inner[0]
	if (crtTag != 0xB6 && crtTag != 0xB8 && crtTag != 0xA4) ||
		inner[1] != 0x03 || inner[2] != 0x84 || inner[3] != 0x01 {
		return cmd // nicht die erwartete Langform – unverändert
	}
	newInner := append([]byte{crtTag, 0x00}, inner[5:]...)
	newLen := len(newInner)
	newData := append([]byte{0x4D, 0x82, byte(newLen >> 8), byte(newLen)}, newInner...) // #nosec G115 -- 4D-Länge, durch APDU-Größe begrenzt
	out := append([]byte(nil), cmd[:4]...)
	out = append(out, 0x00, byte(len(newData)>>8), byte(len(newData))) // #nosec G115 -- Lc, durch APDU-Größe begrenzt
	out = append(out, newData...)
	// evtl. Le-Byte am Ende übernehmen
	if 7+lc < len(cmd) {
		out = append(out, cmd[7+lc:]...)
	}
	return out
}

// synthAlgInfo baut den FA-Inhalt: je Slot (C1/C2/C3) RSA 2048/3072/4096 mit
// dem aus der 6E-Antwort gelesenen Exponenten-/Importformat (Fallback 0x0020
// / 0x00). Format je Eintrag: 01 <mod:2> <expLen:2> <importFmt:1>.
func (c *compatCard) synthAlgInfo() []byte {
	var body []byte
	for _, keyTag := range []byte{0xC1, 0xC2, 0xC3} {
		expHi, expLo, fmtB := byte(0x00), byte(0x20), byte(0x00)
		if a := c.curAttrs[keyTag]; len(a) == 6 && a[0] == 0x01 {
			expHi, expLo, fmtB = a[3], a[4], a[5]
		}
		for _, mod := range []int{2048, 3072, 4096} {
			body = append(body, keyTag, 0x06, 0x01, byte(mod>>8), byte(mod), expHi, expLo, fmtB) // #nosec G115 -- RSA-Modulusgröße, feste Werte
		}
	}
	return body
}

// captureAttrs liest die aktuellen Slot-Algorithmus-Attribute (C1/C2/C3) aus
// der 6E-Antwort und merkt sie für eine spätere FA-Synthese. Einfache Suche
// nach „<tag> 06 <6 Bytes>" (RSA-Attribute sind genau 6 Bytes); andere
// Längen werden anhand des BER-Längenbytes übernommen.
func (c *compatCard) captureAttrs(data []byte) {
	if c.curAttrs == nil {
		c.curAttrs = map[byte][]byte{}
	}
	for _, keyTag := range []byte{0xC1, 0xC2, 0xC3} {
		idx := bytes.IndexByte(data, keyTag)
		for idx >= 0 && idx+1 < len(data) {
			l := int(data[idx+1])
			if l > 0 && l < 0x80 && idx+2+l <= len(data) {
				c.curAttrs[keyTag] = append([]byte(nil), data[idx+2:idx+2+l]...)
				break
			}
			next := bytes.IndexByte(data[idx+1:], keyTag)
			if next < 0 {
				break
			}
			idx += 1 + next
		}
	}
}

// defaultLengthInfo: synthetisches 7F66-DO (max. Kommando 255, max. Antwort
// 256 Bytes) für v2-Karten ohne eigenes DO.
var defaultLengthInfo = []byte{0x7F, 0x66, 0x08, 0x02, 0x02, 0x00, 0xFF, 0x02, 0x02, 0x01, 0x00}

// topTLV beschreibt ein DO auf oberster Ebene: Tag (1–2 Bytes) und die
// Grenzen [start, end) bzw. den Wertbeginn val.
type topTLV struct {
	tag             uint16
	start, val, end int
}

// topLevel zerlegt data in die DOs der obersten Ebene (nil bei Formfehler).
func topLevel(data []byte) []topTLV {
	var out []topTLV
	i := 0
	for i < len(data) {
		e := topTLV{start: i, tag: uint16(data[i])}
		i++
		if data[e.start]&0x1F == 0x1F { // zweites Tag-Byte
			if i >= len(data) {
				return nil
			}
			e.tag = e.tag<<8 | uint16(data[i])
			i++
		}
		l, n := decodeBERLen(data[i:])
		if n == 0 || i+n+l > len(data) {
			return nil
		}
		e.val, e.end = i+n, i+n+l
		i = e.end
		out = append(out, e)
	}
	return out
}

// injectLengthInfo hängt das 7F66-DO inj ans Ende des 73-Containers und
// korrigiert dessen Länge. Findet sich kein Container, bleibt data unverändert.
func injectLengthInfo(data, inj []byte) []byte {
	for _, e := range topLevel(data) {
		if e.tag != 0x73 {
			continue
		}
		newVal := append(append([]byte{}, data[e.val:e.end]...), inj...)
		out := append([]byte{}, data[:e.start]...)
		out = append(out, 0x73)
		out = append(out, berLength(len(newVal))...)
		out = append(out, newVal...)
		return append(out, data[e.end:]...)
	}
	return data
}

// relocateLengthInfo verschiebt ein direkt in 6E liegendes 7F66 (Spec 3.x)
// in den Container 73, wo die Bibliothek es erwartet. ok=false, wenn es kein
// 7F66 auf oberster Ebene oder keinen 73-Container gibt.
func relocateLengthInfo(data []byte) ([]byte, bool) {
	var li *topTLV
	has73 := false
	for _, e := range topLevel(data) {
		switch e.tag {
		case 0x7F66:
			e := e
			li = &e
		case 0x73:
			has73 = true
		}
	}
	if li == nil || !has73 {
		return nil, false
	}
	inj := append([]byte(nil), data[li.start:li.end]...)
	rest := append(append([]byte(nil), data[:li.start]...), data[li.end:]...)
	return injectLengthInfo(rest, inj), true
}

func decodeBERLen(b []byte) (length, consumed int) {
	if len(b) == 0 {
		return 0, 0
	}
	switch {
	case b[0] < 0x80:
		return int(b[0]), 1
	case b[0] == 0x81 && len(b) >= 2:
		return int(b[1]), 2
	case b[0] == 0x82 && len(b) >= 3:
		return int(b[1])<<8 | int(b[2]), 3
	}
	return 0, 0
}

func berLength(n int) []byte {
	switch {
	case n < 0x80:
		return []byte{byte(n)} // #nosec G115 -- n<0x80 in diesem Zweig
	case n <= 0xFF:
		return []byte{0x81, byte(n)}
	default:
		return []byte{0x82, byte(n >> 8), byte(n)} // #nosec G115 -- BER-Längenbyte, durch APDU-Größe begrenzt
	}
}

// openOnReader verbindet einen Reader (shared, damit z.B. gpg parallel
// zumindest lesen kann) und selektiert das OpenPGP-Applet.
func openOnReader(sctx *scard.Context, reader string) (*opc.Card, error) {
	isoCard, err := pcscdrv.NewCard(sctx, reader, true)
	if err != nil {
		return nil, err
	}
	isoCard.PCSCCard = &compatCard{PCSCCard: isoCard.PCSCCard}
	card, err := opc.NewCard(isoCard)
	if err != nil {
		if closer, ok := isoCard.PCSCCard.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		return nil, err
	}
	return card, nil
}

// mapCardErr übersetzt PC/SC-/Kartenfehler in die Paket-Sentinels.
func mapCardErr(err error) error {
	var se scard.Error
	if errors.As(err, &se) {
		switch se {
		case scard.ErrSharingViolation:
			// Sauberes Sentinel ohne Roh-Fehler: "sharing violation" trägt
			// keine Zusatzinfo und würde die UI-Meldung nur verrauschen.
			return ErrCardBusy
		case scard.ErrNoSmartcard, scard.ErrUnknownReader, scard.ErrNoReadersAvailable:
			return fmt.Errorf("%w (%v)", ErrNoCard, err)
		}
	}
	var ae opc.AuthError
	if errors.As(err, &ae) {
		if ae.Retries <= 0 {
			return ErrPINBlocked
		}
		return &PINError{Retries: ae.Retries}
	}
	return err
}

func buildInfo(c *opc.Card, reader string) CardInfo {
	info := CardInfo{
		Serial:       fmt.Sprintf("%X", c.AID.Serial),
		Manufacturer: c.AID.Manufacturer.String(),
		Reader:       reader,
		Slots:        map[Slot]SlotInfo{},
	}
	for slot, ref := range slotRefs {
		ki, ok := c.Keys[ref]
		// Präsenz allein am Fingerprint (DO C5) festmachen – wie GnuPG. Das
		// separate Key-Status-DO liefern ältere Karten (ZeitControl v2.x)
		// nicht; ein non-zero Fingerprint bedeutet aber, dass ein Schlüssel
		// im Slot liegt.
		if !ok || allZero(ki.Fingerprint) {
			info.Slots[slot] = SlotInfo{}
			continue
		}
		info.Slots[slot] = SlotInfo{
			Present:     true,
			Fingerprint: FingerprintHex(ki.Fingerprint),
			Algo:        algoString(ki.AlgAttrs),
			Created:     ki.GenerationTime,
		}
	}
	// Unterstützte RSA-Längen (Algorithm-Information-DO, erst Card-Spec 3.4;
	// ältere Karten wie ZeitControl v2.x melden nichts → RSABits bleibt leer).
	if algos, err := c.SupportedAlgorithms(); err == nil {
		seen := map[int]bool{}
		for _, a := range algos[opc.KeySign] {
			if a.Algorithm == opc.AlgPubkeyRSA && !seen[a.LengthModulus] {
				seen[a.LengthModulus] = true
				info.RSABits = append(info.RSABits, a.LengthModulus)
			}
		}
		sort.Ints(info.RSABits)
		for _, a := range algos[opc.KeySign] {
			info.Algos = append(info.Algos, algoString(a))
		}
	}
	if c.ApplicationRelated != nil {
		info.Version = fmt.Sprintf("%d.%d", c.AID.Version.Major, c.AID.Version.Minor)
		ps := c.PasswordStatus
		info.PINRetries, info.AdminRetries = int(ps.AttemptsPW1), int(ps.AttemptsPW3)
		info.SigPINForced = ps.ValidityPW1 == 0
		info.MaxPINLen = int(min(ps.LengthPW1, ps.LengthPW3))
		info.KDFSupported = c.Capabilities.Flags&opc.CapKDF != 0
		lc := opc.LifeCycleStatus(c.HistoricalBytes.LifeCycleStatus)
		info.CanReset = lc == opc.LifeCycleStatusInitialized || lc == opc.LifeCycleStatusOperational
	}
	if c.SecuritySupportTemplate != nil {
		info.SigCount = c.SignatureCounter
	}
	if info.KDFSupported {
		if k, err := c.GetKDF(); err == nil {
			info.KDFActive = k.Algorithm != opc.AlgKDFNone
		}
	}
	return info
}

func algoString(a opc.AlgorithmAttributes) string {
	if a.Algorithm == opc.AlgPubkeyRSA {
		return fmt.Sprintf("rsa%d", a.LengthModulus)
	}
	// GnuPG-Namen statt ANSI-X9.62-Bezeichnungen (ansix9p256r1 → nistp256).
	c := a.Curve().String()
	if lc := strings.ToLower(c); strings.HasPrefix(lc, "ansix9p") {
		return "nistp" + strings.TrimSuffix(strings.TrimPrefix(lc, "ansix9p"), "r1")
	}
	if strings.HasPrefix(strings.ToLower(c), "brainpool") {
		return "brainpoolP" + c[len("brainpoolp"):]
	}
	return strings.ToLower(c)
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// FingerprintHex formatiert einen binären OpenPGP-Fingerprint wie im Stub
// gespeichert (Hex, Großschreibung).
func FingerprintHex(fp []byte) string {
	return strings.ToUpper(hex.EncodeToString(fp))
}

type pcscSession struct {
	sctx *scard.Context
	card *opc.Card
	info CardInfo
}

// Info liest den aktuellen Kartenzustand frisch (die Bibliothek aktualisiert
// c.Keys nach jedem Import), damit z.B. Slot-Fingerprints nach keytocard
// sichtbar sind. Fällt das Neulesen aus, bleibt der Open-Snapshot.
func (s *pcscSession) Info() CardInfo {
	if _, err := s.card.GetApplicationRelatedData(); err == nil {
		s.info = buildInfo(s.card, s.info.Reader)
	}
	return s.info
}

// VerifyPIN verifiziert die Nutzer-PIN für Signieren (PW1/81) UND
// Entschlüsseln (PW1forPSO/82) – gleiche PIN, getrennte Zustände auf der Karte.
// Eigene APDUs statt card.VerifyPassword: die Bibliothek hasht bei KDF für
// 82 mit dem Salt des Resetting Codes (0x82 = RC), die Karte erwartet aber
// den PW1-Hash.
func (s *pcscSession) VerifyPIN(pin string) error {
	pw, err := s.derive(opc.PW1, pin)
	if err != nil {
		return err
	}
	for _, ref := range []byte{opc.PW1, opc.PW1forPSO} {
		if err := s.sendAuth(iso.InsVerify, 0x00, ref, pw); err != nil {
			return err
		}
	}
	return nil
}

func (s *pcscSession) VerifyAdminPIN(pin string) error {
	pw, err := s.derive(opc.PW3, pin)
	if err != nil {
		return err
	}
	return s.sendAuth(iso.InsVerify, 0x00, opc.PW3, pw)
}

// derive liefert die PIN so, wie die Karte sie erwartet: bei aktivem KDF
// als S2K-Hash (Salt je PIN-Art), sonst unverändert.
func (s *pcscSession) derive(pwType byte, pin string) ([]byte, error) {
	if s.card.Capabilities.Flags&opc.CapKDF == 0 {
		return []byte(pin), nil
	}
	k, err := s.card.GetKDF()
	if err != nil {
		return nil, mapCardErr(err)
	}
	return k.DerivePassword(pwType, pin)
}

// sendAuth schickt ein PIN-Kommando und übersetzt die Statuswörter wie die
// Bibliothek (63Cx = falsche PIN, x Versuche übrig; 6983 = gesperrt).
func (s *pcscSession) sendAuth(ins iso.Instruction, p1, p2 byte, data []byte) error {
	_, err := s.card.Send(&iso.CAPDU{Ins: ins, P1: p1, P2: p2, Data: data})
	var code iso.Code
	if errors.As(err, &code) {
		switch {
		case code[0] == 0x63 && code[1]&0xF0 == 0xC0:
			err = opc.AuthError{Retries: int(code[1] & 0x0F)}
		case code == iso.Code{0x69, 0x83}:
			err = opc.AuthError{}
		}
	}
	return mapCardErr(err)
}

// changePW: CHANGE REFERENCE DATA mit (ggf. KDF-gehashter) alter und neuer
// PIN. Die Bibliothek (ChangePassword) hasht bei KDF nicht – ein PIN-Wechsel
// über sie machte eine KDF-Karte unbenutzbar.
func (s *pcscSession) changePW(pwType byte, current, next string) error {
	maxLen := int(s.card.PasswordStatus.LengthPW1)
	minLen := MinPINLen
	if pwType == opc.PW3 {
		maxLen, minLen = int(s.card.PasswordStatus.LengthPW3), MinAdminPINLen
	}
	if len(next) < minLen || (maxLen > 0 && len(next) > maxLen) {
		return ErrNotSupported
	}
	oldPW, err := s.derive(pwType, current)
	if err != nil {
		return err
	}
	newPW, err := s.derive(pwType, next)
	if err != nil {
		return err
	}
	return s.sendAuth(iso.InsChangeReferenceData, 0x00, pwType, append(oldPW, newPW...))
}

func (s *pcscSession) ResetPIN(adminPIN, newPIN string) error {
	if len(newPIN) < MinPINLen {
		return ErrNotSupported
	}
	if err := s.VerifyAdminPIN(adminPIN); err != nil {
		return err
	}
	pw, err := s.derive(opc.PW1, newPIN)
	if err != nil {
		return err
	}
	// P1=02: nach verifizierter Admin-PIN, Daten = neue PIN.
	return s.sendAuth(iso.InsResetRetryCounter, 0x02, opc.PW1, pw)
}

// kdfIterations: Iterationszahl für das S2K-Hashing (Stand GnuPG).
const kdfIterations = 0x02000000

func (s *pcscSession) SetupKDF(pin, adminPIN string) error {
	err := s.card.SetupKDF(opc.AlgKDFIterSaltedS2K, kdfIterations, pin, adminPIN)
	return mapCardErr(err)
}

// FactoryReset: TERMINATE DF + ACTIVATE FILE. Gelingt TERMINATE nicht
// direkt, werden PW3 und – für ältere Karten – PW1 absichtlich mit
// Zufallswerten gesperrt (wie GnuPGs factory-reset); jede Schleife ist
// begrenzt.
func (s *pcscSession) FactoryReset() error {
	lc := opc.LifeCycleStatus(s.card.HistoricalBytes.LifeCycleStatus)
	if lc != opc.LifeCycleStatusInitialized && lc != opc.LifeCycleStatusOperational {
		return ErrNotSupported
	}
	terminate := func() error {
		_, err := s.card.Send(&iso.CAPDU{Ins: iso.InsTerminateDF})
		return err
	}
	block := func(ref byte) {
		junk := []byte("uucpmail-reset-00")
		for i := 0; i < 16; i++ { // max. PIN-Versuche einer Karte: 15
			junk[len(junk)-1] = byte('a' + i)
			if err := s.sendAuth(iso.InsVerify, 0x00, ref, junk); errors.Is(err, ErrPINBlocked) {
				return
			}
		}
	}
	if lc == opc.LifeCycleStatusOperational {
		err := terminate()
		if err != nil {
			block(opc.PW3)
			err = terminate()
		}
		if err != nil {
			block(opc.PW1)
			err = terminate()
		}
		if err != nil {
			return mapCardErr(err)
		}
	}
	if _, err := s.card.Send(&iso.CAPDU{Ins: iso.InsActivateFile}); err != nil {
		return mapCardErr(err)
	}
	return nil
}

// go-openpgp-card v0.3.11 implementiert RSA-PSO:SIGN/DECIPHER NICHT (Stubs,
// nur EC). Wir rahmen die beiden Operationen daher selbst als APDU – die
// RSA-Rechnung bleibt auf der Karte. Für andere Algorithmen (EC) fällt der
// Code auf die Bibliotheks-Implementierung zurück.
func (s *pcscSession) Signer(pub crypto.PublicKey) (crypto.Signer, error) {
	if rpub, ok := pub.(*rsa.PublicKey); ok {
		return &rsaCardKey{card: s.card, pub: rpub}, nil
	}
	sk, err := s.card.PrivateKey(opc.KeySign, pub)
	if err != nil {
		return nil, mapCardErr(err)
	}
	signer, ok := sk.(crypto.Signer)
	if !ok {
		return nil, ErrNotSupported
	}
	return signer, nil
}

func (s *pcscSession) Decrypter(pub crypto.PublicKey) (crypto.Decrypter, error) {
	if rpub, ok := pub.(*rsa.PublicKey); ok {
		return &rsaCardKey{card: s.card, pub: rpub}, nil
	}
	sk, err := s.card.PrivateKey(opc.KeyDecrypt, pub)
	if err != nil {
		return nil, mapCardErr(err)
	}
	dec, ok := sk.(crypto.Decrypter)
	if !ok {
		return nil, ErrNotSupported
	}
	return dec, nil
}

// digestInfoPrefix: ASN.1-DER-Präfixe für RSASSA-PKCS1-v1_5 (RFC 8017). Die
// Karte (PSO:CDS) erwartet als Eingabe die DigestInfo-Struktur, nicht den
// nackten Hash, und führt Padding + RSA selbst aus.
var digestInfoPrefix = map[crypto.Hash][]byte{
	crypto.SHA256: {0x30, 0x31, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x01, 0x05, 0x00, 0x04, 0x20},
	crypto.SHA384: {0x30, 0x41, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x02, 0x05, 0x00, 0x04, 0x30},
	crypto.SHA512: {0x30, 0x51, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x03, 0x05, 0x00, 0x04, 0x40},
	crypto.SHA1:   {0x30, 0x21, 0x30, 0x09, 0x06, 0x05, 0x2b, 0x0e, 0x03, 0x02, 0x1a, 0x05, 0x00, 0x04, 0x14},
}

// rsaCardKey ist ein crypto.Signer UND crypto.Decrypter über die RSA-Slots
// der Karte (PSO:SIGN / MSE+PSO:DECIPHER). Vorher muss die PIN verifiziert
// sein (Session-VerifyPIN).
type rsaCardKey struct {
	card *opc.Card
	pub  *rsa.PublicKey
}

func (k *rsaCardKey) Public() crypto.PublicKey { return k.pub }

func (k *rsaCardKey) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if _, pss := opts.(*rsa.PSSOptions); pss {
		return nil, ErrNotSupported // OpenPGP nutzt PKCS#1 v1.5
	}
	prefix, ok := digestInfoPrefix[opts.HashFunc()]
	if !ok {
		return nil, ErrNotSupported
	}
	data := append(append([]byte{}, prefix...), digest...)
	// PSO: COMPUTE DIGITAL SIGNATURE (P1P2 = 9E 9A).
	sig, err := k.card.Send(&iso.CAPDU{Ins: iso.InsPerformSecurityOperation, P1: 0x9E, P2: 0x9A, Data: data, Ne: k.pub.Size()})
	if err != nil {
		return nil, mapCardErr(err)
	}
	return sig, nil
}

func (k *rsaCardKey) Decrypt(_ io.Reader, ciphertext []byte, _ crypto.DecrypterOpts) ([]byte, error) {
	// Kein MANAGE SECURITY ENVIRONMENT: PSO:DECIPHER verwendet den DEC-Slot
	// als Default; MSE wäre nur für einen abweichenden Schlüssel nötig (und
	// wird von älteren Karten teils gar nicht unterstützt).
	// PSO: DECIPHER (P1P2 = 80 86); führendes 0x00 = Padding-Indikator (RSA).
	data := append([]byte{0x00}, ciphertext...)
	plain, err := k.card.Send(&iso.CAPDU{Ins: iso.InsPerformSecurityOperation, P1: 0x80, P2: 0x86, Data: data, Ne: k.pub.Size()})
	if err != nil {
		return nil, mapCardErr(err)
	}
	return plain, nil
}

// ImportKey schreibt den Schlüssel in den Slot. Die Karte speichert den
// OpenPGP-Fingerprint, den die Bibliothek aus Public Key + Clock() berechnet –
// deshalb wird Clock auf die Erzeugungszeit des Schlüsselpakets gestellt,
// sonst passt der Karten-Fingerprint nicht zum Keyring.
func (s *pcscSession) ImportKey(slot Slot, priv crypto.PrivateKey, created time.Time) error {
	ref, ok := slotRefs[slot]
	if !ok {
		return ErrNotSupported
	}
	prevClock := s.card.Clock
	s.card.Clock = func() time.Time { return created }
	defer func() { s.card.Clock = prevClock }()
	if _, err := s.card.ImportKey(ref, priv); err != nil {
		return mapCardErr(err)
	}
	return nil
}

func (s *pcscSession) ChangePIN(current, next string) error {
	return s.changePW(opc.PW1, current, next)
}

func (s *pcscSession) ChangeAdminPIN(current, next string) error {
	return s.changePW(opc.PW3, current, next)
}

func (s *pcscSession) Close() error {
	err := s.card.Close()
	_ = s.sctx.Release()
	return err
}
