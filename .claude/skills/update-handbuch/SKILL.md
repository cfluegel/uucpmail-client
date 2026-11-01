---
name: update-handbuch
description: Aktualisiert das Nutzerhandbuch (docs/HANDBUCH.md + docs/MANUAL.md) und die README-Verweise vor einem GitHub-Release des uucpmail-Clients. Unbedingt ausführen, wenn der Nutzer einen Release vorbereitet, das Handbuch/Manual/die Doku aktualisieren oder prüfen will, oder fragt, ob die Dokumentation noch zum Code passt – auch wenn er nur „Doku prüfen" oder „vor dem Release" sagt.
---

# Handbuch vor dem Release aktualisieren

Ziel: `docs/HANDBUCH.md` (DE) und `docs/MANUAL.md` (EN) spiegeln exakt den
aktuellen Stand von Oberfläche und CLI wider – nichts Erfundenes, nichts
Vergessenes. Die beiden Dateien sind inhaltsgleich aufgebaut (Teil 1
Oberfläche, Teil 2 Kommandozeile) und müssen synchron bleiben.

Arbeite im Repo-Wurzelverzeichnis des Clients (dort, wo `go.mod` liegt).

## 1. Was hat sich seit dem letzten Release geändert?

Referenzpunkt ist der letzte Commit, der das Handbuch angefasst hat –
NICHT `git describe --tags`: die v*-Release-Tags liegen auf dem separaten
`public-main` (gesquashte Historie) und sind keine Vorfahren von `main`.

```sh
REF=$(git log -1 --format=%H -- docs/HANDBUCH.md)   # letzter Doku-Stand
git log $REF..HEAD --oneline                        # alles seitdem
git diff $REF..HEAD --stat -- pkg/ui pkg/i18n cmd README.md README.de.md
```

Ist die Liste leer, prüfe trotzdem stichprobenartig (Schritte 2–4) –
die Doku kann auch unabhängig vom Diff Fehler enthalten.

Lies die Commit-Betreffs und notiere alles Nutzersichtbare: neue Knöpfe,
Dialoge, Einstellungsfelder, Tastaturkürzel, CLI-Kommandos oder -Flags,
geändertes Verhalten. Die i18n-Diffs (`pkg/i18n/i18n.go`) sind ein guter
Indikator: neue Schlüssel = neue sichtbare UI-Elemente.

## 2. CLI-Wahrheit aus dem Binary ziehen

Die Kommandoreferenz in Teil 2 darf nur enthalten, was das Binary wirklich
kann. Baue und frage es ab – **Achtung: ohne Argumente startet das Binary
die GUI und blockiert**, nutze deshalb ein ungültiges Subkommando für den
Usage-Text:

```sh
go build -o bin/uucpmail-client ./cmd/uucpmail-client
./bin/uucpmail-client help              # Usage-Übersicht
./bin/uucpmail-client <kommando> -h     # je Subkommando: alle Flags + Defaults
```

Zweite Falle: `sync` und `queue` parsen keine Flags – `sync -h` fährt eine
ECHTE UUCP-Session gegen den Server! Diese beiden Kommandos deshalb NIE
aufrufen; ihr Verhalten steht in `cmd/uucpmail-client/main.go` und
`commands.go` (nachlesen statt ausführen).

Gleiche jedes Kommando, jedes Flag und jeden Default-Wert mit den Tabellen
in BEIDEN Handbüchern ab. Fehlende Kommandos/Flags ergänzen, entfernte
streichen, geänderte Defaults korrigieren. Keine Flags dokumentieren, die
`-h` nicht zeigt.

## 3. Oberflächen-Teil abgleichen

Prüfe Teil 1 gegen den Code-Stand – die wichtigsten Quellen der Wahrheit:

- **Tastaturtabelle** ↔ `pkg/ui/shortcuts.go` (`handleZoneKey` + die
  Canvas-Shortcuts in `setupKeyboard`). Die Tabelle steht zusätzlich in
  beiden READMEs – alle vier Stellen müssen übereinstimmen.
- **Aktionsleiste der Mailansicht** ↔ `pkg/ui/mainwindow.go`
  (`newViewPanel`: welche Buttons, welche Ordner-Sichtbarkeit in `show()`).
- **Einstellungs-Reiter** ↔ `pkg/ui/settings.go` (Tab-Liste und
  Formularfelder je Reiter; auch `buildKeyTab`, `buildTarifTab`,
  `buildBackupTab`, FIDO2-Teile).
- **Compose-Fenster** ↔ `pkg/ui/composewin.go` (Felder, Toggles,
  Tab-Sprungverhalten).
- **Ordner** ↔ `pkg/store/store.go` (`Folders`).

Beschreibe Verhalten knapp und nutzerorientiert; übernimm die im Handbuch
etablierte Begrifflichkeit (deutsche UI-Begriffe aus `pkg/i18n/i18n.go`,
im MANUAL die englischen Werte derselben Schlüssel).

## 4. Sprach-Parität und README-Verweise

- HANDBUCH.md und MANUAL.md müssen dieselben Abschnitte, Tabellen und
  Aussagen enthalten – nur die Sprache unterscheidet sich. Jede Änderung
  in der einen Datei braucht ihr Gegenstück in der anderen; die
  gegenseitigen Verweise im Kopf der Dateien bleiben erhalten.
- READMEs (`README.de.md`, `README.md`): der Benutzung/Usage-Abschnitt
  verweist auf das Handbuch und nennt die CLI-Kommandos in einem Satz –
  neue Kommandos dort ergänzen. Feature-Abschnitte der READMEs nur
  anfassen, wenn sie dem Code widersprechen (sie sind Kurzfassungen,
  keine Kopie des Handbuchs).
- Linkcheck: alle relativen Links in den vier Dateien müssen auf
  existierende Dateien zeigen (`docs/…`).

## 5. Abschluss

- Wenn es nichts zu ändern gab: NICHTS committen, dem Nutzer kurz melden,
  dass die Doku aktuell ist (mit dem geprüften Tag als Referenz).
- Sonst: einen einzelnen Doku-Commit erstellen (auf `main`; läuft der
  Skill in einem Git-Worktree, auf dessen Branch – dann den Nutzer darauf
  hinweisen, dass der Commit noch nach `main` übernommen werden muss)
  (Betreff-Muster: `docs: Handbuch für Release aktualisiert (<Themen>)`,
  Commit-Konventionen des Repos beachten) und dem Nutzer eine kurze
  Zusammenfassung geben: was ergänzt/korrigiert wurde und – falls
  aufgefallen – welche Lücken schon länger bestanden.
- Nichts außer Doku-Dateien anfassen; Code-Befunde (z. B. ein Flag, das
  sich falsch verhält) nur melden, nicht fixen.
