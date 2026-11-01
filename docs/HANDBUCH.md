# uucpmail-Client – Handbuch

Nutzerdokumentation für die grafische Oberfläche und die Kommandozeile.
(English version: [MANUAL.md](MANUAL.md))

Der Client ist ein Store-and-Forward-Mailclient für uucpmail.de: Mails
werden lokal in einer Outbox gesammelt und bei einer Synchronisation per
UUCP über SSH an den Server übergeben; eingehende Mails werden dabei
abgeholt. Es gibt keinen Dauerbetrieb und kein automatisches Polling –
synchronisiert wird, wenn du es auslöst.

**Wichtig zum Verständnis:** „Gesendet" bedeutet *an den Server übergeben*,
nicht *zugestellt*. Lehnt der Dienst eine Mail später ab (z. B. nicht
registrierter Absender), kommt eine Unzustellbarkeits-Mail (DSN) an deine
Hauptadresse – beim nächsten Sync.

---

## Teil 1: Oberfläche

### Hauptfenster

Oben die Werkzeugleiste (Neue Nachricht, Synchronisieren, Adressbuch,
Protokollfenster, Einstellungen), darunter die Ordner-Tabs:

- **Posteingang** – empfangene Mails; Zähler zeigt Ungelesene.
- **Entwürfe** – gespeicherte Entwürfe (Doppelklick oder „Bearbeiten"
  öffnet sie wieder; Autosave-Intervall einstellbar).
- **Postausgang** – wartet auf die nächste Synchronisation; Zähler zeigt
  die Anzahl. Löschen storniert auch den Sendeauftrag.
- **Gesendet** – an den Server übergebene Mails.
- **Papierkorb** – gelöschte Mails aus allen Ordnern (siehe unten).

Darunter die Mailliste (Spaltenbreiten per Ziehen der Trenner anpassbar,
Klick auf die Datum-Überschrift wechselt die Sortierrichtung) und die
Mailansicht. Die Statusleiste fasst die letzte Synchronisation zusammen.

### Tastaturbedienung

`Tab` wechselt zwischen Mailliste und Mailtext (der aktive Bereich ist
farblich umrandet); andere Elemente sind bewusst nicht per Tab erreichbar.

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

Lösch-Rückfragen sind komplett per Tastatur bedienbar: `Enter` bestätigt
(Fokus startet auf „Ja"), Pfeiltasten/`Tab` wechseln, `Esc` bricht ab.

### Mailansicht und Aktionen

Unter der Mail die Aktionsleiste: Antworten, Allen antworten,
Weiterleiten, Löschen – plus kontextabhängig:

- **Entschlüsseln** bei PGP-verschlüsselten Mails (Passphrase wird für die
  eingestellte Zeit zwischengespeichert).
- **Bearbeiten** (nur Entwürfe).
- **Wiederherstellen** (nur Papierkorb) – zurück in den Ursprungsordner,
  wirkt auf die ganze Markierung.
- **Ungelesen (u)** (nur Posteingang).
- **Erneut bearbeiten** (nur Gesendet) – öffnet die Mail als neue,
  vollständig vorbefüllte Nachricht (BCC ist in der Gesendet-Kopie nicht
  enthalten; verschlüsselt gespeicherte Mails zuerst entschlüsseln).
- **Quelltext** – rohe Nachricht anzeigen.

Anhänge erscheinen als Speichern-Knöpfe. Enthält ein Anhang einen
öffentlichen PGP-Schlüssel, gibt es zusätzlich **„Schlüssel importieren"**:
übernimmt den Schlüssel in den Schlüsselbund und bietet an, den Absender
ins Adressbuch zu übernehmen.

### Mehrfachauswahl

Shift+`↑`/`↓` (oder Shift-Klick-Logik per Cursor) markiert einen
zusammenhängenden Bereich. Darauf wirken: Löschen (mit Rückfrage),
Wiederherstellen, Ungelesen sowie `F` (hängt alle markierten Mails als
`.eml`-Dateien an eine neue Nachricht). Antworten ist bei Mehrfachauswahl
bewusst inaktiv.

### Papierkorb

Löschen verschiebt ohne Rückfrage in den Papierkorb (rückholbar); im
Papierkorb löscht Entf endgültig, immer mit Rückfrage. Beim Programmstart
werden Papierkorb-Mails automatisch endgültig entfernt, die älter sind als
der Wert in Einstellungen → Allgemein (Standard 30 Tage, 0 = nie).

### Neue Nachricht

Felder Von (Identitäten-Auswahl), An, CC, BCC (einklappbar), Betreff,
Text. `Tab` springt vom Betreff direkt in den Nachrichtentext (die
Knopfleiste wird übersprungen; `Shift+Tab` führt zurück). Der
Adressbuch-Knopf öffnet den Auswahl-Dialog (An/CC/BCC-Zuordnung).

Die Leiste über dem Text fügt Markdown-Schnipsel ein; dahinter – sobald
ein eigener PGP-Schlüssel existiert – die Umschalter **Signieren**,
**Verschlüsseln** (verlangt öffentliche Schlüssel aller Empfänger;
Gesendet-Kopie wird mitverschlüsselt) und **Schlüssel anhängen** (eigener
öffentlicher Schlüssel als `.asc`-Anhang).

„Speichern" legt einen Entwurf an (automatische Zwischenspeicherung läuft
zusätzlich). Ist in Einstellungen → Allgemein eine Signatur aktiviert,
wird sie mit `-- `-Trennzeichen eingefügt (bei Antworten/Weiterleitungen
nur mit der zweiten Option). Eine weiche Größenwarnung erscheint, wenn die
Mail das eingestellte Limit überschreitet.

