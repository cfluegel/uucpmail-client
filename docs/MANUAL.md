# uucpmail client – Manual

User documentation for the graphical interface and the command line.
(Deutsche Fassung: [HANDBUCH.md](HANDBUCH.md))

The client is a store-and-forward mail client for uucpmail.de: mails are
collected in a local outbox and handed to the server via UUCP over SSH
during a synchronisation, which also fetches incoming mail. There is no
background polling – you trigger every sync yourself.

**Important:** "Sent" means *handed to the server*, not *delivered*. If
the service rejects a mail later (e.g. unregistered sender), a delivery
status notification (DSN) arrives at your main address on the next sync.

---

## Part 1: Graphical interface

### Main window

Toolbar on top (compose, sync, address book, Web of Trust – spider web
icon, enabled once you have an own private PGP key –, log window,
settings), below it the folder tabs:

- **Inbox** – received mail; the counter shows unread messages.
- **Drafts** – saved drafts (double-click or "Edit" reopens them;
  autosave interval is configurable).
- **Outbox** – waiting for the next synchronisation; the counter shows
  the count. Deleting also cancels the pending send job.
- **Sent** – mails handed to the server.
- **Trash** – deleted mails from all folders (see below).

Below: the message list (drag the column dividers to resize, click the
date header to flip the sort order) and the message view. The status bar
summarises the last synchronisation.

### Keyboard controls

`Tab` toggles between the message list and the mail text (the active area
gets a colored outline); other elements are deliberately not reachable
via Tab.

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

Delete confirmations are fully keyboard-driven: `Enter` confirms (focus
starts on "Yes"), arrow keys/`Tab` switch, `Esc` cancels.

### Message view and actions

Below the mail: Reply, Reply all, Forward, Delete – plus, depending on
context:

- **Decrypt** for PGP-encrypted mail (the passphrase is cached for the
  configured time; with a key on a smartcard the card PIN is asked
  instead).
- **Edit** (drafts only).
- **Restore** (trash only) – back to the original folder, applies to the
  whole selection.
- **Mark unread (u)** (inbox only).
- **Edit as new** (sent only) – opens the mail as a new, fully prefilled
  message (BCC is not part of the sent copy; decrypt encrypted sent
  copies first).
- **Source** – show the raw message.

Attachments appear as save buttons. If an attachment contains a public
PGP key there is an additional **"Import key"** button: it adds the key
to the keyring and offers to add the sender to the address book.

### Multi-selection

Shift+`↑`/`↓` selects a contiguous range. Delete (with confirmation),
Restore, Mark-unread and `F` (attaches all selected mails as `.eml`
files to a new message) operate on it. Reply is deliberately inactive for
multi-selections.

### Trash

Deleting moves mail to the trash without confirmation (recoverable);
inside the trash, Del deletes permanently, always with confirmation. At
startup, trash mails older than the configured value (Settings → General,
default 30 days, 0 = never) are purged automatically.

### New message

Fields: From (identity picker), To, CC, BCC (collapsed), Subject, text.
`Tab` jumps from the subject straight into the message text (the button
bar is skipped; `Shift+Tab` goes back). The address book button opens the
picker (To/CC/BCC assignment).

The bar above the text inserts Markdown snippets; after it – once you
have an own PGP key – the toggles **Sign**, **Encrypt** (requires public
keys for all recipients; the sent copy is encrypted to yourself too) and
**Attach key** (your public key as an `.asc` attachment). If the key is
on a smartcard, the client asks for the card PIN when sending; like the
passphrase it is cached for the configured time.

"Save" stores a draft (autosave runs additionally). If a signature is
enabled in Settings → General it is inserted with the `-- ` separator
(for replies/forwards only with the second option). A soft size warning
appears when the mail exceeds the configured limit.

### Address book

Reachable from the toolbar: contacts with first/last name, e-mail and an
optional PGP key – picked from the keyring (dropdown) or pasted as armor
text. A contact with a key is found automatically when encrypting
(matched by e-mail address).

### Settings (Cmd/Ctrl+,)

Changes apply on "Save"; "Cancel" discards them. (Exception: the key
actions in the SSH tab take effect immediately.)

- **Account:** node ID, server, port – from the registration reply.
  The node ID may only contain letters, digits and `._-`; whitespace
  (e.g. from copy & paste) is stripped on save, and invalid characters
  abort the sync with an error message.
