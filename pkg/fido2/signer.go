package fido2

import (
	"context"
	"crypto/elliptic"
	"encoding/asn1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"

	"golang.org/x/crypto/ssh"
)

// Signer implementiert ssh.Signer für sk-Schlüssel: jede Signatur läuft
// über das Token (PIN-Abfrage + Berührung bei jedem Verbindungsaufbau).
type Signer struct {
	stub    *Stub
	pub     ssh.PublicKey
	prompt  Prompter
	backend Backend // in Tests injizierbar; nil = DefaultBackend()
}

var _ ssh.Signer = (*Signer)(nil)

func NewSigner(stub *Stub, prompt Prompter) (*Signer, error) {
	pub, err := stub.SSHPublicKey()
	if err != nil {
		return nil, fmt.Errorf("fido2-stub: %w", err)
	}
	if pub.Type() != stub.Algorithm {
		return nil, fmt.Errorf("fido2-stub: Public Key ist %s, erwartet %s", pub.Type(), stub.Algorithm)
	}
	return &Signer{stub: stub, pub: pub, prompt: prompt}, nil
}

func (s *Signer) PublicKey() ssh.PublicKey { return s.pub }

// Sign lässt das Token die SSH-Session-Daten signieren und baut daraus die
// sk-Signatur (PROTOCOL.u2f): Blob = Rohsignatur (ed25519) bzw. r/s-mpints
// (ecdsa), Rest = flags-Byte + Counter aus dem authData.
func (s *Signer) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	b := s.backend
	if b == nil {
		var err error
		if b, err = DefaultBackend(); err != nil {
			return nil, err
		}
	}
	app := s.stub.Application
	if app == "" {
		app = SSHApplication
	}
	a, err := b.GetAssertion(context.Background(), app, data, s.stub.CredentialID, s.prompt)
	if err != nil {
		return nil, err
	}
	// sshd rekonstruiert SHA256(app)||flags||counter||SHA256(data) –
	// Extensions im authData würden die Verifikation brechen.
	if len(a.AuthData) != 37 {
		return nil, fmt.Errorf("fido2: authData hat %d Bytes (erwartet 37, Extensions sind nicht erlaubt)", len(a.AuthData))
	}
	flags := a.AuthData[32]
	if flags&flagUserPresent == 0 {
		return nil, ErrNoUserPresence
	}
	counter := binary.BigEndian.Uint32(a.AuthData[33:37])

	blob := a.Signature
	if s.stub.Algorithm == AlgoECDSA {
		if blob, err = derToSSHBlob(a.Signature); err != nil {
			return nil, err
		}
	}
	return &ssh.Signature{
		Format: s.stub.Algorithm,
		Blob:   blob,
		Rest: ssh.Marshal(struct {
			Flags   byte
			Counter uint32
		}{flags, counter}),
	}, nil
}

// derToSSHBlob wandelt eine ASN.1/DER-ECDSA-Signatur in das SSH-Blob-Format
// (mpint r, mpint s) um.
func derToSSHBlob(der []byte) ([]byte, error) {
	var sig struct{ R, S *big.Int }
	if rest, err := asn1.Unmarshal(der, &sig); err != nil || len(rest) != 0 {
		return nil, errors.New("fido2: ungültige ECDSA-Signatur vom Token")
	}
	return ssh.Marshal(struct{ R, S *big.Int }{sig.R, sig.S}), nil
}

// MarshalED25519PublicKey baut das SSH-Wire-Format eines
// sk-ssh-ed25519-Public-Keys (für Stubs aus Enrollment/MakeCredential).
func MarshalED25519PublicKey(pub []byte, application string) []byte {
	return ssh.Marshal(struct {
		Name        string
		Pub         []byte
		Application string
	}{AlgoED25519, pub, application})
}

// MarshalECDSAPublicKey baut das SSH-Wire-Format eines
// sk-ecdsa-sha2-nistp256-Public-Keys aus den Kurvenkoordinaten.
func MarshalECDSAPublicKey(x, y *big.Int, application string) []byte {
	point := elliptic.Marshal(elliptic.P256(), x, y)
	return ssh.Marshal(struct {
		Name        string
		Curve       string
		Point       []byte
		Application string
	}{AlgoECDSA, "nistp256", point, application})
}
