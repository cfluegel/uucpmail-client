package uucp

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Protokoll t (Taylor prott.c), zugeschnitten auf zuverlässige 8-Bit-Kanäle:
//   - Commands: NUL-terminierter String, mit NULs auf ein Vielfaches von
//     512 Bytes aufgefüllt; der Empfänger liest 512er-Blöcke bis einer ein
//     NUL enthält.
//   - Dateidaten: Folge von Records aus 4-Byte-Big-Endian-Länge + Rohdaten;
//     Länge 0 = EOF. Taylor schreibt max. 1024 Bytes pro Record.
const (
	tCmdBlock   = 512
	tDataRecord = 1024
	// maxDataRecord begrenzt defensiv, was wir als Record-Länge akzeptieren.
	maxDataRecord = 64 * 1024
)

// WriteCommand sendet einen Protokoll-t-Command-String.
func (w *Wire) WriteCommand(s string) error {
	n := ((len(s) + 1 + tCmdBlock - 1) / tCmdBlock) * tCmdBlock
	buf := make([]byte, n)
	copy(buf, s)
	_, err := w.W.Write(buf)
	return err
}

// ReadCommand liest 512-Byte-Blöcke, bis ein Block ein NUL enthält, und
// liefert den String bis zum ersten NUL.
func (w *Wire) ReadCommand() (string, error) {
	var all []byte
	block := make([]byte, tCmdBlock)
	for {
		if _, err := io.ReadFull(w.R, block); err != nil {
			return "", err
		}
		for i, b := range block {
			if b == 0 {
				return string(append(all, block[:i]...)), nil
			}
		}
		all = append(all, block...)
		if len(all) > 16*tCmdBlock {
			return "", fmt.Errorf("protokoll t: command ohne NUL nach %d Bytes", len(all))
		}
	}
}

// SendFile streamt r als Protokoll-t-Datei (Records + EOF-Record) und liefert
// die übertragene Byte-Anzahl.
func (w *Wire) SendFile(r io.Reader) (int64, error) {
	var total int64
	buf := make([]byte, tDataRecord)
	hdr := make([]byte, 4)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			binary.BigEndian.PutUint32(hdr, uint32(n))
			if _, werr := w.W.Write(hdr); werr != nil {
				return total, werr
			}
			if _, werr := w.W.Write(buf[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return total, err
		}
	}
	binary.BigEndian.PutUint32(hdr, 0)
	_, err := w.W.Write(hdr)
	return total, err
}

// ReceiveFile liest eine Protokoll-t-Datei bis zum EOF-Record nach dst und
// liefert die empfangene Byte-Anzahl.
func (w *Wire) ReceiveFile(dst io.Writer) (int64, error) {
	var total int64
	hdr := make([]byte, 4)
	for {
		if _, err := io.ReadFull(w.R, hdr); err != nil {
			return total, err
		}
		n := binary.BigEndian.Uint32(hdr)
		if n == 0 {
			return total, nil
		}
		if n > maxDataRecord {
			return total, fmt.Errorf("protokoll t: record-länge %d überschreitet limit %d", n, maxDataRecord)
		}
		if _, err := io.CopyN(dst, w.R, int64(n)); err != nil {
			return total, err
		}
		total += int64(n)
	}
}
