# uucpmail-client

*Deutsche Fassung – [English version](README.md)*

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

**Vollständige Dokumentation** (Oberfläche Schritt für Schritt und alle
CLI-Kommandos mit Flags und Beispielen): **[docs/HANDBUCH.md](docs/HANDBUCH.md)**.
Die CLI bietet neben `sync` u. a. `send` (Mail aus stdin), `queue`,
`keygen [-import]`, `pgp-keygen`, `ctl quota|usage`, `fido2 enroll` und
`pgp-card status|reset`.

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

## FIDO2-Security-Key für SSH

Der SSH-Transportschlüssel kann auf einem FIDO2-Security-Key (Yubikey,
Nitrokey 3, SoloKeys u. a., FIDO2-PIN erforderlich) als residenter
Schlüssel liegen – der private Schlüssel verlässt das Token nie. Einrichtung
über Einstellungen → SSH → „FIDO2 SSH Key nutzen" (geführter Dialog:
vorhandenen Schlüssel übernehmen oder neu erzeugen) oder per CLI:

```
uucpmail-client fido2 enroll -new   # residenten Schlüssel erzeugen
uucpmail-client fido2 list          # Schlüssel auf dem Token anzeigen
```

Den angezeigten Public Key (`sk-ssh-ed25519@…`) beim Dienst als
Node-Schlüssel registrieren. Jede Verbindung fragt die PIN ab (bewusst ohne
Zwischenspeicherung) und verlangt eine Berührung des Tokens. Der lokale
Stub (`fido2_key.json`) enthält kein Schlüsselmaterial und wandert im
portablen Modus mit – zusammen mit Token + PIN + Node-ID reicht das für die
Einrichtung an einem fremden Rechner. Für die OpenSSH-CLI lässt sich der
Schlüssel jederzeit mit `ssh-keygen -K` regenerieren. Der bisherige
Datei-Schlüssel bleibt als Rückfalloption erhalten (Haken abwählen).
Windows: Einrichtung/Signieren laufen über die System-UI (WebAuthn);
vorhandene Token-Schlüssel lassen sich dort nicht auflisten – auf
macOS/Linux einrichten und den portablen Stub mitnehmen. Der
Windows-Zweig ist noch nicht live getestet.

## Portabler Modus

Liegt neben dem Client-Programm ein Ordner `uucpmail-data`, speichert der
Client alle Daten dort (Konfiguration, Schlüssel, Mail-Store, Spool,
Fensterzustand) – ideal für einen USB-Stick, der Gastrechner bleibt sauber.
Ohne diesen Ordner gilt wie bisher das Benutzer-Konfigurationsverzeichnis.

Einrichtung: Ordner `uucpmail-data` neben dem Binary anlegen (beim nächsten
Start erscheint der Einrichtungs-Assistent) – oder in den Einstellungen
unter „Sicherung" den Knopf „Portablen Datenordner erstellen…" nutzen, der
die bestehende Identität (Konfiguration, Schlüssel, Adressbuch – ohne
Mails) in einen neuen Datenordner kopiert.

Hinweise: Auf einem fremden Rechner muss der Server-Hostkey beim ersten
Sync einmal bestätigt werden (TOFU). Auf FAT-formatierten Sticks sind
Unix-Dateirechte wirkungslos – den Stick entsprechend schützen.

## Tastaturbedienung

Der Client ist tastaturzentriert bedienbar. `Tab` wechselt zwischen Mailliste
und Mailtext (der aktive Bereich ist farblich umrandet); andere Elemente sind
bewusst nicht per Tab erreichbar.

| Taste | Funktion |
|---|---|
| `S` (oder `Cmd/Ctrl+S`) | Synchronisieren |
| `N` | Neue Nachricht |
| `R` / `Shift+R` | Antworten / Allen antworten |
| `F` | Weiterleiten (bei Mehrfachauswahl: als .eml-Anhänge) |
| `U` | Als ungelesen markieren (nur Posteingang) |
| `Entf`/`Backspace` | Löschen (in den Papierkorb; dort endgültig) |
| `↑`/`↓` | Liste: Cursor bewegen · Mailtext: zeilenweise scrollen |
| `Shift+↑`/`↓` | Liste: Auswahl erweitern · Mailtext: Sprung (einstellbar) |
| `Cmd/Ctrl+,` | Einstellungen öffnen |

