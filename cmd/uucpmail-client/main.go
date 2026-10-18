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
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unbekanntes Kommando %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`uucpmail-client – Mail-Client für uucpmail.de

Aufruf:
  uucpmail-client                 GUI starten
  uucpmail-client keygen          SSH-Schlüssel erzeugen und Pubkey anzeigen
  uucpmail-client pgp-keygen      PGP-Schlüsselpaar erzeugen (-name, -email, -pass)
  uucpmail-client send <rcpt...>  Mail von stdin lesen und in die Outbox stellen
  uucpmail-client sync            UUCP-Session fahren (senden + abholen)
  uucpmail-client queue           Outbox/Spool-Status anzeigen
  uucpmail-client ctl quota|usage Dienststatus abfragen (Antwort per Mail)
`)
}
