# Sicherheitsaudit 2026-10-30

Tools: gosec 2.30.0, govulncheck (vuln-DB Stand 2026-10-30),
shellcheck 0.11. Umfang: alle Go-Pakete (`./...`, 55 Dateien) und
`test/it/run-it.sh`. Ergebnis: **keine kritische erreichbare Lücke**;
Behebungen in den Commits audit-M1…M3.

## govulncheck

- **GO-2026-6866 / GO-2026-6860** (golang.org/x/net/html, Fix 0.62.0):
  Das html-Paket parst in `pkg/ui/htmlview.go` nicht vertrauenswürdige
  Mail-Inhalte. Beide Lücken liegen laut Symbolanalyse im – von uns nicht
  genutzten – Render-Pfad („0 vulnerabilities affecting your code"),
  das Update auf v0.62.0 wurde trotzdem durchgeführt (audit-M1):
  bei einem Parser für Fremddaten ist Versionshygiene Pflicht.
- **GO-2026-5932** (golang.org/x/crypto/openpgp „unmaintained"): nur
  Modul-Ebene. Der Client importiert dieses Paket nicht – OpenPGP läuft
  vollständig über ProtonMail/go-crypto. Keine Aktion; bleibt als
  Modul-Hinweis sichtbar, solange x/crypto (für ssh) benötigt wird.

## gosec (47 Funde, Triage)

| Regel | Befund | Bewertung / Maßnahme |
|---|---|---|
| G115 (HIGH) pkg/pgp/keyring.go | `uint32(lifetime/Second)` konnte bei Nutzereingaben > ~136 Jahren überlaufen → falsches Ablaufdatum | **Echter Bug, behoben:** Clamp auf MaxUint32 + CLI-Validierung `-years` 0–100 + Unit-Test (audit-M2) |
| G115 (HIGH) pkg/uucp/prott.go | `uint32(n)` aus `Read` | n ist durch len(buf)=512 begrenzt; expliziter Guard ergänzt + #nosec mit Begründung |
| G115 (HIGH) pkg/ui/pgptab.go | `byte(r)` in groupFpr | Eingabe ist Hex-ASCII; trotzdem auf `strings.Builder`/`WriteRune` umgestellt |
| G101 (HIGH, 2×) pkg/i18n | „hardcoded credentials" | False Positive: Übersetzungstabellen enthalten Wörter wie „Passphrase"/„PIN". #nosec an beiden Tabellen-Literalen |
| G703 (HIGH, 4×) / G304 (MEDIUM, 16×) | Pfad-Taint bei ReadFile/WriteFile | False Positives: sämtliche Pfade stammen aus den config.Dir()-Helfern oder aus Nutzer-Dateidialogen des Desktop-Clients; es gibt keinen Netzwerk-/Fremdeingabe-Pfad in Dateinamen. Regeln im CI-Lauf ausgeschlossen statt 20 Annotationen zu streuen |
| G306 (MEDIUM, 2×) pkg/keys | `.pub` mit 0644 | Beabsichtigt (öffentliche Schlüssel); #nosec mit Begründung |
| G104 (LOW, 20×) | unbehandelte Fehler | Unkritische Stellen (Log-/UI-Refresh, Cleanup); im CI-Lauf ausgeschlossen |

## shellcheck

`test/it/run-it.sh`: 0 Funde.

## Dauerhafte Verankerung

Die CI (`.github/workflows/build.yml`, test-Job) führt seit audit-M3
zusätzlich `gosec -quiet -exclude=G104,G304,G703 ./...` aus; der Lauf ist
auf diesem Stand fundfrei und schlägt bei künftigen Regressionen an.
govulncheck lief dort bereits zuvor bei jedem Push.

Nebenbefund der Meilenstein-Prüfung: `TestCreateRestoreRoundtrip` prüfte
einen 2-Byte-Klartextmarker gegen das Chiffrat und war dadurch in ~1 % der
Läufe falsch-negativ „rot" – auf hinreichend lange Marker umgestellt
(300 Läufe stabil).
