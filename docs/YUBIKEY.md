# Machbarkeitsanalyse: Hardware-Token-Integration (FIDO2 / Yubikey)

> **Status-Update 2026-10-26:** Die FIDO2-SSH-Integration aus Abschnitt 1b
> ist umgesetzt (pkg/fido2: sk-Signer, CTAP2-Backend macOS/Linux via
> telesma-app/ctap, Windows-Backend via WebAuthn-API kompiliert/ungetestet;
> Einrichtung über Einstellungen→SSH oder `fido2 enroll`). Live-Test mit
> echtem Token und serverseitige sk-Key-Registrierung stehen aus
> (EXCHANGE #7). Dieses Dokument bleibt als Analyse-Grundlage erhalten.

Stand: 25. Oktober 2026, bezogen auf den Code-Stand des Clients (pure Go,
ProtonMail/go-crypto für OpenPGP, golang.org/x/crypto v0.58 für SSH).

**Vision des Nutzers:** Den Client an einem beliebigen Rechner frisch
einrichten und nur zwei Dinge brauchen – die Node-ID (Merkfaktor) und das
eingesteckte Hardware-Token (Besitzfaktor). Alle privaten Schlüssel (SSH für
den Transport, PGP für Ende-zu-Ende) leben auf der Hardware und verlassen
sie nie.

**Rahmenbedingungen (präzisiert):**

- Der Nutzer am fremden Rechner hat **möglicherweise keine Admin-Rechte und
  darf keine Software installieren**. Lösungen, die einen nachinstallierten
  Agenten oder Systemdienste voraussetzen, tragen dort nicht.
- Fokus ist **FIDO2** (OpenSSH-sk-Schlüssel) – herstellerneutral: Yubikey,
  Nitrokey 3, SoloKeys Solo 2, Token2 u. a. (Google Titan kann nur
  ecdsa-sk, kein ed25519-sk). SSH-Authentifizierung über den
  GPG-Authentication-Subkey (gpg-agent) ist out of scope.

## 1. SSH-Schlüssel im FIDO2-Token

### 1a. Über einen vorhandenen ssh-agent (Komfort-Pfad, eigener Rechner)

Der Client baut seine SSH-Verbindung an genau einer Stelle auf
(`pkg/sshtransport/transport.go`): `ssh.PublicKeys(signer)` mit einem aus
`id_ed25519` geladenen Signer (`pkg/keys/keys.go`). Die Naht lässt sich um
Agent-Unterstützung erweitern (`golang.org/x/crypto/ssh/agent` ist bereits
Teil der vorhandenen Abhängigkeit – keine neue Dependency, kein CGO):
`SSH_AUTH_SOCK` → `ssh.PublicKeysCallback(agentClient.Signers)`. Der
x/crypto-Agent-Client reicht sk-Schlüssel transparent durch (der Agent
signiert) – damit funktioniert jeder im Agenten geladene Hardware-Key.

**Aber: für das Fremdrechner-Szenario ohne Admin-Rechte reicht das nicht.**

- **Windows:** Das mitgelieferte OpenSSH (Win 11 24H2: 9.5p1) kann sk-Keys
  zwar ab Werk (interner Provider über die WebAuthn-API), der Dienst
  „OpenSSH Authentication Agent" ist aber **standardmäßig deaktiviert –
  das Aktivieren erfordert einmalig Admin-Rechte**. Zudem ist das Laden
  von sk-Keys durch Fremd-Clients in den Windows-Agenten fehleranfällig,
  und `ssh-keygen -K` (Resident-Keys abrufen) verlangt dort Elevation.
- **macOS (Sonoma/Sequoia):** Apples OpenSSH-Build kommt **ohne**
  Security-Key-Builtin – `ed25519-sk` scheitert ab Werk. Apples Build lädt
  zwar eine externe Provider-Bibliothek (`SSH_SK_PROVIDER` →
  `sk-libfido2.dylib`), die man als einzelne Datei ohne Admin-Rechte
  mitbringen könnte, aber das ist bereits „Software mitbringen light".
- **Linux-Desktop:** einziger Fall, in dem der Bordmittel-Weg trägt –
  openssh-client (Ubuntu 22.04+/Fedora) ist mit libfido2 gebaut, sk-Keys
  funktionieren ab Werk, Session-Agent meist vorhanden.

Fazit: Agent-Unterstützung lohnt als kleiner Komfort-Schritt für den
eigenen, eingerichteten Rechner – das Fremdrechner-Ziel erreicht sie nicht.

### 1b. FIDO2 direkt aus dem Client (empfohlener Weg)

Neubewertung gegenüber der ersten Fassung dieses Dokuments: **machbar, ohne
Admin-Rechte, ohne Installation, auf allen drei Plattformen** – weil das
portable Client-Binary selbst mit dem Token spricht und auf Windows die
system-eigene API nutzt.

**Was x/crypto heute kann und was fehlt:**

- sk-Schlüssel-*Verifikation* und Agent-Pass-through sind vorhanden;
  **natives Signieren mit sk-Keys fehlt** (kein Parser/Signer für
  sk-Stubs). Das offizielle Proposal golang/go **#69904** bestätigt die
  Richtung: x/crypto wird FIDO2 bewusst nicht selbst implementieren,
  sondern einen Hook (`UnsupportedSecurityKeyError`) anbieten, an den
  Aufrufer eigenes FIDO-Signing anflanschen.
- Schon heute sauber möglich: `ssh.Signer` ist ein Interface – ein
  **eigener sk-Signer** (eigenes `PublicKey()`/`Sign()`) lässt sich direkt
  in `ssh.PublicKeys(...)` stecken, ohne auf x/crypto zu warten.
- **Signaturformat ist simpel** (OpenSSH PROTOCOL.u2f): signiert wird
  `SHA256(application) || flags || counter || SHA256(session-daten)` mit
  application = `ssh:…`; der SSH-Signatur-Blob enthält Signatur + flags +
  counter. Das entspricht exakt dem, was CTAP2 `getAssertion` liefert
  (rpId `ssh:`, clientDataHash = SHA256 der Session-Daten) – das Mapping
  im eigenen Signer ist trivial.

**Plattform-Matrix „ohne Admin, ohne Installation":**

| Plattform | Token-Zugriff aus dem portablen Binary | Status |
|---|---|---|
| Linux (Desktop) | direkt `/dev/hidraw`: systemd ≥ 244 liefert FIDO-udev-Regeln ab Werk (uaccess für den angemeldeten Seat-Nutzer) – Ubuntu/Fedora/Debian ohne Zusatzschritte; nur Headless-/SSH-Sessions bräuchten Admin | ✅ |
| macOS | direkt IOHIDManager: normaler Nutzerprozess, auch unsigniertes Binary, kein Root, kein TCC-Prompt | ✅ |
| Windows | raw-HID auf FIDO-Geräte ist seit Win10 1903 für Nicht-Admin-Prozesse gesperrt → stattdessen **WebAuthn-API (webauthn.dll)**, von jedem normalen Prozess ohne Admin/Signatur nutzbar; RP-ID `ssh:` funktioniert (Win32-OpenSSH macht es selbst so). System zeigt PIN-/Touch-UI an | ✅ (via WebAuthn-API) |

Windows-Einschränkungen: kein `no-touch-required` (brauchen wir nicht),
und Resident-Keys lassen sich nicht per credentialManagement aufzählen –
die Auswahl läuft über die Windows-System-UI (getAssertion mit leerer
AllowList).

**Go-Bibliotheken:**

- Pure-Go-CTAP2 existiert: stärkster Kandidat **telesma-app/ctap**
  (CTAP 2.0–2.3, PIN/UV-Auth-Protokoll 1+2, getAssertion,
  credentialManagement, eigener CGO-freier HID-Stack; sehr aktiv
  entwickelt). Risiko: pre-1.0, geringe Verbreitung – API-Brüche möglich;
  Alternative wäre eine schlanke Eigenimplementierung (CTAPHID +
  getAssertion + clientPin + credMgmt ist überschaubar).
- Windows-Seite: **telesma-app/winhello** (CGO-freie webauthn.dll-Bindings)
  oder Teleports produktionserprobtes `webauthnwin` als Referenz.
- **keys-pub/go-libfido2** scheidet aus: CGO + installiertes libfido2
  nötig, Projekt faktisch unbetreut – doppelt disqualifiziert für den
  No-Install-Anspruch.

**Resident vs. Non-Resident – entscheidend fürs Szenario:**

- Für „neue Maschine mit nichts als Token + PIN" sind
  **Resident/Discoverable Credentials Pflicht**: der Key-Handle liegt im
  Token (`ssh-keygen -t ed25519-sk -O resident`, setzt eine FIDO2-PIN
  voraus). Abruf auf neuer Maschine = credentialManagement-Enumeration
  (PIN-Eingabe nötig) – genau das, was `ssh-add -K` tut und was der Client
  auf Linux/macOS selbst implementieren kann; auf Windows übernimmt die
  System-UI die Auswahl.
- Non-Resident-Keys erzeugen einen Stub (`id_ed25519_sk`): Public Key +
  Key-Handle, **kein privates Material** – aber die Datei muss mitgeführt
  werden; ohne sie ist der Key nicht nutzbar. Fürs Szenario ungeeignet.
- `ed25519-sk` erfordert echtes FIDO2/CTAP2 (U2F-only-Token können nur
  ecdsa-sk); Resident-Keys ebenfalls. Yubikey ab Firmware 5.2.3,
  Nitrokey 3, Solo 2, Token2 sind geeignet.

**Zielarchitektur (für eine spätere Umsetzung, jetzt kein Code):**
ein sk-`ssh.Signer` im Client mit drei Backends –

1. **Agent-Pass-through** (eigener Rechner, Komfort),
2. **natives CTAP2 über USB-HID** (Linux + macOS),
3. **webauthn.dll** (Windows).

UI-Bedarf: PIN-Dialog + „Token berühren"-Hinweis (auf Windows übernimmt
das die System-UI); Erststart-Wizard um „FIDO2-Token verwenden" ergänzen,
inkl. Erzeugen eines Resident-Keys und Anzeige des Public Keys für die
Node-Registrierung. Einzige verbleibende Lücke: Resident-Enumeration auf
Windows nur über die System-UI.

