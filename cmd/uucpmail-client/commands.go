package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cfluegel/uucpmail-client/pkg/compose"
	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/keys"
	"github.com/cfluegel/uucpmail-client/pkg/pgp"
	"github.com/cfluegel/uucpmail-client/pkg/store"
	syncengine "github.com/cfluegel/uucpmail-client/pkg/sync"
	"github.com/cfluegel/uucpmail-client/pkg/uucp"
)

func cmdKeygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	comment := fs.String("comment", "uucpmail", "Kommentar für den öffentlichen Schlüssel")
	importPath := fs.String("import", "", "vorhandenen OpenSSH-Privatschlüssel importieren statt neu zu erzeugen")
	if err := fs.Parse(args); err != nil {
		return err
	}
	keyPath, err := config.KeyPath()
	if err != nil {
		return err
	}
	if *importPath != "" {
		if err := keys.Import(*importPath, keyPath, *comment); err != nil {
			return err
		}
	} else if err := keys.Generate(keyPath, *comment); err != nil {
		return err
	}
	signer, err := keys.Load(keyPath)
	if err != nil {
		return err
	}
	fmt.Println("Schlüssel gespeichert unter:", keyPath)
	fmt.Println("Fingerprint:", keys.Fingerprint(signer.PublicKey()))
	fmt.Println()
	fmt.Println("Öffentlicher Schlüssel (für die Registrierung an beta@uucpmail.de senden):")
	fmt.Println(keys.AuthorizedLine(signer.PublicKey(), *comment))
	return nil
}

func requireConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if !cfg.Complete() {
		p, _ := config.Path()
		return nil, fmt.Errorf("konfiguration unvollständig (node_name/server fehlen) – %s bearbeiten oder GUI-Einrichtung nutzen", p)
	}
	return cfg, nil
}

// openEngine baut die Engine mit Store + Spool; der Aufrufer schließt den Store.
func openEngine(cfg *config.Config, log uucp.Logf) (*syncengine.Engine, func(), error) {
	mailDir, err := config.MailDir()
	if err != nil {
		return nil, nil, err
	}
	st, err := store.Open(mailDir)
	if err != nil {
		return nil, nil, err
	}
	spoolDir, err := config.SpoolDir()
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	spool, err := uucp.OpenSpool(spoolDir)
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	return &syncengine.Engine{Cfg: cfg, Store: st, Spool: spool, Log: log},
		func() { st.Close() }, nil
}

func stderrLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// cmdSend liest eine Nachricht von stdin (erste Zeile "Subject: …" optional
// als Betreff, Rest Body) und stellt sie in die Outbox.
func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	from := fs.String("from", "", "Absender-Identität (Default: default_identity)")
	subject := fs.String("subject", "", "Betreff")
	cc := fs.String("cc", "", "CC-Empfänger (Komma-getrennt)")
	bcc := fs.String("bcc", "", "BCC-Empfänger (Komma-getrennt, nur Envelope)")
	attach := fs.String("attach", "", "Datei anhängen (Pfad)")
	sign := fs.Bool("sign", false, "Nachricht mit PGP signieren")
	encrypt := fs.Bool("encrypt", false, "Nachricht mit PGP verschlüsseln")
	pgpPass := fs.String("pgp-pass", "", "Passphrase des eigenen PGP-Schlüssels (nur für Skripte; GUI fragt interaktiv)")
	now := fs.Bool("sync", false, "nach dem Einreihen sofort eine UUCP-Session fahren")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rcpts := fs.Args()
	if len(rcpts) == 0 {
		return errors.New("aufruf: uucpmail-client send [-from adr] [-subject s] [-sync] <empfänger...> < nachricht.txt")
	}
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	identity, err := cfg.Identity(*from)
	if err != nil {
		return err
	}
	known := false
	for _, id := range cfg.Identities {
		if strings.EqualFold(strings.TrimSpace(id), strings.TrimSpace(identity)) {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("absender %q ist keine konfigurierte Identität – der Server würde die Mail kommentarlos verwerfen (553 ohne Bounce)", identity)
	}

	body, err := io.ReadAll(bufio.NewReader(os.Stdin))
	if err != nil {
		return err
	}
	subj := *subject
	text := string(body)
	if subj == "" {
		// Konvention: erste Zeile "Subject: …" wird als Betreff übernommen.
		if rest, ok := strings.CutPrefix(text, "Subject:"); ok {
			if idx := strings.IndexByte(rest, '\n'); idx >= 0 {
				subj = strings.TrimSpace(rest[:idx])
				text = strings.TrimPrefix(rest[idx+1:], "\n")
			}
		}
	}

	eng, closeFn, err := openEngine(cfg, stderrLog)
	if err != nil {
		return err
	}
	defer closeFn()

	msg := &compose.Message{
		Identity: identity,
		To:       rcpts,
		Cc:       splitList(*cc),
		Bcc:      splitList(*bcc),
		Subject:  subj,
		Body:     text,
		NodeName: cfg.NodeName,
		Sign:     *sign,
		Encrypt:  *encrypt,
	}
	if msg.Sign || msg.Encrypt {
		sealer, err := openSealer(identity, msg.Sign, *pgpPass)
		if err != nil {
			return err
		}
		eng.PGP = sealer
	}
	if *attach != "" {
		data, err := os.ReadFile(*attach)
		if err != nil {
			return err
		}
		name := filepath.Base(*attach)
		mt := mime.TypeByExtension(filepath.Ext(name))
		if mt == "" {
			mt = "application/octet-stream"
		}
		msg.Attachments = append(msg.Attachments, compose.Attachment{
			Filename: name, Data: data, MIMEType: mt,
		})
	}
	meta, err := eng.Enqueue(msg)
	if err != nil {
		return err
	}
	fmt.Printf("in Outbox eingereiht: %s (%s)\n", meta.ID, meta.Subject)
	if *now {
		return runSync(eng)
	}
	return nil
}

