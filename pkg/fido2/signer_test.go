package fido2

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// fakeBackend emuliert ein CTAP2-Token in Software: es signiert
// authData||SHA256(clientData) – exakt das, was sshd verifiziert.
type fakeBackend struct {
	edPriv  ed25519.PrivateKey
	ecPriv  *ecdsa.PrivateKey
	counter uint32
	flags   byte
	extraAD []byte // simuliert Extensions im authData
	pins    int    // zählt RequestPIN-Aufrufe über den Prompter
}

type fakePrompter struct{ pins *int }

func (p fakePrompter) RequestPIN(context.Context) (string, error) {
	if p.pins != nil {
		*p.pins++
	}
	return "123456", nil
}
func (fakePrompter) NotifyTouch() {}
func (fakePrompter) TouchDone()   {}

func (f *fakeBackend) GetAssertion(ctx context.Context, rpID string, clientData, credID []byte, prompt Prompter) (*Assertion, error) {
	if _, err := prompt.RequestPIN(ctx); err != nil {
		return nil, err
	}
	f.counter++
	rpHash := sha256.Sum256([]byte(rpID))
	authData := make([]byte, 37, 37+len(f.extraAD))
	copy(authData, rpHash[:])
	authData[32] = f.flags
	binary.BigEndian.PutUint32(authData[33:37], f.counter)
	authData = append(authData, f.extraAD...)

	cdh := sha256.Sum256(clientData)
	msg := append(append([]byte{}, authData...), cdh[:]...)

	var sig []byte
	var err error
	if f.edPriv != nil {
		sig = ed25519.Sign(f.edPriv, msg)
	} else {
		digest := sha256.Sum256(msg)
		sig, err = ecdsa.SignASN1(rand.Reader, f.ecPriv, digest[:])
		if err != nil {
			return nil, err
		}
	}
	return &Assertion{AuthData: authData, Signature: sig, CredentialID: credID}, nil
}

func (f *fakeBackend) MakeResidentCredential(context.Context, string, string, Prompter) (*Stub, error) {
	return nil, ErrNotSupported
}
func (f *fakeBackend) EnumerateSSHCredentials(context.Context, string, Prompter) ([]Stub, error) {
	return nil, ErrNotSupported
}

func newEDSigner(t *testing.T, fb *fakeBackend) *Signer {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fb.edPriv = priv
	if fb.flags == 0 {
		fb.flags = flagUserPresent
	}
	stub := &Stub{
		Version: 1, Algorithm: AlgoED25519, Application: SSHApplication,
		PublicKey: MarshalED25519PublicKey(pub, SSHApplication),
		Resident:  true, Created: time.Now(),
	}
	s, err := NewSigner(stub, fakePrompter{pins: &fb.pins})
	if err != nil {
		t.Fatal(err)
	}
	s.backend = fb
	return s
}