Im Ordner Gesendet öffnet „Erneut bearbeiten" eine verschickte Mail als
neue, vollständig vorbefüllte Nachricht (BCC ist in der Gesendet-Kopie
nicht enthalten). Gelöschte Mails wandern in den Papierkorb (Ordner rechts neben Gesendet) und
können dort wiederhergestellt werden. Der Papierkorb wird beim Programmstart
automatisch geleert, wenn Nachrichten älter sind als der eingestellte Wert
(Einstellungen → Allgemein, 0 = nie).

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
  Datei), Widerrufszertifikat erzeugen. Fremde Public Keys verwaltet das
  Web-of-Trust-Fenster (siehe unten). Ablage als Armor-Dateien
  `pubring.asc`/`secring.asc` im Konfigurationsverzeichnis (0600).
- **Senden**: Toggle-Buttons „Signieren" und „Verschlüsseln" im Compose-
  Fenster (einzeln oder kombiniert). Format: PGP/MIME (RFC 3156);
  signiert+verschlüsselt als kombinierte One-Pass-Message. Verschlüsseln
  braucht Public Keys ALLER Empfänger; die eigene Kopie wird immer
  mitverschlüsselt (Gesendet bleibt lesbar). CLI: `send -sign -encrypt
  [-pgp-pass …]`, `pgp-keygen -name -email -pass`.
- **Empfangen**: multipart/encrypted, multipart/signed, Inline-PGP und
  Clearsign. Beim Öffnen verschlüsselter Mails fragt der Client (Ja/Nein),
  dann Passphrase; der entsperrte Schlüssel bleibt für die konfigurierte
  Dauer (Default 15 min) im Speicher – weitere verschlüsselte Mails öffnen
  sich solange ohne Nachfrage. Entschlüsseltes wird nie auf die Platte
  geschrieben. Signaturstatus in der Ansicht: ✓ gültig / ✗ ungültig /
  ? Schlüssel unbekannt.
- **Web of Trust** (Spinnennetz in der Werkzeugleiste): Schlüssel
  importieren (Datei, Text, Keyserver), nach persönlicher Fingerprint-
  Prüfung zertifizieren und caff-artig zurücksenden (je Identität eine
  verschlüsselte Mail), Zertifizierungen, Schlüsseldetails und Ablauf
  ansehen, Fingerprint-Streifen als PDF. Keine Vertrauensberechnung – du
  entscheidest anhand der Zertifizierungen.
- **OpenPGP-Smartcard** (GnuPG-/ZeitControl-Karte, YubiKey über PC/SC):
  Wizard bringt die Unterschlüssel auf die Karte (Hauptschlüssel bleibt im
  Schlüsselbund, Pflicht-Backup); Signieren und Entschlüsseln fragen dann
  die Karten-PIN. Kartendetails, PIN entsperren, Werksreset; CLI
  `pgp-card status|reset`.

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

## Self-Service / Dienststatus

In den Einstellungen zeigt der Reiter „Tarif" den zuletzt abgefragten
Dienststatus (Tarifname, Postfach-Quota und Belegung, Queue-Auslastung,
Identitäten, Aufbewahrung unabgeholter Mail). Zwei Buttons reihen read-only
`uuctl`-Abfragen (`GET quota`, `GET usage`) als normale UUCP-Jobs ein: ein
Sync überträgt die Anfrage, der nächste holt die Antwort ab – sie kommt als
gewöhnliche Mail an die Hauptadresse der Node und wird automatisch in den
persistierten Status geparst. Dasselbe geht per Kommandozeile:

```
uucpmail-client ctl quota -sync   # Tarif & Limits abfragen
uucpmail-client ctl usage -sync   # aktuellen Verbrauch abfragen
```

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

## Lizenz

Apache License 2.0 – siehe [LICENSE](LICENSE).
Copyright © 2026 Christoph Flügel.
