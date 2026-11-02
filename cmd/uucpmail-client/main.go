// uucpmail-client – Mail-Client für uucpmail.de (UUCP-over-SSH).
// Ohne Argumente startet die GUI; daneben gibt es Debug-/Automations-Subcommands.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		runGUI()
		return
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = cmdKeygen(os.Args[2:])
	case "pgp-keygen":
		err = cmdPGPKeygen(os.Args[2:])
	case "send":
		err = cmdSend(os.Args[2:])
	case "sync":
		err = cmdSync(os.Args[2:])
	case "queue":
		err = cmdQueue(os.Args[2:])
	case "ctl":
		err = cmdCtl(os.Args[2:])
	case "fido2":
		err = cmdFIDO2(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`uucpmail-client – mail client for uucpmail.de

Usage:
  uucpmail-client                 start the GUI
  uucpmail-client keygen          generate an SSH key and print the pubkey
  uucpmail-client pgp-keygen      generate a PGP key pair (-name, -email, -pass)
  uucpmail-client send <rcpt...>  read a mail from stdin into the outbox
  uucpmail-client sync            run a UUCP session (send + fetch)
  uucpmail-client queue           show outbox/spool status
  uucpmail-client ctl quota|usage query service status (reply arrives by mail)
  uucpmail-client fido2 list|enroll [-new]  set up a FIDO2 security key

Portable mode: if a folder "uucpmail-data" sits next to the binary,
all data is stored there (e.g. on a USB stick).
`)
}
