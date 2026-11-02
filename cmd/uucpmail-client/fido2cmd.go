package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"

	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/fido2"
	"github.com/cfluegel/uucpmail-client/pkg/keys"
	"github.com/cfluegel/uucpmail-client/pkg/sshtransport"
)

// terminalPrompter: PIN ohne Echo von stdin, Hinweise auf stderr.
type terminalPrompter struct{}

func (terminalPrompter) RequestPIN(context.Context) (string, error) {
	fmt.Fprint(os.Stderr, "FIDO2 PIN: ")
	pin, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(pin), nil
}
func (terminalPrompter) NotifyTouch() { fmt.Fprintln(os.Stderr, "Touch your security key …") }
func (terminalPrompter) TouchDone()   {}

// installSignerSource verdrahtet den FIDO2-Signer, wenn der Modus aktiv ist.
// Vom CLI-Dispatcher vor Engine-Kommandos aufgerufen.
func installSignerSource(cfg *config.Config) error {
	if cfg.SSHKeyMode != "fido2" {
		return nil
	}
	stubPath, err := config.FIDO2StubPath()
	if err != nil {
		return err
	}
	stub, err := fido2.LoadStub(stubPath)
	if err != nil {
		return fmt.Errorf("fido2 mode active but stub not loadable: %w", err)
	}
	signer, err := fido2.NewSigner(stub, terminalPrompter{})
	if err != nil {
		return err
	}
	sshtransport.SetSignerSource(func(*config.Config) (ssh.Signer, error) {
		return signer, nil
	})
	return nil
}

// cmdFIDO2: Self-Service für den Security-Key.
//
//	fido2 list          Resident-SSH-Schlüssel auf dem Token anzeigen
//	fido2 enroll        vorhandenen Schlüssel übernehmen (oder -new: erzeugen)
func cmdFIDO2(args []string) error {
	fs := flag.NewFlagSet("fido2", flag.ContinueOnError)
	createNew := fs.Bool("new", false, "create a new resident key on the token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sub := ""
	if fs.NArg() >= 1 {
		sub = fs.Arg(0)
		if err := fs.Parse(fs.Args()[1:]); err != nil {
			return err
		}
	}
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	backend, err := fido2.DefaultBackend()
	if err != nil {
		return err
	}
	ctx := context.Background()
	prompt := terminalPrompter{}

	switch sub {
	case "list":
		stubs, err := backend.EnumerateSSHCredentials(ctx, fido2.SSHApplication, prompt)
		if err != nil {
			return err
		}
		if len(stubs) == 0 {
			fmt.Println("No resident SSH keys on the token.")
			return nil
		}
		for i, st := range stubs {
			pub, err := st.SSHPublicKey()
			if err != nil {
				continue
			}
			fmt.Printf("%d) %s  %s  (%s)\n", i+1, st.Algorithm, keys.Fingerprint(pub), st.Comment)
		}
		return nil

	case "enroll":
		var stub *fido2.Stub
		if *createNew {
			stub, err = backend.MakeResidentCredential(ctx, fido2.SSHApplication, "uucpmail-"+cfg.NodeName, prompt)
			if err != nil {
				return err
			}
		} else {
			stubs, err := backend.EnumerateSSHCredentials(ctx, fido2.SSHApplication, prompt)
			if err != nil {
				return err
			}
			if len(stubs) == 0 {
				return errors.New(`no resident SSH key on the token – create one with "fido2 enroll -new"`)
			}
			stub = &stubs[0]
			if len(stubs) > 1 {
				fmt.Fprintf(os.Stderr, "note: %d keys found, adopting the first one (%s)\n",
					len(stubs), stub.Comment)
			}
		}
		stubPath, err := config.FIDO2StubPath()
		if err != nil {
			return err
		}
		if err := stub.Save(stubPath); err != nil {
			return err
		}
		cfg.SSHKeyMode = "fido2"
		if err := cfg.Save(); err != nil {
			return err
		}
		pub, err := stub.SSHPublicKey()
		if err != nil {
			return err
		}
		fmt.Println("FIDO2 key adopted and activated (ssh_key_mode=fido2).")
		fmt.Println("Register this line with the service as a node key:")
		fmt.Println(keys.AuthorizedLine(pub, cfg.NodeName))
		return nil

	default:
		return errors.New("usage: uucpmail-client fido2 list|enroll [-new]")
	}
}
