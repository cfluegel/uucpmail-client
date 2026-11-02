// Package keys erzeugt, importiert und beschreibt den SSH-Schlüssel (ed25519),
// über den der Server die UUCP-Node-Identität zuordnet.
package keys

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"strings"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"golang.org/x/crypto/ssh"
)

// Generate erzeugt ein neues ed25519-Schlüsselpaar und schreibt es im
// OpenSSH-Format nach path (0600) bzw. path+".pub". Ein vorhandener Schlüssel
// wird nie überschrieben.
func Generate(path, comment string) error {
	if _, err := os.Stat(path); err == nil {
		return i18n.Errorf("err.keys.exists", path)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return err
	}
	return os.WriteFile(path+".pub", []byte(AuthorizedLine(sshPub, comment)+"\n"), 0o644) // #nosec G306 -- Public Key, bewusst welt-lesbar
}

// Import validiert eine vorhandene OpenSSH-Privatschlüsseldatei und kopiert sie
// (samt generierter .pub) nach dstPath.
func Import(srcPath, dstPath, comment string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		if _, ok := err.(*ssh.PassphraseMissingError); ok {
			return i18n.Errorf("err.keys.protected")
		}
		return i18n.Wrap(err, "err.keys.invalid")
	}
	if err := os.WriteFile(dstPath, data, 0o600); err != nil {
		return err
	}
	return os.WriteFile(dstPath+".pub", []byte(AuthorizedLine(signer.PublicKey(), comment)+"\n"), 0o644) // #nosec G306 -- Public Key, bewusst welt-lesbar
}

// Load liest den Privatschlüssel als ssh.Signer.
func Load(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, i18n.Errorf("err.keys.missing", path)
	}
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(data)
}

// AuthorizedLine liefert die authorized_keys-Zeile (für die Registrierungsmail).
func AuthorizedLine(pub ssh.PublicKey, comment string) string {
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if comment != "" {
		line += " " + comment
	}
	return line
}

// Fingerprint liefert den SHA256-Fingerprint des öffentlichen Schlüssels.
func Fingerprint(pub ssh.PublicKey) string {
	return ssh.FingerprintSHA256(pub)
}
