package pgpcard

import (
	"encoding/hex"
	"strings"
	"testing"

	iso "cunicu.li/go-iso7816"
	opc "cunicu.li/go-openpgp-card"
)

// replayCard spielt aufgezeichnete Kartenantworten ab (Schlüssel: INS P1 P2
// als Hex). Unbekannte Kommandos → 6A88.
type replayCard struct {
	resp map[string][]byte
	log  [][]byte // alle Kommandos außer GET DATA / SELECT
}

func (r *replayCard) Transmit(cmd []byte) ([]byte, error) {
	if cmd[1] != 0xCA && cmd[1] != 0xA4 {
		r.log = append(r.log, append([]byte(nil), cmd...))
		return []byte{0x90, 0x00}, nil
	}
	if v, ok := r.resp[hex.EncodeToString(cmd[1:4])]; ok {
		return append(append([]byte(nil), v...), 0x90, 0x00), nil
	}
	return []byte{0x6A, 0x88}, nil
}
func (r *replayCard) BeginTransaction() error { return nil }
func (r *replayCard) EndTransaction() error   { return nil }
func (r *replayCard) Close() error            { return nil }
func (r *replayCard) Base() iso.PCSCCard      { return r }

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// zeitControl34 sind die echten Antworten einer leeren ZeitControl-Karte
// (Spec 3.4, Serial 0000D676; TestHWRawDump): 6E und FA ohne äußeres Tag,
// 7F66 direkt in 6E.
func zeitControl34(t *testing.T) *replayCard {
	zeros := strings.Repeat("00", 60)
	return &replayCard{resp: map[string][]byte{
		"a40400": nil,
		"ca006e": mustHex(t, "4F10D276000124010304000500 00D6760000 5F520A0031F573C001600590 00"+
			"7F6608020208000202 0800"+
			"7381BF C00A7F000800080008000001 C10601080000200 0C20601080000 2000C306010800002000"+
			"C407004040400300 03C53C"+zeros+"C63C"+zeros+"CD0C"+strings.Repeat("00", 12)+"DE0601000200 0300"),
		"ca0065": mustHex(t, "5B005F2D0264655F350130"),
		"ca007a": mustHex(t, "9303000000"),
		"ca00f9": mustHex(t, "810100"),
		"ca00fa": mustHex(t, "C10601080000200 0C106010C00002000C1060110000020 00"+
			"C109132A8648CE3D030107C106132B81040022C106132B81040023"+
			"C10A132B2403030208010107C10A132B240303020801010BC10A132B240303020801010D"+
			"C20601080000200 0C206010C00002000C20601100000200 0"+
			"C209122A8648CE3D030107C206122B81040022C206122B81040023"+
			"C20A122B2403030208010107C20A122B240303020801010BC20A122B240303020801010D"+
			"C30601080000200 0C306010C00002000C30601100000200 0"+
			"C309132A8648CE3D030107C306132B81040022C306132B81040023"+
			"C30A132B2403030208010107C30A132B240303020801010BC30A132B240303020801010D"),
	}}
}

func TestCompatZeitControl34(t *testing.T) {
	card, err := opc.NewCard(iso.NewCard(&compatCard{PCSCCard: zeitControl34(t)}))
	if err != nil {
		t.Fatalf("NewCard: %v", err)
	}
	li := card.ApplicationRelated.LengthInfo
	if li.MaxCommandLength != 2048 || li.MaxResponseLength != 2048 {
		t.Errorf("LengthInfo: %+v", li)
	}
	if card.AID.Version.Major != 3 || card.AID.Version.Minor != 4 {
		t.Errorf("Version: %v", card.AID.Version)
	}
	algos, err := card.SupportedAlgorithms()
	if err != nil {
		t.Fatalf("SupportedAlgorithms: %v", err)
	}
	for _, ref := range []opc.KeyRef{opc.KeySign, opc.KeyDecrypt, opc.KeyAuthn} {
		var rsa []int
		for _, a := range algos[ref] {
			if a.Algorithm == opc.AlgPubkeyRSA {
				rsa = append(rsa, a.LengthModulus)
			}
		}
		if len(algos[ref]) != 9 || len(rsa) != 3 || rsa[0] != 2048 || rsa[2] != 4096 {
			t.Errorf("%v: %d Algorithmen, RSA %v", ref, len(algos[ref]), rsa)
		}
	}
	info := buildInfo(card, "test")
	if len(info.RSABits) != 3 || info.Slots[SlotSign].Present || info.Version != "3.4" ||
		info.PINRetries != 3 || info.AdminRetries != 3 || !info.SigPINForced || info.MaxPINLen != 64 ||
		!info.KDFSupported || info.KDFActive || !info.KDFPossible() || !info.CanReset || len(info.Algos) != 9 {
		t.Errorf("Info: %+v", info)
	}
}