func TestSignED25519VerifiesWithXCrypto(t *testing.T) {
	fb := &fakeBackend{}
	s := newEDSigner(t, fb)
	data := []byte("ssh session data")
	sig, err := s.Sign(rand.Reader, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublicKey().Verify(data, sig); err != nil {
		t.Fatalf("x/crypto-Verify lehnt ab: %v", err)
	}
	if fb.pins != 1 {
		t.Fatalf("PIN muss genau einmal angefragt werden, war %d", fb.pins)
	}
	// Zweite Signatur: neuer Counter, neue PIN-Abfrage (kein Cache).
	if _, err := s.Sign(rand.Reader, data); err != nil {
		t.Fatal(err)
	}
	if fb.pins != 2 {
		t.Fatalf("PIN darf nicht gecacht werden (Aufrufe: %d)", fb.pins)
	}
}

func TestSignECDSAVerifiesWithXCrypto(t *testing.T) {
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fb := &fakeBackend{ecPriv: ec, flags: flagUserPresent | flagUserVerified}
	stub := &Stub{
		Version: 1, Algorithm: AlgoECDSA, Application: SSHApplication,
		PublicKey: MarshalECDSAPublicKey(ec.X, ec.Y, SSHApplication),
		Resident:  true, Created: time.Now(),
	}
	s, err := NewSigner(stub, fakePrompter{})
	if err != nil {
		t.Fatal(err)
	}
	s.backend = fb
	data := []byte("ecdsa session data")
	sig, err := s.Sign(rand.Reader, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublicKey().Verify(data, sig); err != nil {
		t.Fatalf("x/crypto-Verify lehnt ab: %v", err)
	}
}

func TestSignRejectsMissingUserPresence(t *testing.T) {
	fb := &fakeBackend{flags: flagUserVerified} // UP fehlt
	s := newEDSigner(t, fb)
	fb.flags = flagUserVerified // newEDSigner setzt UP, wieder entfernen
	if _, err := s.Sign(rand.Reader, []byte("x")); !errors.Is(err, ErrNoUserPresence) {
		t.Fatalf("erwartet ErrNoUserPresence, war %v", err)
	}
}

func TestSignRejectsExtensionAuthData(t *testing.T) {
	fb := &fakeBackend{extraAD: []byte{0xa1, 0x01, 0xf5}} // CBOR-Extension
	s := newEDSigner(t, fb)
	if _, err := s.Sign(rand.Reader, []byte("x")); err == nil {
		t.Fatal("authData > 37 Bytes muss abgelehnt werden")
	}
}

func TestTamperedSignatureFailsVerify(t *testing.T) {
	fb := &fakeBackend{}
	s := newEDSigner(t, fb)
	data := []byte("session")
	sig, err := s.Sign(rand.Reader, data)
	if err != nil {
		t.Fatal(err)
	}
	// Flags/Counter manipulieren → Verify muss scheitern.
	bad := *sig
	bad.Rest = ssh.Marshal(struct {
		Flags   byte
		Counter uint32
	}{flagUserPresent, 999999})
	if err := s.PublicKey().Verify(data, &bad); err == nil {
		t.Fatal("manipulierter Counter darf nicht verifizieren")
	}
	// Andere Application → anderer Schlüssel-Hash → Verify scheitert.
	otherStub := *s.stub
	otherStub.PublicKey = MarshalED25519PublicKey(
		s.pub.(ssh.CryptoPublicKey).CryptoPublicKey().(ed25519.PublicKey), "ssh:andere")
	otherPub, err := ssh.ParsePublicKey(otherStub.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := otherPub.Verify(data, sig); err == nil {
		t.Fatal("falsche Application darf nicht verifizieren")
	}
}

func TestStubRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fido2_key.json")
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	st := &Stub{
		Version: 1, Algorithm: AlgoED25519, Application: SSHApplication,
		CredentialID: []byte{1, 2, 3}, Resident: true, Created: time.Now(),
		PublicKey: MarshalED25519PublicKey(pub, SSHApplication), Comment: "ub61af8",
	}
	if err := st.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadStub(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Algorithm != st.Algorithm || string(got.CredentialID) != string(st.CredentialID) ||
		string(got.PublicKey) != string(st.PublicKey) || got.Comment != st.Comment {
		t.Fatalf("Roundtrip-Verlust: %+v", got)
	}
	if _, err := got.SSHPublicKey(); err != nil {
		t.Fatal(err)
	}
}

// TestFullSSHHandshake beweist die Wire-Korrektheit Ende-zu-Ende: ein echter
// SSH-Handshake über net.Pipe, Server akzeptiert den sk-Publickey.
func TestFullSSHHandshake(t *testing.T) {
	fb := &fakeBackend{}
	s := newEDSigner(t, fb)

	// Server-Hostkey (normaler ed25519).
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	wantKey := string(s.PublicKey().Marshal())
	serverCfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) != wantKey {
				return nil, errors.New("unbekannter Schlüssel")
			}
			return &ssh.Permissions{}, nil
		},
	}
	serverCfg.AddHostKey(hostSigner)

	// TCP-Loopback statt net.Pipe: der SSH-Handshake schreibt auf beiden
	// Seiten gleichzeitig und würde auf der ungepufferten Pipe deadlocken
	// (x/crypto testet aus demselben Grund über TCP).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	connCh := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		connCh <- c
	}()
	cConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	sConn := <-connCh
	errCh := make(chan error, 1)
	go func() {
		_, chans, reqs, err := ssh.NewServerConn(sConn, serverCfg)
		if err != nil {
			errCh <- err
			return
		}
		go ssh.DiscardRequests(reqs)
		for ch := range chans {
			ch.Reject(ssh.Prohibited, "test")
		}
		errCh <- nil
	}()

	clientCfg := &ssh.ClientConfig{
		User:            "uucp",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(s)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	conn, _, _, err := ssh.NewClientConn(cConn, "pipe", clientCfg)
	if err != nil {
		t.Fatalf("Handshake fehlgeschlagen: %v", err)
	}
	conn.Close()
}