### Adressbuch

Erreichbar über die Werkzeugleiste: Kontakte mit Vor-/Nachname, E-Mail
und optionalem PGP-Schlüssel – wählbar aus dem Schlüsselbund (Dropdown)
oder per eingefügtem Schlüsseltext. Ein Kontakt mit Schlüssel wird beim
Verschlüsseln automatisch gefunden (Zuordnung über die E-Mail-Adresse).

### Einstellungen (Cmd/Ctrl+,)

Änderungen gelten erst mit „Speichern"; „Abbrechen" verwirft sie.
(Ausnahmen: die Schlüssel-Aktionen im SSH-Tab wirken sofort.)

- **Account:** Node-ID, Server, Port – aus der Registrierungs-Antwort.
- **Identitäten:** Absenderadressen verwalten, Standard festlegen (★).
- **SSH:** Fingerprint und `authorized_keys`-Zeile des Transportschlüssels
  (Kopieren-Knopf für die Registrierung), **„Neuen Schlüssel erzeugen …"** und
  **Schlüssel importieren…** (vorhandenes OpenSSH-Schlüsselpaar, z. B.
  `id_ed25519`, ersetzt den internen Schlüssel – mit Warnung; der neue
  öffentliche Schlüssel muss danach beim Dienst registriert werden;
  passphrase-geschützte Dateien werden abgelehnt). Dazu der Haken
  **„FIDO2 SSH Key nutzen"**: geführte Einrichtung eines residenten
  Schlüssels auf einem Security-Key (PIN + Berührung bei jeder
  Verbindung; der Datei-Schlüssel bleibt als Rückfall erhalten).
  Unten die Server-Hostkeys aus der known_hosts.
- **PGP:** eigenes Schlüsselpaar erzeugen (Gültigkeit wählbar, mehrere
  Identitäten je Schlüssel), importieren/exportieren, fremde Schlüssel
  verwalten, Passphrase-Cache-Dauer.
- **Tarif:** zuletzt abgefragter Dienststatus (Tarif, Quota/Belegung,
  Queue, Identitäten, Aufbewahrung) mit Abfrage-Knöpfen – die Antwort
  kommt als Mail: ein Sync sendet die Anfrage, der nächste holt sie ab.
- **Allgemein:** Sprache, Farbschema, Schriftgröße, Entwurf-Autosave,
  max. Mailgröße (MB, weiche Warnung, Obergrenze 10), Warnschwelle
  Mails/Stunde, Papierkorb-Aufbewahrung, Scroll-Sprungweite, Signatur
  (eigener Dialog), Anzeige des Datenverzeichnisses.
- **Sicherung:** verschlüsselte Sicherung der Identität erstellen
  (Config, Schlüssel, Adressbuch – ohne Mails) bzw. destruktiv
  wiederherstellen; außerdem **„Portablen Datenordner erstellen…"**
  (siehe unten).

### Portabler Modus

Liegt ein Ordner `uucpmail-data` neben dem Programm, speichert der Client
alles dort (Config, Schlüssel, Mails, Fensterzustand) – ideal für einen
USB-Stick; der Gastrechner bleibt sauber. Einrichtung: Ordner anlegen oder
den Knopf in Einstellungen → Sicherung nutzen (kopiert die Identität, ohne
Mails). Auf fremden Rechnern muss der Server-Hostkey beim ersten Sync
einmal bestätigt werden.

### FIDO2-Security-Key

