//go:build windows

package fido2

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/go-ctap/ctaphid/pkg/webauthntypes"
	"github.com/go-ctap/winhello"
	"github.com/ldclabs/cose/iana"
	"github.com/ldclabs/cose/key"
	"golang.org/x/sys/windows"
)

// platformBackend: Windows-WebAuthn-API (webauthn.dll) – der einzige Weg
// ohne Adminrechte (raw-HID auf FIDO-Geräte ist seit Win10 1903 gesperrt).
// PIN-/Touch-Dialoge zeigt die System-UI selbst; unser Prompter wird nur für
// den Berührungs-Hinweis genutzt. Resident-Key-Enumeration (credMgmt) ist
// ohne Elevation nicht möglich → ErrNotSupported; die Auswahl übernimmt die
// System-UI über eine leere AllowList.
//
// Wichtig: GetAssertion erhält die ROHEN Client-Daten – webauthn.dll hasht
// sie selbst (SHA-256), was exakt dem clientDataHash entspricht, den die
// sk-Signatur braucht.
func platformBackend() (Backend, error) {
	return &winBackend{}, nil
}

type winBackend struct{}

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
)

func foregroundWindow() windows.HWND {
	h, _, _ := procGetForegroundWindow.Call()
	return windows.HWND(h)
}

func (b *winBackend) GetAssertion(ctx context.Context, rpID string, clientData, credID []byte, prompt Prompter) (*Assertion, error) {
	var allowList []webauthntypes.PublicKeyCredentialDescriptor
	if len(credID) > 0 {
		allowList = []webauthntypes.PublicKeyCredentialDescriptor{{
			Type: webauthntypes.PublicKeyCredentialTypePublicKey, ID: credID,
		}}
	}
	prompt.NotifyTouch()
	defer prompt.TouchDone()
	resp, err := winhello.GetAssertion(foregroundWindow(), rpID, clientData, allowList, nil,
		&winhello.AuthenticatorGetAssertionOptions{Timeout: 2 * time.Minute})
	if err != nil {
		return nil, fmt.Errorf("webauthn: %w", err)
	}
	return &Assertion{
		AuthData:     resp.AuthDataRaw,
		Signature:    resp.Signature,
		CredentialID: resp.Credential.ID,
	}, nil
}

func (b *winBackend) MakeResidentCredential(ctx context.Context, rpID, userName string, prompt Prompter) (*Stub, error) {
	userID := make([]byte, 16)
	if _, err := rand.Read(userID); err != nil {
		return nil, err
	}
	clientData := make([]byte, 32)
	if _, err := rand.Read(clientData); err != nil {
		return nil, err
	}
	prompt.NotifyTouch()
	defer prompt.TouchDone()
	resp, err := winhello.MakeCredential(foregroundWindow(), clientData,
		webauthntypes.PublicKeyCredentialRpEntity{ID: rpID, Name: "uucpmail"},
		webauthntypes.PublicKeyCredentialUserEntity{ID: userID, Name: userName, DisplayName: userName},
		[]webauthntypes.PublicKeyCredentialParameters{
			{Type: webauthntypes.PublicKeyCredentialTypePublicKey, Algorithm: iana.AlgorithmEdDSA},
			{Type: webauthntypes.PublicKeyCredentialTypePublicKey, Algorithm: iana.AlgorithmES256},
		},
		nil, nil,
		&winhello.AuthenticatorMakeCredentialOptions{
			Timeout:            2 * time.Minute,
			RequireResidentKey: true,
		})
	if err != nil {
		return nil, fmt.Errorf("webauthn: %w", err)
	}
	ad := resp.AuthData
	if ad == nil || ad.AttestedCredentialData == nil {
		return nil, errors.New("fido2: Windows lieferte keine Credential-Daten")
	}
	return winStubFromCOSE(resp.CredentialID, ad.AttestedCredentialData.CredentialPublicKey, rpID, userName)
}

func (b *winBackend) EnumerateSSHCredentials(ctx context.Context, rpID string, prompt Prompter) ([]Stub, error) {
	return nil, ErrNotSupported
}

// winStubFromCOSE extrahiert den Public Key aus dem ldclabs-COSE-Key.
func winStubFromCOSE(credID []byte, k key.Key, rpID, comment string) (*Stub, error) {
	st := &Stub{
		Version: 1, Application: rpID, CredentialID: credID,
		Resident: true, Created: time.Now(), Comment: comment,
	}
	switch k.Kty() {
	case iana.KeyTypeOKP:
		x, err := k.GetBytes(iana.OKPKeyParameterX)
		if err != nil {
			return nil, err
		}
		st.Algorithm = AlgoED25519
		st.PublicKey = MarshalED25519PublicKey(x, rpID)
	case iana.KeyTypeEC2:
		xb, err := k.GetBytes(iana.EC2KeyParameterX)
		if err != nil {
			return nil, err
		}
		yb, err := k.GetBytes(iana.EC2KeyParameterY)
		if err != nil {
			return nil, err
		}
		st.Algorithm = AlgoECDSA
		st.PublicKey = MarshalECDSAPublicKey(new(big.Int).SetBytes(xb), new(big.Int).SetBytes(yb), rpID)
	default:
		return nil, fmt.Errorf("fido2: nicht unterstützter COSE-Schlüsseltyp %d", k.Kty())
	}
	return st, nil
}
