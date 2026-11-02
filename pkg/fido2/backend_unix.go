//go:build darwin || linux

package fido2

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/telesma-app/ctap/authenticator"
	directhid "github.com/telesma-app/ctap/backend/hid"
	"github.com/telesma-app/ctap/cose"
	"github.com/telesma-app/ctap/credential"
	"github.com/telesma-app/ctap/protocol"
	"github.com/telesma-app/ctap/transport"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"golang.org/x/crypto/ssh"
)

// platformBackend: direkter CTAP2-Zugriff über USB-HID (telesma-app/ctap,
// CGO-frei; macOS IOHIDManager, Linux hidraw). Funktioniert als normaler
// Nutzerprozess ohne Adminrechte.
func platformBackend() (Backend, error) {
	return &hidBackend{}, nil
}

type hidBackend struct{}

// openDevice wählt das Token (bei genau einem Gerät ohne Interaktion).
func openDevice(ctx context.Context) (*authenticator.Device, error) {
	dev, err := authenticator.Select(ctx, directhid.Enumerate)
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrNoDevice, err)
	}
	return dev, nil
}

// pinToken holt über den Prompter die PIN und tauscht sie gegen ein
// pinUvAuthToken mit der gewünschten Berechtigung.
func pinToken(ctx context.Context, dev *authenticator.Device, perm protocol.Permission,
	rpID string, prompt Prompter) ([]byte, error) {
	pin, err := prompt.RequestPIN(ctx)
	if err != nil {
		return nil, err
	}
	token, err := dev.GetPinUvAuthTokenUsingPIN(ctx, pin, perm, rpID)
	if err != nil {
		var ce *transport.CTAPError
		if errors.As(err, &ce) && (ce.StatusCode == transport.CTAP2_ERR_PIN_INVALID) {
			return nil, ErrPINWrong
		}
		return nil, err
	}
	return token, nil
}

func (b *hidBackend) GetAssertion(ctx context.Context, rpID string, clientData, credID []byte, prompt Prompter) (*Assertion, error) {
	dev, err := openDevice(ctx)
	if err != nil {
		return nil, err
	}
	defer dev.Close()

	token, err := pinToken(ctx, dev, protocol.PermissionGetAssertion, rpID, prompt)
	if err != nil {
		return nil, err
	}
	var allowList []credential.PublicKeyCredentialDescriptor
	if len(credID) > 0 {
		allowList = []credential.PublicKeyCredentialDescriptor{{
			Type: credential.PublicKeyCredentialTypePublicKey, ID: credID,
		}}
	}
	prompt.NotifyTouch()
	defer prompt.TouchDone()
	// Erste Assertion genügt; KEINE Extensions anfordern (authData muss
	// 37 Bytes bleiben, sonst scheitert die sshd-Verifikation).
	for resp, err := range dev.GetAssertion(ctx, token, rpID, clientData, allowList, nil, nil) {
		if err != nil {
			var ce *transport.CTAPError
			if errors.As(err, &ce) && ce.StatusCode == transport.CTAP2_ERR_NO_CREDENTIALS {
				return nil, ErrNoCredentials
			}
			return nil, err
		}
		return &Assertion{
			AuthData:     resp.AuthDataRaw,
			Signature:    resp.Signature,
			CredentialID: resp.Credential.ID,
		}, nil
	}
	return nil, ErrNoCredentials
}

func (b *hidBackend) MakeResidentCredential(ctx context.Context, rpID, userName string, prompt Prompter) (*Stub, error) {
	dev, err := openDevice(ctx)
	if err != nil {
		return nil, err
	}
	defer dev.Close()

	token, err := pinToken(ctx, dev, protocol.PermissionMakeCredential, rpID, prompt)
	if err != nil {
		return nil, err
	}
	// Zufällige, stabile User-ID; Name dient nur der Anzeige im Token.
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
	resp, err := dev.MakeCredential(ctx, token, clientData,
		credential.PublicKeyCredentialRpEntity{ID: rpID, Name: "uucpmail"},
		credential.PublicKeyCredentialUserEntity{ID: userID, Name: userName, DisplayName: userName},
		[]credential.PublicKeyCredentialParameters{
			// Präferenzreihenfolge: das Token nimmt den ersten
			// unterstützten Algorithmus.
			{Type: credential.PublicKeyCredentialTypePublicKey, Algorithm: cose.AlgorithmEdDSA},
			{Type: credential.PublicKeyCredentialTypePublicKey, Algorithm: cose.AlgorithmES256},
		},
		nil, nil,
		map[protocol.Option]bool{protocol.OptionResidentKeys: true},
		0, nil)
	if err != nil {
		return nil, err
	}
	ad := resp.AuthData
	if ad == nil {
		parsed, err := protocol.ParseMakeCredentialAuthData(resp.AuthDataRaw)
		if err != nil {
			return nil, err
		}
		ad = &parsed
	}
	if ad.AttestedCredentialData == nil {
		return nil, i18n.Errorf("err.fido2.nocreddata")
	}
	return stubFromCredential(ad.AttestedCredentialData.CredentialID,
		ad.AttestedCredentialData.CredentialPublicKey, rpID, userName)
}

func (b *hidBackend) EnumerateSSHCredentials(ctx context.Context, rpID string, prompt Prompter) ([]Stub, error) {
	dev, err := openDevice(ctx)
	if err != nil {
		return nil, err
	}
	defer dev.Close()

	token, err := pinToken(ctx, dev, protocol.PermissionCredentialManagement, "", prompt)
	if err != nil {
		return nil, err
	}
	rpHash := sha256.Sum256([]byte(rpID))
	var stubs []Stub
	for resp, err := range dev.EnumerateCredentials(ctx, token, rpHash[:]) {
		if err != nil {
			var ce *transport.CTAPError
			if errors.As(err, &ce) && ce.StatusCode == transport.CTAP2_ERR_NO_CREDENTIALS {
				return nil, nil
			}
			return nil, err
		}
		st, err := stubFromCredential(resp.CredentialID.ID, resp.PublicKey, rpID, resp.User.Name)
		if err != nil {
			continue // unbekannter Schlüsseltyp – überspringen
		}
		stubs = append(stubs, *st)
	}
	return stubs, nil
}

// stubFromCredential baut aus Credential-ID + COSE-Key einen Stub.
func stubFromCredential(credID []byte, key cose.Key, rpID, comment string) (*Stub, error) {
	pub, err := key.PublicKey()
	if err != nil {
		return nil, err
	}
	st := &Stub{
		Version: 1, Application: rpID, CredentialID: credID,
		Resident: true, Created: time.Now(), Comment: comment,
	}
	switch k := pub.(type) {
	case ed25519.PublicKey:
		st.Algorithm = AlgoED25519
		st.PublicKey = MarshalED25519PublicKey(k, rpID)
	case *ecdsa.PublicKey:
		st.Algorithm = AlgoECDSA
		st.PublicKey = MarshalECDSAPublicKey(k.X, k.Y, rpID)
	default:
		return nil, i18n.Errorf("err.fido2.badkeytype", fmt.Sprintf("%T", pub))
	}
	if _, err := ssh.ParsePublicKey(st.PublicKey); err != nil {
		return nil, err
	}
	return st, nil
}
