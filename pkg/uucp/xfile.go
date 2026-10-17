package uucp

import (
	"bufio"
	"fmt"
	"strings"
)

// XFile repräsentiert eine X.*-Execute-Datei (uux-Job).
type XFile struct {
	User    string   // U-Zeile: anfordernder User
	System  string   // U-Zeile: anforderndes System
	Files   []string // F-Zeilen: benötigte Dateien (Spool-Namen)
	Input   string   // I-Zeile: stdin-Datei des Kommandos
	Command string   // C-Zeile: auszuführendes Kommando inkl. Argumenten
}

// BuildXFile erzeugt den Dateiinhalt für einen rmail-Job:
//
//	U <user> <system>
//	F <datafile>
//	I <datafile>
//	C rmail <rcpt...>
func BuildXFile(user, system, dataFile, command string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "U %s %s\n", user, system)
	fmt.Fprintf(&sb, "F %s\n", dataFile)
	fmt.Fprintf(&sb, "I %s\n", dataFile)
	fmt.Fprintf(&sb, "C %s\n", command)
	return sb.String()
}

// ParseXFile parst eine empfangene Execute-Datei. Unbekannte Zeilentypen
// werden ignoriert (Taylor kennt weitere wie Z, N, R, …).
func ParseXFile(content string) (*XFile, error) {
	x := &XFile{}
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if len(line) < 2 || line[1] != ' ' {
			continue
		}
		rest := strings.TrimSpace(line[2:])
		switch line[0] {
		case 'U':
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				x.User = fields[0]
			}
			if len(fields) > 1 {
				x.System = fields[1]
			}
		case 'F':
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				x.Files = append(x.Files, fields[0])
			}
		case 'I':
			x.Input = rest
		case 'C':
			x.Command = rest
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if x.Command == "" {
		return nil, fmt.Errorf("execute-datei ohne C-Zeile")
	}
	return x, nil
}

// RmailRecipients liefert die Empfänger, wenn das Kommando ein rmail-Aufruf
// ist; sonst einen Fehler (einziges serverseitig erlaubtes Kommando).
func (x *XFile) RmailRecipients() ([]string, error) {
	fields := strings.Fields(x.Command)
	if len(fields) < 2 || fields[0] != "rmail" {
		return nil, fmt.Errorf("nicht unterstütztes Kommando %q (nur rmail)", x.Command)
	}
	return fields[1:], nil
}