**Serverseite:** unverändert. sk-Public-Keys stehen in normalen
`authorized_keys` (OpenSSH ≥ 8.2, bei uucp.uucpmail.de gegeben). sshd
verlangt per Default das User-Presence-Flag (Touch) – unser Signer muss
das flags-Byte aus getAssertion korrekt in die SSH-Signatur übernehmen,
sonst lehnt der Server ab. Optional könnte der Node-Eintrag
`verify-required` erzwingen (PIN bei jeder Nutzung).

## 2. PGP-Privatschlüssel auf dem Yubikey (OpenPGP-Smartcard)

Heute hält der Client das private Schlüsselmaterial in-process
(ProtonMail/go-crypto, `secring.asc`, Passphrase-Cache). Es existiert kein
austauschbares Signer-Interface – alle Pfade sind auf `*openpgp.Entity`
typisiert:

- `pkg/pgp/seal.go` `Signer(identity) (*Entity, error)` – der konkrete
  Rückgabetyp erzwingt In-Process-Schlüssel.
- `pkg/pgp/crypto.go` `SignDetached` (ArmoredDetachSignText), `Decrypt`,
  `DecryptionCandidates` – arbeiten auf entsperrten Entities.
- `pkg/ui/pgpdialogs.go` – Passphrase-Dialog + `pgp.Unlock` + Cache;
  eine Karte braucht stattdessen PIN-Eingabe und Touch-Hinweis.