Details und Hintergrund: README-Abschnitt „FIDO2-Security-Key für SSH".
Kurzfassung: Haken im SSH-Tab setzen → PIN → vorhandenen residenten
Schlüssel übernehmen oder neu erzeugen → angezeigte Zeile beim Dienst
registrieren. Jede Verbindung fragt die PIN ab und verlangt eine
Berührung des Tokens; solange das Token auf die Berührung wartet, zeigt
ein kleines Popup „Security-Key berühren …" den Wartezustand an und
schließt sich von selbst.

---

## Teil 2: Kommandozeile

Alle Kommandos nutzen dasselbe Datenverzeichnis wie die Oberfläche
(portabler Modus wird automatisch erkannt). Aufruf ohne Argumente startet
die GUI.

### keygen – SSH-Schlüssel erzeugen oder importieren

```
uucpmail-client keygen [-comment TEXT] [-import PFAD]
```

| Flag | Bedeutung |
|---|---|
| `-comment` | Kommentar für den öffentlichen Schlüssel (Default `uucpmail`) |
| `-import` | vorhandenen OpenSSH-Privatschlüssel importieren statt neu zu erzeugen |

Gibt Pfad, Fingerprint und die `authorized_keys`-Zeile zur Registrierung
aus. Ohne `-import` wird ein neuer ed25519-Schlüssel erzeugt (bricht ab,
wenn schon einer existiert); mit `-import` wird der interne Schlüssel
ersetzt.

```
uucpmail-client keygen -import ~/.ssh/id_ed25519
```

### pgp-keygen – PGP-Schlüsselpaar erzeugen

```
uucpmail-client pgp-keygen -name NAME -email ADRESSE[,ADRESSE…] [-pass P] [-years N]
```

| Flag | Bedeutung |
|---|---|
| `-name` | Name für die Schlüssel-UIDs |
| `-email` | E-Mail-Adresse(n), Komma-getrennt – ein Schlüssel kann mehrere Identitäten tragen |
| `-pass` | Passphrase (leer = ungeschützt) |
| `-years` | Gültigkeit in Jahren, 0 = läuft nicht ab (Default 2, max. 100) |

```
uucpmail-client pgp-keygen -name "Christoph" -email christoph@uucpmail.de,postmaster@uucpmail.de -years 2
```

### send – Mail von stdin in die Outbox stellen

```
uucpmail-client send [Flags] EMPFÄNGER…
```

| Flag | Bedeutung |
|---|---|
| `-from` | Absender-Identität (Default: default_identity) |
| `-subject` | Betreff |
| `-cc` / `-bcc` | weitere Empfänger, Komma-getrennt (BCC nur Envelope) |
| `-attach` | Datei anhängen (Pfad) |
| `-sign` / `-encrypt` | PGP signieren / verschlüsseln |
| `-pgp-pass` | Passphrase des eigenen PGP-Schlüssels (nur für Skripte) |
| `-sync` | nach dem Einreihen sofort synchronisieren |

```
echo "Hallo!" | uucpmail-client send -subject "Gruß" -sync empfaenger@example.org
```

### sync – UUCP-Session fahren

```
uucpmail-client sync
```

Sendet alles aus der Outbox und holt wartende Mails ab. FIDO2-Modus:
PIN-Abfrage im Terminal, Berührungs-Hinweis auf stderr.

### queue – Outbox/Spool-Status

```
uucpmail-client queue
```

Zeigt wartende Sendeaufträge (inkl. Steuerjobs wie `[uuctl]`).

### ctl – Dienststatus abfragen

```
uucpmail-client ctl quota|usage [-sync]
```

Reiht eine Self-Service-Abfrage ein (`quota` = Tarif & Limits, `usage` =
aktueller Verbrauch). Die Antwort kommt als Mail an die Hauptadresse:
ein Sync überträgt die Anfrage, der nächste holt die Antwort ab (sie
aktualisiert automatisch den Tarif-Reiter). `-sync` fährt die erste
Session sofort.

### fido2 – Security-Key einrichten

```
uucpmail-client fido2 list
uucpmail-client fido2 enroll [-new]
```

`list` zeigt die residenten SSH-Schlüssel auf dem Token (PIN nötig;
macOS/Linux). `enroll` übernimmt den ersten vorhandenen Schlüssel bzw.
erzeugt mit `-new` einen neuen, speichert den lokalen Stub, aktiviert den
FIDO2-Modus und gibt die Registrierungszeile aus.

### Fehlerverhalten

Jedes Kommando beendet sich bei Fehlern mit Exit-Code ≠ 0 und einer
Meldung auf stderr (`Fehler: …`) – geeignet für Skripte.