func TestFixAlgInfo(t *testing.T) {
	// Library-Abzug nachbilden: Byte [3] -= 2.
	lib := func(b []byte) []byte {
		b = append([]byte(nil), b...)
		b[3] -= 2
		return b
	}
	body := mustHex(t, "C10601080000200 0")
	// Ohne äußeres Tag: wird verpackt, nach dem Abzug korrekt.
	if got := lib(fixAlgInfo(body)); hex.EncodeToString(got) != "fa08"+hex.EncodeToString(body) {
		t.Errorf("ohne Tag: % X", got)
	}
	// Stimmige Länge mit Tag: ebenso.
	tagged := append([]byte{0xFA, 0x08}, body...)
	if got := lib(fixAlgInfo(tagged)); hex.EncodeToString(got) != hex.EncodeToString(tagged) {
		t.Errorf("mit Tag: % X", got)
	}
	// YubiKey-Form (Länge um 2 zu groß): unverändert.
	yk := append([]byte{0xFA, 0x0A}, body...)
	if got := fixAlgInfo(yk); hex.EncodeToString(got) != hex.EncodeToString(yk) {
		t.Errorf("YubiKey: % X", got)
	}
}

func TestRelocateLengthInfo(t *testing.T) {
	in := mustHex(t, "4F0101 7F66080202080002020800 7303C00100")
	out, ok := relocateLengthInfo(in)
	if !ok || hex.EncodeToString(out) != "4f0101"+"730ec00100"+"7f66080202080002020800" {
		t.Fatalf("ok=%v % X", ok, out)
	}
	if _, ok := relocateLengthInfo(mustHex(t, "4F0101 7303C00100")); ok {
		t.Error("ohne 7F66: ok erwartet false")
	}
}

// TestKDFPINCommands: Bei aktivem KDF müssen VERIFY (81 UND 82), CHANGE
// REFERENCE DATA und RESET RETRY COUNTER den PW1- bzw. PW3-Hash senden –
// nie die Klartext-PIN, und für 82 mit dem PW1-Salt (nicht dem RC-Salt).
func TestKDFPINCommands(t *testing.T) {
	rc := zeitControl34(t)
	// KDF aktiv: iteriertes S2K, SHA-256, 65536 Iterationen, drei Salts.
	rc.resp["ca00f9"] = mustHex(t, "810103 820108 830400010000"+
		"84081111111111111111 85082222222222222222 86083333333333333333")
	card, err := opc.NewCard(iso.NewCard(&compatCard{PCSCCard: rc}))
	if err != nil {
		t.Fatal(err)
	}
	k, err := card.GetKDF()
	if err != nil {
		t.Fatal(err)
	}
	hash := func(ref byte, pin string) string {
		b, err := k.DerivePassword(ref, pin)
		if err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(b)
	}
	s := &pcscSession{card: card}
	rc.log = nil
	if err := s.VerifyPIN("123456"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangePIN("123456", "654321"); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetPIN("12345678", "111111"); err != nil {
		t.Fatal(err)
	}
	pw1 := hash(opc.PW1, "123456")
	if pw1 == hash(opc.RC, "123456") {
		t.Fatal("Testaufbau: PW1- und RC-Salt müssen sich unterscheiden")
	}
	data := func(cmd []byte) string { return hex.EncodeToString(cmd[5:]) }
	want := []struct{ hdr, data string }{
		{"00200081", pw1},
		{"00200082", pw1},
		{"00240081", pw1 + hash(opc.PW1, "654321")},
		{"00200083", hash(opc.PW3, "12345678")},
		{"002c0281", hash(opc.PW1, "111111")},
	}
	if len(rc.log) != len(want) {
		t.Fatalf("%d Kommandos, erwartet %d", len(rc.log), len(want))
	}
	for i, w := range want {
		if got := hex.EncodeToString(rc.log[i][:4]); got != w.hdr || data(rc.log[i]) != w.data {
			t.Errorf("Kommando %d: %s %s, erwartet %s %s", i, got, data(rc.log[i]), w.hdr, w.data)
		}
	}
}
