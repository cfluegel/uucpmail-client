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
Web of Trust – Spinnennetz-Symbol, aktiv sobald ein eigener privater
PGP-Schlüssel existiert –, Protokollfenster, Einstellungen), darunter die
Ordner-Tabs:

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
  eingestellte Zeit zwischengespeichert; bei einem Schlüssel auf der
  Smartcard wird stattdessen die Karten-PIN abgefragt).
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
öffentlicher Schlüssel als `.asc`-Anhang). Liegt der Schlüssel auf einer
Smartcard, fragt der Client beim Senden die Karten-PIN ab; sie wird wie
die Passphrase für die eingestellte Cache-Dauer zwischengespeichert.

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
  Die Node-ID darf nur Buchstaben, Ziffern und `._-` enthalten;
  Leerzeichen (z. B. vom Kopieren) werden beim Speichern entfernt, bei
  ungültigen Zeichen bricht der Abgleich mit einer Fehlermeldung ab.
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
  Ist bereits ein Token eingerichtet, startet „Neu einrichten…" den
  Wizard erneut (z. B. für den Umzug auf ein anderes Token; der bisherige
  Schlüssel bleibt auf dem alten Token und registriert).
  Unten die Server-Hostkeys aus der known_hosts.
- **PGP:** eigenes Schlüsselpaar erzeugen (Gültigkeit wählbar, mehrere
  Identitäten je Schlüssel; eine Passphrase ist Pflicht – stimmen die
  beiden Eingaben nicht überein, öffnet sich der Dialog mit geleerten
  Passphrase-Feldern erneut) oder privaten Schlüssel importieren. Je
  eigenem Schlüssel: **Exportieren** (öffentlicher Schlüssel zum Kopieren
  oder Speichern), **Widerruf …** und der rote Mülleimer. „Widerruf …"
  erzeugt ein Widerrufszertifikat wie `gpg --gen-revoke`: Grund wählen
  (kein Grund angegeben, kompromittiert, ersetzt, nicht mehr verwendet),
  optionale Beschreibung, Passphrase – das Zertifikat wird angezeigt und
  lässt sich kopieren oder als `<Fingerprint>.rev` speichern. Der Schlüssel
  selbst bleibt gültig; erst wer das Zertifikat in GnuPG importiert oder
  auf einem Keyserver veröffentlicht, widerruft ihn. Sicher aufbewahren!
  Darunter der Abschnitt **Smartcard** (siehe unten), Passphrase-Cache-Dauer
  und Keyserver-Adresse (Standard `hkps://keyserver.ubuntu.com`).
  Öffentliche Schlüssel anderer verwaltet das Fenster *Web of Trust*.
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

### OpenPGP-Smartcard

Der PGP-Schlüssel kann auf einer OpenPGP-Smartcard liegen (z. B. GnuPG-
bzw. ZeitControl-Karte, Spec 2.x–3.4, oder YubiKey; Kartenleser per
PC/SC). Abschnitt **Smartcard** in Einstellungen → PGP:

- **Statuszeile:** erkannte Karte mit belegten Slots (SIG/DEC/AUT);
  „Erneut prüfen" sucht neu. Hält ein anderes Programm (gpg/scdaemon) die
  Karte, erscheint ein Hinweis – dann z. B. `gpgconf --kill scdaemon`.
- **Karte für Signieren/Entschlüsseln verwenden:** schaltet den
  Kartenbetrieb ein oder aus (wirkt sofort).
- **Karte einrichten …** (Wizard für eine leere Karte): Admin-PIN →
  Werks-PINs müssen durch eigene ersetzt werden (Nutzer-PIN mind. 6,
  Admin-PIN mind. 8 Zeichen); bei einer frischen Karte, die es kann,
  zusätzlich **„PIN nur gehasht übertragen (KDF)"** (standardmäßig an) →
  Name, Identitäten, Gültigkeit des Hauptschlüssels, RSA-Länge (2048,
  3072 oder 4096, je nach Karte), Passphrase (Pflicht) → **Pflicht-Backup**
  des Hauptschlüssels als Datei, *bevor* etwas auf die Karte geschrieben
  wird → die drei Unterschlüssel (Signieren, Entschlüsseln,
  Authentisieren) kommen auf die Karte. Der Hauptschlüssel bleibt,
  durch die Passphrase geschützt, im Schlüsselbund. Ohne das Backup sind
  bei Verlust der Karte alle verschlüsselten Mails verloren.