- **Identities:** manage sender addresses, set the default (★).
- **SSH:** fingerprint and `authorized_keys` line of the transport key
  (copy button for registration), **"Generate new key …"** and **Import key…**
  (an existing OpenSSH key pair such as `id_ed25519` replaces the
  internal key – with a warning; register the new public key with the
  service afterwards; passphrase-protected files are rejected). Plus the
  **"Use FIDO2 SSH key"** checkbox: guided setup of a resident key on a
  security token (PIN + touch on every connection; the file key remains
  as fallback). If a token is already set up, "Set up again…" restarts the
  wizard (e.g. to move to a different token; the previous key stays on
  the old token and remains registered). Below: the server host keys
  from known_hosts.
- **PGP:** generate your key pair (validity selectable, multiple
  identities per key; a passphrase is required – if the two entries do
  not match, the dialog reopens with the passphrase fields cleared) or
  import a private key. Per own key: **Export** (public key to copy or
  save), **Revoke …** and the red trash can. "Revoke …" creates a
  revocation certificate like `gpg --gen-revoke`: choose a reason (no
  reason specified, compromised, superseded, no longer used), optional
  description, passphrase – the certificate is shown and can be copied or
  saved as `<fingerprint>.rev`. The key itself stays valid; it is only
  revoked once someone imports the certificate into GnuPG or publishes it
  on a keyserver. Keep it safe! Below: the **Smartcard** section (see
  below), passphrase cache duration and keyserver address (default
  `hkps://keyserver.ubuntu.com`). Other people's public keys are managed
  in the *Web of Trust* window.
- **Plan:** last queried service status (plan, quota/usage, queue,
  identities, retention) with query buttons – the reply arrives as mail:
  one sync submits the request, the next fetches the answer.
- **General:** language, theme, font size, draft autosave, max mail size
  (MB, soft warning, capped at 10), mails-per-hour warning threshold,
  trash retention, scroll jump, signature (own dialog), data directory
  display.
- **Backup:** create an encrypted identity backup (config, keys, address
  book – no mails) or restore it destructively; plus **"Create portable
  data folder…"** (see below).

### Portable mode

If a folder named `uucpmail-data` sits next to the binary, the client
stores everything there (config, keys, mails, window state) – ideal for a
USB stick; the guest machine stays clean. Setup: create the folder, or
use the button in Settings → Backup (copies your identity, no mails). On
a foreign machine the server host key must be confirmed once on the
first sync.

### FIDO2 security key

Details and background: README section "FIDO2 security key for SSH".
Short version: check the box in the SSH tab → PIN → adopt an existing
resident key or create a new one → register the shown line with the
service. Every connection asks for the PIN and requires a token touch;
while the token waits for the touch, a small "Touch your security key …"
popup indicates the waiting state and closes by itself.

### OpenPGP smartcard

Your PGP key can live on an OpenPGP smartcard (e.g. GnuPG/ZeitControl
card, spec 2.x–3.4, or YubiKey; card reader via PC/SC). The
**Smartcard** section in Settings → PGP:

- **Status line:** detected card with occupied slots (SIG/DEC/AUT);
  "Re-check" scans again. If another program (gpg/scdaemon) holds the
  card, a hint is shown – then e.g. `gpgconf --kill scdaemon`.
- **Use card for signing/decryption:** turns card mode on or off (takes
  effect immediately).
- **Set up card …** (wizard for an empty card): admin PIN → factory PINs
  must be replaced by your own (user PIN at least 6, admin PIN at least
  8 characters); on a fresh card that supports it, additionally
  **"Transmit PIN only hashed (KDF)"** (on by default) → name,
  identities, validity of the primary key, RSA size (2048, 3072 or 4096,
  depending on the card), passphrase (required) → **mandatory backup** of
  the primary key to a file, *before* anything is written to the card →
  the three subkeys (sign, encrypt, authenticate) go onto the card. The
  primary key stays in the keyring, protected by the passphrase. Without
  the backup, all encrypted mail is lost if the card is lost.
- **Use existing card …:** adopts a card set up elsewhere (e.g. with
  GnuPG); the matching public key must be in the keyring. RSA cards are
  fully usable; Ed25519 cards for signing only.
- **Card details …:** serial, manufacturer, reader, spec version,
  algorithms, slot usage, remaining PIN tries, signature PIN (every
  signature or once per session), KDF, signature counter – no PIN
  needed. Also there:
  - **Unblock PIN …** – sets a new user PIN with the admin PIN and resets
    the retry counter (after three wrong entries).
  - **Factory reset …** – deletes all keys on the card and resets the
    PINs (123456 / 12345678). You must type the serial number to confirm,
    followed by a final question. If the client was set up for this
    card, card mode is turned off.
