# uucpmail-client

*English version – [deutsche Fassung](README.de.md)*

A native multi-platform mail client (macOS, Windows, Linux) for
[uucpmail.de](https://uucpmail.de) – a UUCP-over-SSH mail service. Ships as a
single static binary with no runtime dependencies (no uucico, no OpenSSH
client, no GnuPG installation required).

- **Winlink-style layout:** folder tabs (Inbox / Drafts / Outbox / Sent),
  message list on top, message content below.
- **Markdown as formatting:** no renderer in V1 – the compose toolbar inserts
  Markdown syntax (`#`, `**…**`, lists, …) as plain text.
- **Own Taylor UUCP implementation** (handshake + protocol `t`) over
  `golang.org/x/crypto/ssh` – verified against the Taylor UUCP 1.07 sources
  and against a live server; details in [docs/PROTOCOL.md](docs/PROTOCOL.md).
- **One account, multiple identities** (sender addresses); SSH key management
  with first-run setup and TOFU host key pinning (host key changes require
  explicit confirmation).
- **Mail features:** CC/BCC (BCC envelope-only), reply / reply-all / forward
  with classic `> ` quoting and proper threading headers, attachments in both
  directions (multipart/mixed), MIME receiving with text/plain preference and
  a safe, rudimentary built-in HTML renderer (own sanitizer on top of Fyne
  RichText – no CSS, no JS, no webview, no remote content), keyboard
  shortcuts N / R / Shift+R / F / Del / Cmd+S.

## OpenPGP

OpenPGP is built in natively (pure Go via ProtonMail/go-crypto – no GnuPG
required, identical on all platforms):

- **Keys**: Settings → PGP. Generate a key pair interactively
  (Ed25519/X25519, v4 – compatible with GnuPG ≥ 2.1, multiple identities
  (UIDs) per key, configurable validity, default 2 years), import an existing
  private key, export your public key, import/delete other people's public
  keys. Stored as armored `pubring.asc`/`secring.asc` files (0600).
- **Sending**: "Sign" and "Encrypt" toggle buttons in the compose window
  (separately or combined). Format: PGP/MIME (RFC 3156) with text-mode
  signatures (gateway-safe); signed+encrypted as combined one-pass message.
  Encrypting requires public keys for ALL recipients; your own copy is always
  encrypted to yourself (Sent stays readable). CLI: `send -sign -encrypt`,
  `pgp-keygen`.
- **Receiving**: multipart/encrypted, multipart/signed, inline PGP and
  clearsign. Opening an encrypted mail asks (yes/no), then for the
  passphrase; the unlocked key stays in memory for a configurable time
  (default 15 min) – further encrypted mails open without prompting.
  Decrypted content is never written to disk. Signature status in the view:
  ✓ valid / ✗ invalid / ? key unknown. No trust/WoT model in V1 – keys are
  verified manually via fingerprint.

Interoperability is verified end-to-end against GnuPG 2.5 in both directions.

## Address book

A minimal address book (toolbar button): first name, last name, e-mail and
optionally a GnuPG public key per contact. In the compose window the address
book button (next to the From dropdown) opens an Outlook-style picker – select
a row and add it via To/CC/BCC (multi-add, automatic separators, duplicate
protection). Pasted public keys go into the central keyring; a contact with a
key encrypts automatically.

## Self-service / service status

The settings dialog has a "Plan" tab showing the last known service status
(plan name, mailbox quota and usage, queue utilisation, identities,
retention of unfetched mail). Two buttons queue read-only `uuctl` queries
(`GET quota`, `GET usage`) as regular UUCP jobs: one sync submits the
request, the next one fetches the reply, which arrives as a normal mail to
the node's main address and is parsed automatically into the persisted
status. The same works from the command line:

```
uucpmail-client ctl quota -sync   # query plan & limits
uucpmail-client ctl usage -sync   # query current usage
```

## Encrypted identity backup

Settings → Backup creates an encrypted backup of the client identity
(settings, SSH key, server host key, PGP keyring, contacts – NOT the mail
store) as a passphrase-protected file (`gpg -c` compatible). Importing on
another client deliberately overwrites the local identity after explicit
warnings.

## Build

```sh
make build        # → bin/uucpmail-client (requires Go + a C toolchain, Fyne/CGO)
make test         # unit and fake-server tests (no Docker, no network)
```

Or directly from the module proxy:

```sh
go install github.com/cfluegel/uucpmail-client/cmd/uucpmail-client@latest
```

Cross builds: GitHub Actions matrix (`.github/workflows/build.yml`) or
locally via `make cross` (fyne-cross, Docker; macOS builds natively/CI only).

## Usage

```sh
uucpmail-client            # GUI (first run: generate key, register the
                           # public key with the operator, enter node/address)
uucpmail-client keygen     # generate the SSH key, print the public key
uucpmail-client pgp-keygen # generate a PGP key pair
uucpmail-client send -subject "Hi" someone@example.com < text.md
uucpmail-client sync       # UUCP session: send + fetch (Winlink style)
uucpmail-client queue      # show pending jobs
```

Configuration and data live in the OS config directory
(`~/Library/Application Support/uucpmail`, `%AppData%\uucpmail`,
`~/.config/uucpmail`): `config.json`, keys, `known_hosts` (TOFU), the mail
store (one `.eml` per message + a rebuildable index) and the UUCP spool.

## Limits of store-and-forward

UUCP is asynchronous: "Sent" means **handed over to the server**, not
delivered. Content checks (sender authorization, recipients, spam) happen
server-side after the transfer; rejected mail triggers a DSN to the node's
main address on a later poll. Transfer errors during the UUCP session are
visible immediately (the mail stays in the Outbox with an error marker).

## License

Apache License 2.0 – see [LICENSE](LICENSE).
Copyright © 2026 Christoph Flügel.