- **Vorhandene Karte verwenden …:** übernimmt eine anderswo (z. B. mit
  GnuPG) eingerichtete Karte; der passende öffentliche Schlüssel muss im
  Schlüsselbund sein. RSA-Karten voll nutzbar; Ed25519-Karten nur zum
  Signieren.
- **Kartendetails …:** Seriennummer, Hersteller, Leser, Spec-Version,
  Algorithmen, Slot-Belegung, verbleibende PIN-Versuche, Signatur-PIN
  (bei jeder Signatur oder einmal pro Sitzung), KDF, Signaturzähler –
  ohne PIN. Dort außerdem:
  - **PIN entsperren …** – setzt mit der Admin-PIN eine neue Nutzer-PIN
    und stellt die Fehlversuche zurück (nach drei Fehleingaben).
  - **Werksreset …** – löscht alle Schlüssel auf der Karte und setzt die
    PINs zurück (123456 / 12345678). Zur Bestätigung muss die
    Seriennummer eingetippt werden, danach folgt eine Rückfrage. War der
    Client auf diese Karte eingerichtet, wird die Kartennutzung
    ausgeschaltet.
- **Subkeys verlängern …:** Die Karten-Unterschlüssel sind 1 Jahr
  gültig; der Knopf verlängert sie um ein Jahr (braucht die Passphrase
  des Hauptschlüssels, an der Karte ändert sich nichts). Beim
  Programmstart warnt der Client, wenn sie in weniger als 30 Tagen
  ablaufen.

### Web of Trust

Fenster über das Spinnennetz-Symbol der Werkzeugleiste.

- **Mein Schlüssel:** **Signaturen …** (wer hat meinen Schlüssel
  zertifiziert) und **Fingerprint-Streifen (PDF) …** – eine A4-Seite mit
  abschneidbaren Streifen (Key-ID und Typ, Name und E-Mail, voller
  Fingerprint) für Key-Signing-
  Partys; in Originalgröße drucken.
- **Öffentliche Schlüssel anderer:** je Schlüssel
  - **Zertifizieren …** – nach persönlicher Prüfung von Fingerprint und
    Identität. Der Dialog zeigt Fingerprint, Algorithmen, Erstellungs-
    und Ablaufdatum (abgelaufene Schlüssel mit gelbem Warndreieck). Zu
    zertifizierende Identitäten und eigenen Schlüssel wählen, dann
    (Pflichtauswahl): **„Signieren und an den Inhaber senden"**
    (vorgewählt) oder **„Nur signieren"**. Senden heißt: je Identität
    eine signierte und an den Schlüssel des Inhabers verschlüsselte Mail
    mit genau dieser Zertifizierung an genau diese Adresse (nach einer
    Bestätigung mit Adressliste; die Mails liegen dann im Postausgang).
  - **Senden …** – verschickt eine vorhandene eigene Zertifizierung
    (später oder erneut). Nur aktiv, wenn der Schlüssel eine gültige
    Signatur von dir trägt; wählbar sind die signierten Identitäten, die
    primäre ist vorgewählt.
  - **Signaturen …** – Zertifizierungen anderer, je Identität
    zugeklappt; Rechtsklick auf einen unbekannten Unterzeichner lädt
    dessen Schlüssel vom Keyserver.
  - **?** – Schlüsselinformationen: Haupt- und Unterschlüssel mit
    Algorithmus, Verwendung (S = signieren, C = zertifizieren,
    E = verschlüsseln, A = authentisieren), Fingerprint, Erstellungs-
    und Ablaufdatum, Status (gültig / abgelaufen / widerrufen).
  - roter Mülleimer – Schlüssel löschen.
  „✓ geprüft" hinter dem Namen heißt: du hast den Schlüssel gültig
  zertifiziert.
