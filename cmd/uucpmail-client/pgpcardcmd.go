package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/pgpcard"
)

const pgpCardUsage = "usage: uucpmail-client pgp-card status | pgp-card reset <serial>"

// cmdPGPCard: Karten-Diagnose (englisch, skriptstabil) und Werksreset.
// Einrichtung läuft bewusst nur über den GUI-Wizard.
func cmdPGPCard(args []string) error {
	switch {
	case len(args) == 1 && args[0] == "status":
		return cmdPGPCardStatus()
	case len(args) == 2 && args[0] == "reset":
		return cmdPGPCardReset(args[1], os.Stdin)
	}
	return errors.New(pgpCardUsage)
}

func cmdPGPCardStatus() error {
	cards, err := pgpcard.DefaultBackend().Cards(context.Background())
	if err != nil {
		return err
	}
	for _, c := range cards {
		fmt.Printf("Card:         %s (%s)\n", c.Serial, c.Manufacturer)
		fmt.Printf("Reader:       %s\n", c.Reader)
		if c.Version != "" {
			fmt.Printf("Spec:         %s\n", c.Version)
		}
		if len(c.RSABits) > 0 {
			fmt.Printf("RSA sizes:    %v\n", c.RSABits)
		}
		if len(c.Algos) > 0 {
			fmt.Printf("Algorithms:   %s\n", strings.Join(c.Algos, ", "))
		}
		kdf := "unsupported"
		switch {
		case c.KDFActive:
			kdf = "on"
		case c.KDFSupported:
			kdf = "off"
		}
		sigPIN := "once per session"
		if c.SigPINForced {
			sigPIN = "every signature"
		}
		fmt.Printf("PIN retries:  user %d, admin %d\n", c.PINRetries, c.AdminRetries)
		fmt.Printf("Sig. PIN:     %s\n", sigPIN)
		fmt.Printf("KDF:          %s\n", kdf)
		fmt.Printf("Signatures:   %d\n", c.SigCount)
		for _, slot := range []pgpcard.Slot{pgpcard.SlotSign, pgpcard.SlotDecrypt, pgpcard.SlotAuth} {
			si := c.Slots[slot]
			name := strings.ToUpper(string(slot)[:3])
			if !si.Present {
				fmt.Printf("Slot %s:     empty\n", name)
				continue
			}
			fmt.Printf("Slot %s:     %s %s (%s)\n", name, si.Algo, si.Fingerprint,
				si.Created.Format(time.DateOnly))
		}
		fmt.Println()
	}
	if path, err := config.PGPCardStubPath(); err == nil {
		if stub, err := pgpcard.LoadStub(path); err == nil {
			fmt.Printf("Configured:   key %s on card %s (%s)\n", stub.KeyFingerprint, stub.Serial, stub.Algo)
		} else if !errors.Is(err, os.ErrNotExist) {
			fmt.Printf("Configured:   stub unreadable: %v\n", err)
		}
	}
	return nil
}

// cmdPGPCardReset setzt die Karte mit der angegebenen Seriennummer in den
// Werkszustand – nach Rückfrage (Seriennummer erneut eintippen).
func cmdPGPCardReset(serial string, in io.Reader) error {
	backend := pgpcard.DefaultBackend()
	sess, err := backend.Open(context.Background(), strings.ToUpper(serial))
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()
	info := sess.Info()
	if !info.CanReset {
		return pgpcard.ErrNotSupported
	}
	fmt.Printf("Factory reset of card %s (%s): ALL keys on the card are deleted,\n", info.Serial, info.Manufacturer)
	fmt.Println("PINs return to 123456 / 12345678, KDF off. This cannot be undone.")
	fmt.Print("Type the serial number again to confirm: ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	if !strings.EqualFold(strings.TrimSpace(line), info.Serial) {
		return errors.New("aborted: serial number does not match")
	}
	if err := sess.FactoryReset(); err != nil {
		return err
	}
	fmt.Printf("Card %s is back to factory state.\n", info.Serial)
	if path, err := config.PGPCardStubPath(); err == nil {
		if stub, err := pgpcard.LoadStub(path); err == nil && stub.Serial == info.Serial {
			fmt.Println("Note: the client was set up for this card; switch off card usage in Settings → PGP.")
		}
	}
	return nil
}
