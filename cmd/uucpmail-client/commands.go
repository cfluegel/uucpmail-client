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
	comment := fs.String("comment", "uucpmail", "comment for the public key")
	importPath := fs.String("import", "", "import an existing OpenSSH private key instead of generating one")
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
	fmt.Println("key saved at:", keyPath)
	fmt.Println("Fingerprint:", keys.Fingerprint(signer.PublicKey()))
	fmt.Println()
	fmt.Println("public key (send to beta@uucpmail.de for registration):")
	fmt.Println(keys.AuthorizedLine(signer.PublicKey(), *comment))
	return nil
}

func requireConfig() (*config.Config, error) {
	cfg, err := loadConfigChecked()
	if err != nil {
		return nil, err
	}
	if err := installSignerSource(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadConfigChecked() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if !cfg.Complete() {
		p, _ := config.Path()
		return nil, fmt.Errorf("configuration incomplete (node_name/server missing) – edit %s or use the GUI setup", p)
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
	from := fs.String("from", "", "sender identity (default: default_identity)")
	subject := fs.String("subject", "", "Betreff")
	cc := fs.String("cc", "", "CC recipients (comma-separated)")
	bcc := fs.String("bcc", "", "BCC recipients (comma-separated, envelope only)")
	attach := fs.String("attach", "", "attach a file (path)")
	sign := fs.Bool("sign", false, "Nachricht mit PGP signieren")
	encrypt := fs.Bool("encrypt", false, "encrypt the message with PGP")
	pgpPass := fs.String("pgp-pass", "", "passphrase of the own PGP key (for scripts; the GUI asks interactively)")
	now := fs.Bool("sync", false, "nach dem Einreihen sofort eine UUCP-Session fahren")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rcpts := fs.Args()
	if len(rcpts) == 0 {
		return errors.New("usage: uucpmail-client send [-from addr] [-subject s] [-sync] <recipients...> < message.txt")
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
		return fmt.Errorf("sender %q is not a configured identity – the server would silently drop the mail (553 without bounce)", identity)
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
	fmt.Printf("queued in outbox: %s (%s)\n", meta.ID, meta.Subject)
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
			return nil, fmt.Errorf("no own PGP key for %s (uucpmail-client pgp-keygen)", identity)
		}
		if pgp.NeedsPassphrase(own) {
			unlocked, err := pgp.Unlock(own, pass)
			if err != nil {
				return nil, fmt.Errorf("unlocking PGP key: %w (pass -pgp-pass)", err)
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
	name := fs.String("name", "", "name for the key UIDs")
	email := fs.String("email", "", "e-mail address(es), comma-separated – one key can carry several identities")
	pass := fs.String("pass", "", "passphrase (required)")
	years := fs.Int("years", 2, "validity in years (0 = never expires)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	emails := splitList(*email)
	if len(emails) == 0 || *pass == "" {
		return errors.New("usage: uucpmail-client pgp-keygen -name NAME -email ADDRESS[,ADDRESS…] -pass P [-years N]")
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
	if *years < 0 || *years > 100 {
		return errors.New("-years muss zwischen 0 und 100 liegen")
	}
	lifetime := time.Duration(*years) * 365 * 24 * time.Hour
	info, err := keyring.Generate(*name, emails, *pass, lifetime)
	if err != nil {
		return err
	}
	fmt.Println("PGP key generated:", info.Fingerprint)
	fmt.Println("identities:", strings.Join(info.Emails, ", "))
	if !info.Expiry.IsZero() {
		fmt.Println("valid until:", info.Expiry.Format("2006-01-02"))
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

// cmdCtl reicht eine uuctl-Self-Service-Anfrage ein (quota|usage).
func cmdCtl(args []string) error {
	fs := flag.NewFlagSet("ctl", flag.ContinueOnError)
	now := fs.Bool("sync", false, "run a UUCP session right away")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	what := ""
	if len(rest) >= 1 {
		what = strings.ToLower(rest[0])
		// Flags dürfen auch nach dem Subkommando stehen (ctl quota -sync).
		if err := fs.Parse(rest[1:]); err != nil {
			return err
		}
	}
	if what != "quota" && what != "usage" || fs.NArg() > 0 {
		return errors.New("usage: uucpmail-client ctl quota|usage [-sync]")
	}
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	eng, closeFn, err := openEngine(cfg, stderrLog)
	if err != nil {
		return err
	}
	defer closeFn()
	if err := eng.EnqueueControl("GET " + what); err != nil {
		return err
	}
	fmt.Println("request queued – the reply arrives as mail to the main address (second sync needed).")
	if *now {
		return runSync(eng)
	}
	return nil
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
	fmt.Printf("sent: %d, rejected: %d, received: %d\n",
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