- Darunter **Öffentlichen Schlüssel importieren …** (Text einfügen oder
  Datei; enthaltene neue Zertifizierungen werden übernommen) und **Vom
  Keyserver herunterladen …** (lange Key-ID oder Fingerprint).

Kann die Zertifizierung nicht zurückgeschickt werden, weil der Schlüssel
keinen gültigen Verschlüsselungs-Unterschlüssel hat, bleibt sie
gespeichert. Typisch bei sehr alten Schlüsseln (z. B. GnuPG 1.4,
DSA/ElGamal), deren Unterschlüssel keine Verwendungsangabe tragen – im
Info-Dialog steht dann „keine Angabe im Schlüssel". Solche Schlüssel
unterstützt der Client bewusst nicht zum Verschlüsseln.

### Anwendungsfälle: welcher Schlüssel wofür?

Ein PGP-Schlüssel besteht aus dem **Hauptschlüssel** und
**Unterschlüsseln**. Beim Karten-Setup liegen nur die Unterschlüssel auf
der Karte; der Hauptschlüssel bleibt, mit der Passphrase geschützt, im
Schlüsselbund (bzw. im Backup).

| Aktion | benutzter Schlüssel | Abfrage |
|---|---|---|
| Mail signieren | Signatur-Unterschlüssel (genau eine Signatur) | Karten-PIN bzw. Passphrase |
| Mail entschlüsseln | Verschlüsselungs-Unterschlüssel | Karten-PIN bzw. Passphrase |
| Fremden Schlüssel zertifizieren | Hauptschlüssel | Passphrase |
| Widerrufszertifikat erzeugen | Hauptschlüssel | Passphrase |
| Karten-Subkeys verlängern | Hauptschlüssel | Passphrase |

Unterwegs nur mit der Karte (Hauptschlüssel nicht auf dem Rechner)
kannst du also Mails signieren und entschlüsseln, aber **keine fremden
Schlüssel zertifizieren** – das darf in OpenPGP nur der Hauptschlüssel.
Zertifizieren daher am eigenen Rechner (oder mit dem Backup); „Nur
signieren" und späteres **Senden …** helfen, das zu entkoppeln.

### Beispiel: sicher unterwegs an fremden Rechnern

Wofür FIDO2-Key, Smartcard und portabler Modus zusammen gedacht sind:
mobil und vertraulich mailen an einem Rechner, der dir nicht gehört
(Hotel, Bibliothek, Rechner von Bekannten) – ohne dort etwas zu
installieren und ohne dass deine privaten Schlüssel den Token verlassen.

**Du brauchst:** einen USB-Stick mit dem Programm und dem Ordner
`uucpmail-data` (portabler Modus), einen FIDO2-Security-Key für die
Verbindung zum Server und eine OpenPGP-Karte samt Leser für Signieren
und Entschlüsseln. Ein YubiKey 5 kann beides zugleich (FIDO2 und
OpenPGP). Dazu im Kopf: FIDO2-PIN, Karten-PIN, Node-ID.

**Einmalig zu Hause einrichten:**

1. Einstellungen → Sicherung → **„Portablen Datenordner erstellen…"** auf
   den Stick.
2. Den Client vom Stick starten; Einstellungen → SSH → **„FIDO2 SSH Key
   nutzen"** und den angezeigten Schlüssel beim Dienst registrieren.
3. Einstellungen → PGP → **Karte einrichten …** (mit KDF). Das
   Pflicht-Backup des Hauptschlüssels gehört an einen sicheren Ort
   *zu Hause*, nicht auf den Stick.
4. Fremde Schlüssel, die du unterwegs brauchst, vorher importieren und
   zertifizieren – zertifizieren geht nur mit dem Hauptschlüssel (siehe
   Anwendungsfälle).

**Unterwegs:**

1. Stick, Security-Key und Kartenleser einstecken, Programm vom Stick
   starten. Beim ersten Sync auf einem neuen Rechner den Server-Hostkey
   einmal bestätigen.
2. **Synchronisieren:** FIDO2-PIN eingeben und den Key berühren – ohne den
   Key kommt niemand an dein Postfach.
3. **Lesen:** verschlüsselte Mails öffnen mit der Karten-PIN; die
   Entschlüsselung rechnet die Karte, der Klartext wird nicht auf die
   Platte geschrieben.
