// Package sshtransport stellt den rohen Byte-Kanal zum uucpmail-Server her:
// Äquivalent von `ssh -T uucp@host` – kein PTY, Forced Command serverseitig,
// stdin/stdout der Session sind der uucico-Datenstrom.
package sshtransport

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/keys"
)

// Conn ist der offene UUCP-Kanal über SSH.
type Conn struct {
	io.Reader
	io.Writer
	session *ssh.Session
	client  *ssh.Client
}

func (c *Conn) Close() error {
	if c.session != nil {
		c.session.Close()
	}
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// Dial baut die SSH-Verbindung auf (User "uucp", Pubkey-Auth, TOFU-Hostkey)
// und startet eine Shell-Session ohne PTY. Rückgabe ist der rohe Kanal.
func Dial(cfg *config.Config) (*Conn, error) {
	keyPath, err := config.KeyPath()
	if err != nil {
		return nil, err
	}
	signer, err := keys.Load(keyPath)
	if err != nil {
		return nil, err
	}
	khPath, err := config.KnownHostsPath()
	if err != nil {
		return nil, err
	}
	hostKeyCB, err := tofuHostKeyCallback(khPath)
	if err != nil {
		return nil, err
	}
	// x/crypto wrappt Callback-Fehler nicht zuverlässig mit %w – den
	// Hostkey-Wechsel deshalb selbst festhalten, damit die UI ihn erkennt.
	var hostKeyChanged *HostKeyChangedError
	checkingCB := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := hostKeyCB(hostname, remote, key)
		var hk *HostKeyChangedError
		if errors.As(err, &hk) {
			hostKeyChanged = hk
		}
		return err
	}

	addr := net.JoinHostPort(cfg.ServerHost, fmt.Sprintf("%d", cfg.ServerPort))
	clientCfg := &ssh.ClientConfig{
		User:            "uucp",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: checkingCB,
		Timeout:         30 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, clientCfg)
	if err != nil {
		if hostKeyChanged != nil {
			return nil, fmt.Errorf("SSH-Verbindung zu %s fehlgeschlagen: %w", addr, hostKeyChanged)
		}
		return nil, fmt.Errorf("SSH-Verbindung zu %s fehlgeschlagen: %w", addr, err)
	}
	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	// Kein RequestPty: entspricht `ssh -T`. Shell startet den Forced Command.
	if err := session.Shell(); err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	return &Conn{Reader: stdout, Writer: stdin, session: session, client: client}, nil
}