- **Extend subkeys …:** the card subkeys are valid for 1 year; the button
  extends them by one year (needs the primary key's passphrase, the card
  itself is unchanged). At startup the client warns when they expire in
  less than 30 days.

### Web of Trust

Window via the spider web icon in the toolbar.

- **My key:** **Signatures …** (who certified my key) and **Fingerprint
  strips (PDF) …** – an A4 page of strips to cut out (key ID and type,
  name and email, full fingerprint) for key signing parties; print at
  original size.
- **Other people's public keys:** per key
  - **Certify …** – after personally checking fingerprint and identity.
    The dialog shows fingerprint, algorithms, creation and expiry date
    (expired keys with a yellow warning triangle). Choose the identities
    and your own key, then (mandatory choice): **"Sign and send to the
    owner"** (preselected) or **"Just sign"**. Sending means: one mail
    per identity, signed and encrypted to the owner's key, containing
    exactly this certification, to exactly this address (after a
    confirmation listing the addresses; the mails then wait in the
    outbox).
  - **Send …** – sends an existing certification of yours (later or
    again). Only enabled if the key carries a valid signature from you;
    you choose among the signed identities, the primary one is
    preselected.
  - **Signatures …** – certifications by others, collapsed per identity;
    right-clicking an unknown signer fetches their key from the
    keyserver.
  - **?** – key information: primary key and subkeys with algorithm,
    usage (S = sign, C = certify, E = encrypt, A = authenticate),
    fingerprint, creation and expiry date, status (valid / expired /
    revoked).
  - red trash can – delete the key.
  "✓ verified" after the name means: you have validly certified the key.
- Below: **Import public key …** (paste text or file; new certifications
  contained are merged) and **Download from keyserver …** (long key ID or
  fingerprint).

If a certification cannot be sent back because the key has no valid
encryption subkey, it stays saved. Typical for very old keys (e.g. GnuPG
1.4, DSA/ElGamal) whose subkeys carry no usage flags – the info dialog
then shows "not stated in the key". The client deliberately does not
support such keys for encryption.

### Use cases: which key for what?

A PGP key consists of the **primary key** and **subkeys**. With the card
setup only the subkeys are on the card; the primary key stays in the
keyring (and the backup), protected by the passphrase.

| Action | key used | prompt |
|---|---|---|
| Sign mail | signing subkey (exactly one signature) | card PIN or passphrase |
| Decrypt mail | encryption subkey | card PIN or passphrase |
| Certify someone else's key | primary key | passphrase |
| Create revocation certificate | primary key | passphrase |
| Extend card subkeys | primary key | passphrase |

On the road with only the card (primary key not on the machine) you can
sign and decrypt mail but **cannot certify other keys** – in OpenPGP only
the primary key may do that. So certify at your own machine (or with the
backup); "Just sign" plus a later **Send …** helps to decouple the two.

### Example: secure on the road with other people's computers