- `pkg/pgp/keyring.go` `Save`/`ListPrivate` – gehen von einem
  serialisierbaren Secring aus; ein Karten-Key hat keinen exportierbaren
  privaten Teil (bräuchte „Stub-Entities" wie GnuPGs card-pointer).

**Realistischer Integrationsweg** (ohne dauerhaft externes GnuPG):
go-crypto unterstützt externe Signer: `packet.NewSignerPrivateKey(time,
crypto.Signer)` bzw. `NewDecrypterPrivateKey(…, crypto.Decrypter)`. Man
implementiert einen `crypto.Signer`/`crypto.Decrypter`, der per PC/SC
(z. B. `ebfe/scard`) APDUs mit dem OpenPGP-Card-Applet spricht
(PIN-Verify, PSO:SIGN / PSO:DECIPHER). Damit bleiben Signieren UND
Entschlüsseln auf der Karte.

Nötige Umbauten: Signer-Interface in seal.go, Karten-Zweig in
SignDetached/Decrypt/DecryptionCandidates, PIN/Touch-UI statt
Passphrase-Dialog, Stub-Einträge im Keyring (Fingerprint + Public-Half,
Marker „auf Karte"), Fehlerbehandlung (Karte gezogen, PIN-Zähler).

**Schlüssel-Logistik (Nutzerentscheidung eingearbeitet):** Der Transfer
eines vorhandenen Schlüssels per GnuPG `keytocard` ist als **einmaliger
Einrichtungsschritt akzeptiert** (Einbahnstraße – der private Teil wird
lokal gelöscht; vorher Backup, z. B. über die vorhandene verschlüsselte
Identitätssicherung). Alternativ Erzeugung direkt auf der Karte (privater
Teil existiert nie außerhalb, dafür kein Backup möglich).

**Wichtig – Identitäten (UIDs) nachträglich ergänzen:** Eine neue Identität
am Schlüssel ist eine neue UID mit einer Zertifizierungs-Selbstsignatur
durch den **Primärschlüssel**. Liegt der Primärschlüssel auf der Karte,
muss diese Signatur AUF der Karte erfolgen – GnuPG kann das
(`gpg --edit-key` + `adduid` mit gesteckter Karte); unser Client erst,
wenn der Karten-Signer (s. o.) existiert. Zwei gangbare Modelle:

1. **Klassisches Setup:** Primärschlüssel bleibt offline (Backup), nur die
   Subkeys (Sign/Encrypt/Auth) wandern auf die Karte. UID-Pflege geht dann
   jederzeit ohne Karte am Backup-Schlüssel; die Karte bleibt fürs
   tägliche Signieren/Entschlüsseln. Empfohlen für den Ease-of-Use-Wunsch.
2. Alles auf der Karte: UID-Pflege nur mit Karte + GnuPG-Schritt (oder
   später im Client mit Karten-Signer).

Randnotiz unabhängig von der Karte: der Client erzeugt UIDs heute nur beim
Keygen (`pgp.Generate(name, emails)`); „bestehenden Schlüssel um eine
Identität ergänzen" ist auch für Software-Schlüssel ein noch offenes
Client-Feature und sollte mit der Karten-Arbeit zusammen gedacht werden.

**Aufwand: groß** (neue PC/SC-Abhängigkeit, Kernrefactoring der
PGP-Schicht, drei Plattformen testen). **Einstufung: V2+.**

## 3. Bewertung des Gesamtszenarios „Node-ID als einziger Merkfaktor"

Mit dem FIDO2-Direktzugriff (1b) ist das Kernszenario auf allen drei
Plattformen ohne Admin-Rechte und ohne Installation erreichbar: portables
Client-Binary starten (z. B. vom USB-Stick), Node-ID eintragen, Token
stecken, FIDO2-PIN eingeben, Touch – Mail-Abruf und -Versand laufen, ohne
dass je ein privater Schlüssel auf dem Rechner liegt. PGP-auf-Karte
(Stufe 2) ergänzt das um Entschlüsseln/Signieren unterwegs.

Verbleibende Punkte, die das Szenario relativieren:

- **known_hosts/TOFU:** Auf einem frischen Rechner muss der Server-Hostkey
  einmal bestätigt werden – der Nutzer sollte den Fingerprint kennen
  (oder wir publizieren ihn auf uucpmail.de).
- **Lokale Artefakte:** Mail-Store, Entwürfe, Adressbuch und Config
  entstehen lokal und bleiben nach der Nutzung zurück. Für „fremde
  Rechner" wäre ein portabler Modus (alles in einem Verzeichnis neben dem
  Binary, ggf. kombiniert mit der verschlüsselten Sicherung) der sinnvolle
  Begleiter.
- **Identitäten:** inzwischen gelöst – der Server stellt `uuctl
  GET addresses` bereit (EXCHANGE 2026-10-25); der Client kann die
  Identitätenliste nach dem ersten Sync automatisch übernehmen
  (Client-Umsetzung steht noch aus).
- **Windows-Detail:** Auswahl von Resident-Keys läuft über die
  Windows-System-UI statt über eine eigene Liste – funktional ok,
  optisch ein Bruch.

## Empfehlung

1. **Nächster Implementierungsschritt (wenn freigegeben):** sk-Signer mit
   drei Backends – Agent-Pass-through, natives CTAP2 (Linux/macOS,
   pure Go), WebAuthn-API (Windows). FIDO2-first, herstellerneutral,
   erfüllt die No-Admin-/No-Install-Anforderung; dazu Wizard-Option
   „FIDO2-Token verwenden" (Resident-Key erzeugen/abrufen) und
   `GET addresses`-Auswertung für die Identitätenliste.
2. **Später (V2+):** OpenPGP-Smartcard über crypto.Signer/Decrypter +
   PC/SC; empfohlenes Schlüsselmodell: Primärschlüssel offline, Subkeys
   auf die Karte (keytocard akzeptiert, UID-Pflege bleibt ohne Karte
   möglich). Zusammen mit portablem Modus und dem Feature „Schlüssel um
   Identitäten ergänzen".

## Quellen (Auswahl)

- golang/go #69904 (x/crypto-Proposal: FIDO2-Hook statt Eigenimplementierung)
- OpenSSH PROTOCOL.u2f (sk-Signaturformat), ssh-keygen(1)/sshd(8)/
  sshd_config(5) (resident, -K, no-touch-required/verify-required),
  OpenSSH 8.2 Release Notes
- Microsoft WebAuthn-API (webauthn.dll); Win32-OpenSSH #1915/#1961/#2062
  (interner SK-Provider, Agent-Verhalten); libfido2 PR #994 (RP-ID `ssh:`)
- systemd rules.d/60-fido-id.rules + uaccess (Linux-HID ohne Admin)
- telesma-app/ctap + winhello (pure-Go CTAP2/WebAuthn),
  gravitational/teleport lib/auth/webauthnwin (Referenz),
  keys-pub/go-libfido2 (CGO, verworfen)
- Yubico: Securing SSH with FIDO2; Apple-Dev-Forum 698683 (macOS-OpenSSH
  ohne Security-Key-Builtin)
