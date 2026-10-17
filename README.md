# uucpmail-client

Nativer Multiplattform-Mail-Client (macOS, Windows, Linux) für den Dienst
[uucpmail.de](https://uucpmail.de) – UUCP-over-SSH, eine einzelne Binärdatei,
keine weitere Software nötig (kein uucico, kein OpenSSH-Client).

- **Winlink-artiges Layout:** Ordner (Posteingang/Postausgang/Gesendet) links,
  Nachrichtenliste oben rechts, Inhalt darunter.
- **Markdown als Formatierung:** V1 ohne Renderer – die Toolbar im
  Compose-Fenster fügt die Syntax (`#`, `**…**`, Listen, …) als Text ein.
- **Eigene Taylor-UUCP-Implementierung** (Handshake + Protokoll `t`) über
  `golang.org/x/crypto/ssh`; Details in [docs/PROTOCOL.md](docs/PROTOCOL.md).
- **Ein Account, mehrere Identitäten** (Absenderadressen); SSH-Key-Verwaltung
  mit Erststart-Einrichtung.
- **Mail-Funktionen (V1.1):** CC/BCC (BCC nur im Envelope), Antworten/Allen
  antworten/Weiterleiten mit klassischem `> `-Zitat und Threading-Headern,
  Anhänge senden und empfangen (multipart/mixed), MIME-Empfang mit
  text/plain-Präferenz und sicherem, rudimentärem HTML-Rendering (eigener
  Sanitizer auf RichText-Basis – kein CSS/JS/Webview/Remote-Inhalte),
  Shortcuts N/R/Shift+R/F/Entf/Cmd+S.

## Build

```sh
make build        # → bin/uucpmail-client (benötigt Go + C-Toolchain, Fyne/CGO)
make test         # Unit- und fakeserver-Tests (ohne Docker, ohne Netz)
```

Cross-Builds: GitHub-Actions-Matrix (`.github/workflows/build.yml`) oder
lokal `make cross` (fyne-cross, Docker; macOS nur nativ/CI).

## Benutzung

```sh
uucpmail-client            # GUI (Erststart: Schlüssel erzeugen, Pubkey an
                           # beta@uucpmail.de senden, Node/Adresse eintragen)
uucpmail-client keygen     # SSH-Schlüssel erzeugen, Pubkey anzeigen
uucpmail-client send -subject "Hi" ziel@example.com < text.md
uucpmail-client sync       # UUCP-Session: senden + abholen (wie Winlink)
uucpmail-client queue      # wartende Jobs anzeigen
```

Konfiguration/Daten liegen unter dem OS-Konfigurationsverzeichnis
(`~/Library/Application Support/uucpmail`, `%AppData%\uucpmail`,
`~/.config/uucpmail`): `config.json`, Schlüssel, `known_hosts` (TOFU),
Mail-Store (`mail/`, eine `.eml` pro Nachricht + bbolt-Index) und
UUCP-Spool (`spool/`).

## Tests gegen den Server-Dev-Stack

`test/it/run-it.sh` startet den Docker-Dev-Stack aus `../server/` (read-only;
das ssh-gateway ist dort offiziell auf `127.0.0.1:2222` published), legt einen
Testnode per `uucpadm` an und fährt Send/Sync end-to-end.
Alternativ ist `../server/client-sandbox/` die Taylor-Referenzimplementierung
zum Vergleich.

## OpenPGP

Der Client bringt OpenPGP nativ mit (reines Go via ProtonMail/go-crypto –
keine GnuPG-Installation nötig, identisch auf allen Plattformen):

- **Schlüssel**: Einstellungen → PGP. Eigenes Schlüsselpaar interaktiv
  erzeugen (Ed25519/X25519, v4 – kompatibel mit GnuPG ≥ 2.1), vorhandenen
  privaten Schlüssel importieren, eigenen Public Key exportieren (Copy/
  Datei), fremde Public Keys importieren/löschen. Ablage als Armor-Dateien
  `pubring.asc`/`secring.asc` im Konfigurationsverzeichnis (0600).
- **Senden**: Toggle-Buttons „Signieren" und „Verschlüsseln" im Compose-
  Fenster (einzeln oder kombiniert). Format: PGP/MIME (RFC 3156);
  signiert+verschlüsselt als kombinierte One-Pass-Message. Verschlüsseln
  braucht Public Keys ALLER Empfänger; die eigene Kopie wird immer
  mitverschlüsselt (Gesendet bleibt lesbar). CLI: `send -sign -encrypt
  [-pgp-pass …]`, `pgp-keygen -name -email [-pass]`.
- **Empfangen**: multipart/encrypted, multipart/signed, Inline-PGP und
  Clearsign. Beim Öffnen verschlüsselter Mails fragt der Client (Ja/Nein),
  dann Passphrase; der entsperrte Schlüssel bleibt für die konfigurierte
  Dauer (Default 15 min) im Speicher – weitere verschlüsselte Mails öffnen
  sich solange ohne Nachfrage. Entschlüsseltes wird nie auf die Platte
  geschrieben. Signaturstatus in der Ansicht: ✓ gültig / ✗ ungültig /
  ? Schlüssel unbekannt. Kein Trust-/WoT-Modell in V1 – Schlüssel werden
  manuell per Fingerprint verifiziert.

Interop ist end-to-end gegen GnuPG 2.5 verifiziert: vom Client signierte+
verschlüsselte Mails entschlüsselt `gpg` mit „Korrekte Signatur"; von
GnuPG erzeugte Nachrichten (inkl. Inline-PGP) entschlüsselt und prüft der
Client. Nachstellen: eigenen Public Key exportieren → `gpg --import`;
Gegenrichtung `gpg -se -r <adresse> --armor` und die Ausgabe als Mailtext
senden.

## Adressbuch

Minimales Adressbuch (Einstellungen → Kontakte): Vorname, Nachname,
E-Mail und optional ein GnuPG Public Key. Im Compose-Fenster öffnet der
Adressbuch-Button (neben dem Von-Dropdown) die Auswahl im Outlook-Stil –
Zeile wählen und per An/CC/BCC einfügen (Mehrfachauswahl, automatische
Trenner, Duplikat-Schutz).

Schlüssel-Handhabung: Beim Anlegen/Bearbeiten eingefügte Public Keys
landen im zentralen Schlüsselbund (pubring.asc); der Kontakt speichert
nur eine Fingerprint-Referenz. Die Verschlüsselung findet Empfänger-
Schlüssel weiterhin über die E-Mail-Adresse – ein Kontakt mit Key
verschlüsselt also automatisch. V1-Verhalten: Beim Löschen eines
Kontakts oder Ersetzen seines Schlüssels bleibt der (alte) Key im
Schlüsselbund und ist im PGP-Tab löschbar. Hinweis: Das Key-Matching
nutzt die UID des Schlüssels – weicht dessen E-Mail von der Kontakt-
Adresse ab, greift die automatische Verschlüsselung nicht.

## Grenzen des Store-and-Forward

UUCP ist asynchron: „Gesendet" bedeutet **an den Server übergeben**, nicht
zugestellt. Die inhaltliche Prüfung (Absender-Autorisierung, Empfänger,
Spam) passiert serverseitig erst nach der Übertragung. Stand heute gilt:

- Nicht registrierte Absenderadressen lehnt der Server mit 553 ab; der
  Absender-Node erhält dazu beim nächsten Sync einen Ablehnungs-DSN
  („Outgoing mail rejected: sender not authorized") an seine Hauptadresse
  (seit Server-Commit 36dc306, live verifiziert). Trotzdem gilt: nur
  registrierte Identitäten verwenden (Hinweis im Tab „Identitäten").
- Scheitert die Weiterleitung nach Annahme dauerhaft, kommt ein DSN von
  MAILER-DAEMON@uucpmail.de beim nächsten Sync als normale Mail an.
- Übertragungsfehler während der UUCP-Session (Ablehnung, Abbruch) sind
  sofort sichtbar: Die Mail bleibt mit ⚠ und Fehlertext im Postausgang.

Erkenntnisse und Fragen Richtung Serverentwicklung werden in
[../EXCHANGE.md](../EXCHANGE.md) ausgetauscht.

## Lizenz

Apache License 2.0 – siehe [LICENSE](LICENSE).
Copyright © 2026 Christoph Flügel.
