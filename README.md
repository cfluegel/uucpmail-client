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
  private key, export your public key, create a revocation certificate.
  Other people's public keys live in the Web of Trust window (see below).
  Stored as armored `pubring.asc`/`secring.asc` files (0600).
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
  ✓ valid / ✗ invalid / ? key unknown.
- **Web of Trust** (toolbar spider icon): import keys (file, paste,
  keyserver), certify after checking the fingerprint in person and send
  the certification back caff-style (one encrypted mail per identity),
  view certifications, key details and expiry, fingerprint strips as PDF.
  No trust computation – you decide based on certifications.
- **OpenPGP smartcard** (GnuPG/ZeitControl card, YubiKey via PC/SC): wizard
  puts the subkeys on the card (primary key stays in the keyring, mandatory
  backup); signing and decryption then ask for the card PIN. Card details,
  PIN unblock, factory reset; CLI `pgp-card status|reset`.

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

**Full documentation** (GUI walkthrough and every CLI command with flags
and examples): **[docs/MANUAL.md](docs/MANUAL.md)**. Besides `sync`, the
CLI offers `send` (mail from stdin), `queue`, `keygen [-import]`,
`pgp-keygen`, `ctl quota|usage`, `fido2 enroll` and `pgp-card status|reset`.

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

## FIDO2 security key for SSH

The SSH transport key can live on a FIDO2 security key (Yubikey, Nitrokey 3,
SoloKeys etc., FIDO2 PIN required) as a resident key – the private key never
leaves the token. Set it up via Settings → SSH → "Use FIDO2 SSH key"
(guided dialog: adopt an existing key or create a new one) or via CLI:

```
uucpmail-client fido2 enroll -new   # create a resident key
uucpmail-client fido2 list          # show keys on the token
```

Register the printed public key (`sk-ssh-ed25519@…`) as a node key with the
service. Every connection asks for the PIN (deliberately never cached) and
requires a token touch. The local stub (`fido2_key.json`) contains no key
material and travels in portable mode – token + PIN + node ID are all you
need on a foreign machine. For the OpenSSH CLI the key can be regenerated
anytime with `ssh-keygen -K`. The previous file key remains as a fallback
(uncheck the box). Windows: enrollment/signing go through the system UI
(WebAuthn); existing token keys cannot be listed there – enroll on
macOS/Linux and carry the portable stub. The Windows path is not yet
live-tested.

## Portable mode

If a folder named `uucpmail-data` sits next to the client binary, the
client stores all data there (configuration, keys, mail store, spool,
window state) – ideal for a USB stick; the guest machine stays clean.
Without that folder the user configuration directory is used as before.

Setup: create a `uucpmail-data` folder next to the binary (the first-run
wizard appears on next start) – or use Settings → Backup →
"Create portable data folder…", which copies your existing identity
(configuration, keys, address book – no mails) into a new data folder.

Notes: on a foreign machine the server host key must be confirmed once on
first sync (TOFU). On FAT-formatted sticks Unix file permissions have no
effect – protect the stick accordingly.

## Keyboard controls

The client is fully keyboard-driven. `Tab` toggles between the message list
and the mail text (the active area gets a colored outline); other elements are
deliberately not reachable via Tab.

| Key | Function |
|---|---|
| `S` (or `Cmd/Ctrl+S`) | Synchronise |
| `N` | New message |
| `R` / `Shift+R` | Reply / Reply all |
| `F` | Forward (multi-selection: as .eml attachments) |
| `U` | Mark unread (inbox only) |
| `Del`/`Backspace` | Delete (to trash; permanent inside trash) |
| `↑`/`↓` | List: move cursor · mail text: scroll line by line |
| `Shift+↑`/`↓` | List: extend selection · mail text: jump (configurable) |
| `Cmd/Ctrl+,` | Open settings |

In the Sent folder, "Edit as new" opens a sent mail as a new, fully
prefilled message (BCC is not part of the sent copy). Deleted mails move to the Trash folder (right of Sent) where they can be
restored. The trash is purged automatically at startup once messages are older
than the configured value (Settings → General, 0 = never).

## Limits of store-and-forward

UUCP is asynchronous: "Sent" means **handed over to the server**, not
delivered. Content checks (sender authorization, recipients, spam) happen
server-side after the transfer; rejected mail triggers a DSN to the node's
main address on a later poll. Transfer errors during the UUCP session are
visible immediately (the mail stays in the Outbox with an error marker).

## License

Apache License 2.0 – see [LICENSE](LICENSE).
Copyright © 2026 Christoph Flügel.