What the FIDO2 key, smartcard and portable mode are meant for together:
mobile, confidential mail on a computer that is not yours (hotel,
library, a friend's machine) – without installing anything there and
without your private keys ever leaving the token.

**You need:** a USB stick with the program and the `uucpmail-data` folder
(portable mode), a FIDO2 security key for the server connection and an
OpenPGP card plus reader for signing and decryption. A YubiKey 5 does
both at once (FIDO2 and OpenPGP). In your head: FIDO2 PIN, card PIN,
node ID.

**Set up once at home:**

1. Settings → Backup → **"Create portable data folder…"** on the stick.
2. Start the client from the stick; Settings → SSH → **"Use FIDO2 SSH
   key"** and register the shown key with the service.
3. Settings → PGP → **Set up card …** (with KDF). Keep the mandatory backup
   of the primary key in a safe place *at home*, not on the stick.
4. Import and certify the keys of others you will need on the road
   beforehand – certifying requires the primary key (see use cases).

**On the road:**

1. Plug in stick, security key and card reader, start the program from
   the stick. On the first sync on a new machine, confirm the server host
   key once.
2. **Sync:** enter the FIDO2 PIN and touch the key – without the key
   nobody gets into your mailbox.
3. **Read:** open encrypted mail with the card PIN; the card does the
   decryption, the plaintext is not written to disk.
4. **Write:** sign and encrypt as usual, card PIN when sending.
5. Afterwards quit the program and unplug everything. Nothing remains on
   the host – all data lives on the stick.

**What this protects – and what it does not:**

- The private keys cannot be copied by the host; without token and PINs,
  the stick contents and intercepted data are useless for connecting and
  decrypting.
- A compromised host can still see what is on screen or typed *during*
  use (mail text, PINs). It can misuse the token for the duration of the
  session, but cannot keep the keys. Use only trusted machines for really
  sensitive matters.
- The stick still holds the file SSH key (fallback) and the primary key –
  both are only as safe as the stick and the passphrase. If only the
  token key is registered with the service, the file SSH key is useless
  to a finder; for the primary key a strong passphrase is a must.
- Certifying other keys on the road only works if you carry the primary
  key and its passphrase – otherwise use "Just sign" and do **Send …** at
  home.
- Requirements on the host: USB access and a card reader the system
  recognises without installing drivers (common for CCID readers and
  YubiKeys). On Windows, FIDO2 runs through the system UI; this branch has
  not been tested live yet.

---

## Part 2: Command line

All commands use the same data directory as the GUI (portable mode is
detected automatically). Running without arguments starts the GUI. The
command line always prints English (script-stable); the language setting
only affects the GUI.

### keygen – create or import the SSH key

```
uucpmail-client keygen [-comment TEXT] [-import PATH]
```

| Flag | Meaning |
|---|---|
| `-comment` | comment for the public key (default `uucpmail`) |
| `-import` | import an existing OpenSSH private key instead of generating |

Prints path, fingerprint and the `authorized_keys` line for
registration. Without `-import` a new ed25519 key is generated (refuses
if one exists); with `-import` the internal key is replaced.

```
uucpmail-client keygen -import ~/.ssh/id_ed25519
```

### pgp-keygen – generate a PGP key pair

```
uucpmail-client pgp-keygen -name NAME -email ADDRESS[,ADDRESS…] -pass P [-years N]
```

| Flag | Meaning |
|---|---|
| `-name` | name for the key UIDs |
| `-email` | e-mail address(es), comma-separated – one key can carry several identities |
| `-pass` | passphrase (required) |
| `-years` | validity in years, 0 = never expires (default 2, max 100) |

```
uucpmail-client pgp-keygen -name "Christoph" -email christoph@uucpmail.de,postmaster@uucpmail.de -pass 'secret' -years 2
```

### send – queue a mail from stdin

```
uucpmail-client send [flags] RECIPIENT…
```

| Flag | Meaning |
|---|---|
| `-from` | sender identity (default: default_identity) |
| `-subject` | subject |
| `-cc` / `-bcc` | additional recipients, comma-separated (BCC envelope-only) |
| `-attach` | attach a file (path) |
| `-sign` / `-encrypt` | PGP sign / encrypt |
| `-pgp-pass` | passphrase of your PGP key (scripts only) |
| `-sync` | run a UUCP session right after queueing |

```
echo "Hello!" | uucpmail-client send -subject "Hi" -sync someone@example.org
```

### sync – run a UUCP session

```
uucpmail-client sync
```

Sends everything in the outbox and fetches waiting mail. FIDO2 mode:
PIN prompt on the terminal, touch hint on stderr. If the server stays
silent for 2 minutes, the session aborts with an error message (this
also applies to syncs started from the GUI).

### queue – outbox/spool status

```
uucpmail-client queue
```

Shows pending send jobs (including control jobs such as `[uuctl]`).

### ctl – query the service status

```
uucpmail-client ctl quota|usage [-sync]
```

Queues a self-service query (`quota` = plan & limits, `usage` = current
usage). The reply arrives as mail at your main address: one sync submits
the request, the next fetches the reply (it also updates the Plan tab).
`-sync` runs the first session immediately.

### fido2 – set up a security key

```
uucpmail-client fido2 list
uucpmail-client fido2 enroll [-new]
```

`list` shows the resident SSH keys on the token (PIN required;
macOS/Linux). `enroll` adopts the first existing key or creates a new one
with `-new`, stores the local stub, activates FIDO2 mode and prints the
registration line.

### pgp-card – OpenPGP smartcard

```
uucpmail-client pgp-card status
uucpmail-client pgp-card reset SERIAL
```

`status` shows all detected cards without a PIN: serial, manufacturer,
reader, spec version, algorithms, slot usage, PIN tries, signature PIN
mode, KDF and signature counter. `reset` returns the card to factory
state (all keys gone, PINs 123456 / 12345678) and asks for the serial
number again to confirm; if it does not match, the command aborts. If
gpg/scdaemon holds the card, run `gpgconf --kill scdaemon` first.

### Error behaviour

Every command exits non-zero on failure with a message on stderr
(`error: …`) – suitable for scripting.