// openSealer lädt den Schlüsselbund und entsperrt bei Bedarf den eigenen
// Schlüssel (CLI-Pfad; die GUI nutzt den interaktiven Passphrase-Dialog).
func openSealer(identity string, needSigner bool, pass string) (*pgp.MailSealer, error) {
	pubPath, err := config.PubringPath()
	if err != nil {
		return nil, err
	}
	secPath, err := config.SecringPath()
	if err != nil {
		return nil, err
	}
	keyring, err := pgp.LoadKeyring(pubPath, secPath)
	if err != nil {
		return nil, err
	}
	cache := pgp.NewCache(time.Minute)
	if needSigner {
		own := keyring.FindPrivateByEmail(identity)
		if own == nil {
			return nil, fmt.Errorf("kein eigener PGP-Schlüssel für %s (uucpmail-client pgp-keygen)", identity)
		}
		if pgp.NeedsPassphrase(own) {
			unlocked, err := pgp.Unlock(own, pass)
			if err != nil {
				return nil, fmt.Errorf("PGP-Schlüssel entsperren: %w (-pgp-pass angeben)", err)
			}
			for _, info := range keyring.ListPrivate() {
				if strings.EqualFold(info.Email, identity) {
					cache.Put(info.Fingerprint, unlocked)
				}
			}
		}
	}
	return &pgp.MailSealer{Keyring: keyring, Cache: cache}, nil
}

// cmdPGPKeygen erzeugt ein PGP-Schlüsselpaar im Client-Schlüsselbund.
func cmdPGPKeygen(args []string) error {
	fs := flag.NewFlagSet("pgp-keygen", flag.ContinueOnError)
	name := fs.String("name", "", "Name für die Schlüssel-UIDs")
	email := fs.String("email", "", "E-Mail-Adresse(n), Komma-getrennt – ein Schlüssel kann mehrere Identitäten tragen")
	pass := fs.String("pass", "", "Passphrase (leer = ungeschützt)")
	years := fs.Int("years", 2, "Gültigkeit in Jahren (0 = läuft nicht ab)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	emails := splitList(*email)
	if len(emails) == 0 {
		return errors.New("aufruf: uucpmail-client pgp-keygen -name NAME -email ADRESSE[,ADRESSE…] [-pass P] [-years N]")
	}
	pubPath, err := config.PubringPath()
	if err != nil {
		return err
	}
	secPath, err := config.SecringPath()
	if err != nil {
		return err
	}
	keyring, err := pgp.LoadKeyring(pubPath, secPath)
	if err != nil {
		return err
	}
	lifetime := time.Duration(*years) * 365 * 24 * time.Hour
	info, err := keyring.Generate(*name, emails, *pass, lifetime)
	if err != nil {
		return err
	}
	fmt.Println("PGP-Schlüssel erzeugt:", info.Fingerprint)
	fmt.Println("Identitäten:", strings.Join(info.Emails, ", "))
	if !info.Expiry.IsZero() {
		fmt.Println("Gültig bis:", info.Expiry.Format("02.01.2006"))
	}
	armored, err := keyring.ExportPublic(info.Fingerprint)
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Print(string(armored))
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func cmdSync(args []string) error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	eng, closeFn, err := openEngine(cfg, stderrLog)
	if err != nil {
		return err
	}
	defer closeFn()
	return runSync(eng)
}

func runSync(eng *syncengine.Engine) error {
	res, err := eng.Run()
	fmt.Printf("gesendet: %d, abgelehnt: %d, empfangen: %d\n",
		res.SentCount, res.FailedCount, res.ReceivedCount)
	return err
}

func cmdQueue(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	eng, closeFn, err := openEngine(cfg, stderrLog)
	if err != nil {
		return err
	}
	defer closeFn()
	out, err := eng.QueueStatus()
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}
