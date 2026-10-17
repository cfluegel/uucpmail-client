# Implementierte UUCP-Wire-Teilmenge

Der Client implementiert die für uucpmail.de nötige Teilmenge von Taylor UUCP
selbst (`internal/uucp`) – kein uucico/uux auf dem Client. Transport ist eine
rohe SSH-Session (`ssh -T uucp@…`, kein PTY), serverseitig per Forced Command
mit `uucico -u <node>` verbunden.

## Initial-Handshake

Strings sind DLE-gerahmt: `\x10<string>\x00`.

```
← Shere=uucpsrv
→ S<node> -Q0 -N01       (Feature-Bits OKTAL; wir bieten nur FEATURE_SIZES=01)
← ROKN<bits> / ROKN / ROK (oder RLCK / RLOGIN / RBADSEQ / RYou are unknown to me)
← Pt                     (Protokollangebot; wir brauchen 't')
→ Ut
…Transferphase (Protokoll t)…
→ OOOOOO ×2              (Gegenseite sendet OOOOOOO ×2; tolerant gelesen)
```

Gegen Taylor UUCP 1.07 (Quellcode) verifiziert:

- Feature-Bits werden **oktal** kodiert (`-N0%o`), die Bestätigung hängt direkt
  an `ROKN` (`ROKN0%o`) – nicht `ROK -N…`. Nacktes `ROKN` = Alt-Taylor (V103):
  Size-Negotiation aktiv, Größen dann **dezimal** statt `0x`-hex
  (`HandshakeResult.V103`). Plain `ROK` = keine Größen.
- Wir bieten bewusst **nur** FEATURE_SIZES (01) an: kein RESTART (02, nicht
  implementiert – so sendet der Server nie `SY <offset>`), kein EXEC (04 –
  so kommen eingehende Jobs als X./D.-Paare statt als `E`-Commands; `E` wird
  defensiv mit `EN2` beantwortet), kein QUOTES (040).
- Terminator-Toleranz: `\r`/`\n` gelten beim Lesen wie `\0` (Taylor
  zget_uucp_cmd); ein zweites DLE mitten im String setzt die Sammlung zurück.

## Protokoll t

Für zuverlässige 8-Bit-Kanäle; keine Checksummen, keine Acks auf Datenebene.

- **Command:** NUL-terminierter String, mit NULs auf ein Vielfaches von
  512 Bytes gepolstert. Empfänger liest 512er-Blöcke bis zum NUL.
- **Dateidaten:** Records aus 4-Byte-Big-Endian-Länge + Rohbytes.
  Länge 0 = EOF. Wir senden max. 1024 Bytes/Record (wie Taylor) und
  akzeptieren empfangsseitig bis 64 KiB.

## Transferphase

Als Master (wir rufen an und senden zuerst) pro Outbox-Job zwei Dateien:

```
→ S D.<node>C<seq> D.<node>C<seq> uucpmail -C D.<node>C<seq> 0666 "" 0x<size>
← SY                     (oder SN<code> → Job bleibt liegen)
→ <Dateidaten>
← CY                     (oder CN5)
→ S D.<node>C<seq> X.<node>C<seq> …   (Execute-Datei, gleicher Ablauf)
```

Die Datendatei ist die RFC-5322-Mail mit führender Envelope-Zeile
`From <identity> <date>` (Pflicht – das Server-rmail liest den
Envelope-Sender nur daraus). Die Execute-Datei:

```
U uucpmail <node>
F D.<node>C<seq>
I D.<node>C<seq>
C rmail <rcpt…>
```

Danach Hangup-Austausch:

```
→ H
← HY → HY               (nichts mehr zu tun)
← HN                     (Server hat Arbeit → Rollentausch)
```

Nach dem Rollentausch (wir sind Slave) beantworten wir `S`-Commands mit
`SY`/`CY` und speichern die D.*/X.*-Paare in `spool/incoming/`; `X`-Commands
(Remote-Exec-Weiterleitung) lehnen wir mit `XN` ab, `R`-Requests mit `RN2`.
Der Server beendet mit `H` → `HY` → `HY`.

## Eingebautes uuxqt

Nach der Session werden vollständige X./D.-Paare verarbeitet: X-Datei parsen
(U/F/I/C-Zeilen), nur `C rmail <addr…>` ist zulässig (sonst Quarantäne in
`spool/quarantine/`), Envelope-Zeile abtrennen, Mail in die Inbox.
Unvollständige Paare bleiben bis zur nächsten Session liegen (idempotent).

## Verifikationsstand

Alle ursprünglich offenen Punkte sind geklärt – zweifach verifiziert:

1. **Taylor-UUCP-1.07-Quellcode** (uucico.c, prott.c, trans.c, send.c, rec.c,
   uux.c): Framing, `-N`-Oktalkodierung, `ROKN`-Antwort, S-Command-Grammatik
   (`S from to user -opts temp 0mode notify [size]`, Size nur bei FEATURE_SIZES,
   `0x`-hex bzw. dezimal bei V103), SN-Codes (generiert werden nur 2/4/6/8),
   Hangup (`H` → `HY` → `HY`), O-Ketten (Caller 6×O zweimal, Callee 7×O zweimal),
   Command-Padding (kleinstes Vielfaches von 512 > strlen – identisch zu unserer
   Formel), Datenrecords (4-Byte-BE, max. 1024, 0 = EOF).
2. **Live gegen uucp.uucpmail.de** (echtes Taylor-uucico hinter dem
   ssh-gateway): Handshake, Protokoll-t-Negotiation, Senden mit Size-Feld,
   Rollentausch, Empfang (inkl. 10-KB-Mail), eingebautes uuxqt, kompletter
   Mail-Roundtrip (DKIM-signiert und byte-identisch zurück).

Bewusste Abweichungen von Taylor: kein Restart (`SY <offset>` wird nie
provoziert, Offsets werden ignoriert), kein `E`-Command-Support (wird nicht
angeboten und mit `EN2` abgelehnt), Empfangsrecords bis 64 KiB akzeptiert
(Taylor-Ringpuffer: 16 KiB; gesendet wird nie >1024).
