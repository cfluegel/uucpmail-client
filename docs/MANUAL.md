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

Toolbar on top (compose, sync, address book, log window, settings), below
it the folder tabs:

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
  configured time).
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
**Attach key** (your public key as an `.asc` attachment).

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
- **Identities:** manage sender addresses, set the default (★).
- **SSH:** fingerprint and `authorized_keys` line of the transport key
  (copy button for registration), **"Generate new key …"** and **Import key…**
  (an existing OpenSSH key pair such as `id_ed25519` replaces the
  internal key – with a warning; register the new public key with the
  service afterwards; passphrase-protected files are rejected). Plus the
  **"Use FIDO2 SSH key"** checkbox: guided setup of a resident key on a
  security token (PIN + touch on every connection; the file key remains
  as fallback). Below: the server host keys from known_hosts.
- **PGP:** generate your key pair (validity selectable, multiple
  identities per key), import/export, manage foreign keys, passphrase
  cache duration.
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
service. Every connection asks for the PIN and requires a token touch.

---

## Part 2: Command line

All commands use the same data directory as the GUI (portable mode is
detected automatically). Running without arguments starts the GUI.

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
uucpmail-client pgp-keygen -name NAME -email ADDRESS[,ADDRESS…] [-pass P] [-years N]
```

| Flag | Meaning |
|---|---|
| `-name` | name for the key UIDs |
| `-email` | e-mail address(es), comma-separated – one key can carry several identities |
| `-pass` | passphrase (empty = unprotected) |
| `-years` | validity in years, 0 = never expires (default 2, max 100) |

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
PIN prompt on the terminal, touch hint on stderr.

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

### Error behaviour

Every command exits non-zero on failure with a message on stderr
(`Fehler: …`) – suitable for scripting.