4. **Schreiben:** signieren und verschlüsseln wie gewohnt, Karten-PIN beim
   Senden.
5. Danach Programm beenden, alles abziehen. Auf dem Gastrechner bleibt
   nichts zurück – alle Daten liegen auf dem Stick.

**Was das schützt – und was nicht:**

- Die privaten Schlüssel lassen sich vom Gastrechner nicht kopieren; ohne
  Token und PINs sind Stick-Inhalt und abgefangene Daten wertlos für
  Verbindung und Entschlüsselung.
- Ein verseuchter Gastrechner kann trotzdem mitlesen, was *während* der
  Nutzung auf dem Bildschirm steht oder getippt wird (Mailtext, PINs). Er
  kann den Token für die Dauer der Sitzung missbrauchen, nicht aber die
  Schlüssel behalten. Für wirklich Heikles nur vertrauenswürdige Rechner
  nutzen.
- Auf dem Stick liegen weiterhin der Datei-SSH-Schlüssel (Rückfall) und
  der Hauptschlüssel – beide sind nur so sicher wie Stick und Passphrase.
  Ist beim Dienst nur noch der Token-Schlüssel registriert, nützt der
  Datei-SSH-Schlüssel einem Finder nichts; für den Hauptschlüssel ist
  eine starke Passphrase Pflicht.
- Zertifizieren fremder Schlüssel ist unterwegs nur möglich, wenn du den
  Hauptschlüssel samt Passphrase dabei hast – sonst „Nur signieren“ und
  **Senden …** zu Hause erledigen.
- Voraussetzungen am Gastrechner: USB-Zugang und ein Kartenleser, den das
  System ohne Treiberinstallation erkennt (üblich bei CCID-Lesern und
  YubiKeys). Unter Windows läuft FIDO2 über die System-Oberfläche; dieser
  Zweig ist noch nicht live getestet.

---

## Teil 2: Kommandozeile

Alle Kommandos nutzen dasselbe Datenverzeichnis wie die Oberfläche
(portabler Modus wird automatisch erkannt). Aufruf ohne Argumente startet
die GUI. Die Kommandozeile gibt grundsätzlich Englisch aus (skript-stabil);
die Spracheinstellung betrifft nur die Oberfläche.

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
uucpmail-client pgp-keygen -name NAME -email ADRESSE[,ADRESSE…] -pass P [-years N]
```

| Flag | Bedeutung |
|---|---|
| `-name` | Name für die Schlüssel-UIDs |
| `-email` | E-Mail-Adresse(n), Komma-getrennt – ein Schlüssel kann mehrere Identitäten tragen |
| `-pass` | Passphrase (Pflicht) |
| `-years` | Gültigkeit in Jahren, 0 = läuft nicht ab (Default 2, max. 100) |

```
uucpmail-client pgp-keygen -name "Christoph" -email christoph@uucpmail.de,postmaster@uucpmail.de -pass 'geheim' -years 2
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
PIN-Abfrage im Terminal, Berührungs-Hinweis auf stderr. Antwortet der
Server 2 Minuten lang nicht, bricht die Session mit einer Fehlermeldung
ab (gilt auch für den Abgleich aus der Oberfläche).

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

### pgp-card – OpenPGP-Smartcard

```
uucpmail-client pgp-card status
uucpmail-client pgp-card reset SERIENNUMMER
```

`status` zeigt ohne PIN alle erkannten Karten: Seriennummer, Hersteller,
Leser, Spec-Version, Algorithmen, Slot-Belegung, PIN-Versuche,
Signatur-PIN-Modus, KDF und Signaturzähler. `reset` setzt die Karte in
den Werkszustand (alle Schlüssel weg, PINs 123456 / 12345678) und fragt
zur Bestätigung die Seriennummer erneut ab; passt sie nicht, bricht das
Kommando ab. Hält gpg/scdaemon die Karte, vorher
`gpgconf --kill scdaemon`.

### Fehlerverhalten

Jedes Kommando beendet sich bei Fehlern mit Exit-Code ≠ 0 und einer
Meldung auf stderr (`error: …`) – geeignet für Skripte.
