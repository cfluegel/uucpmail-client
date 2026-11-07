// Package keyslip erzeugt eine A4-Seite mit den eigenen Schlüsseldaten als
// Streifen zum Abschneiden (Konferenz statt zentraler Key Signing Party):
// je Streifen Key-ID + Typ, Name + E-Mail und der volle Fingerprint, wie
// die üblichen Teilnehmerlisten, nur ohne Prüfkästchen.
package keyslip

import (
	"bytes"
	"math"
	"strings"

	"github.com/go-pdf/fpdf"
)

// Info sind die Angaben eines Streifens.
type Info struct {
	Name        string
	Email       string
	KeyID       string // lange Key-ID, 16 Hex (ohne 0x)
	Types       string // z. B. „Ed25519 / ECDH cv25519“
	Fingerprint string // Hex ohne Leerzeichen
}

// Fonts sind TrueType-Schriften (Unicode-fähig) für Text und Hex-Werte.
type Fonts struct {
	Regular, Bold, Mono []byte
}

// Maße in mm.
const (
	pageW, pageH = 210.0, 297.0
	margin       = 10.0
	pad          = 1.6 // Innenabstand oben/unten je Streifen
	lineH        = 4.0 // Zeilenhöhe bei 9 pt
	gap          = 5.0 // Abstand zwischen den Spalten
	inset        = 2.0 // Text-Einzug links/rechts
	fontSize     = 9.0 // Grundschrift
	smallSize    = 7.0 // Schlüsseltyp
	minSize      = 5.5 // kleinste Schrift beim Einpassen
	rowH         = 2*lineH + 2*pad
)

// PerPage ist die Zahl der Streifen auf einer A4-Seite.
var PerPage = int(math.Floor((pageH - 2*margin) / rowH))

// A4 liefert eine PDF-Seite, gefüllt mit Streifen für info. Zwischen den
// Streifen liegen gestrichelte Schnittlinien.
func A4(info Info, fonts Fonts) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(margin, margin, margin)
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetCreator("uucpmail-client", true)
	pdf.SetTitle("OpenPGP 0x"+info.KeyID, true)
	pdf.AddUTF8FontFromBytes("sans", "", fonts.Regular)
	pdf.AddUTF8FontFromBytes("sans", "B", fonts.Bold)
	pdf.AddUTF8FontFromBytes("mono", "", fonts.Mono)
	pdf.AddPage()

	keyID := "0x" + info.KeyID
	fpLines := splitFingerprint(info.Fingerprint)

	// Spaltenbreiten aus der Monospace-Breite bei Grundschrift.
	pdf.SetFont("mono", "", fontSize)
	col1 := pdf.GetStringWidth(keyID)
	col3 := pdf.GetStringWidth(fpLines[0])
	if w := pdf.GetStringWidth(fpLines[1]); w > col3 {
		col3 = w
	}
	x1 := margin + inset
	x3 := pageW - margin - inset - col3
	x2 := x1 + col1 + gap
	col2 := x3 - gap - x2

	pdf.SetDrawColor(150, 150, 150)
	pdf.SetLineWidth(0.2)
	pdf.SetDashPattern([]float64{1.5, 1.2}, 0)
	cut := func(y float64) { pdf.Line(0, y, pageW, y) }

	for i := 0; i < PerPage; i++ {
		top := margin + float64(i)*rowH
		if i == 0 {
			cut(top)
		}
		y1 := top + pad + lineH*0.75 // Grundlinie Zeile 1
		y2 := y1 + lineH             // Grundlinie Zeile 2

		pdf.SetFont("mono", "", fontSize)
		pdf.Text(x1, y1, keyID)
		fitText(pdf, "sans", "", smallSize, col1, x1, y2, info.Types)

		fitText(pdf, "sans", "B", fontSize, col2, x2, y1, info.Name)
		if info.Email != "" {
			fitText(pdf, "sans", "", fontSize, col2, x2, y2, "<"+info.Email+">")
		}

		pdf.SetFont("mono", "", fontSize)
		pdf.Text(x3, y1, fpLines[0])
		pdf.Text(x3, y2, fpLines[1])

		cut(top + rowH)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fitText schreibt s in die Breite width: erst Schrift verkleinern (bis
// minSize), dann mit „…“ kürzen.
func fitText(pdf *fpdf.Fpdf, family, style string, size, width, x, y float64, s string) {
	for ; size > minSize; size -= 0.5 {
		pdf.SetFont(family, style, size)
		if pdf.GetStringWidth(s) <= width {
			pdf.Text(x, y, s)
			return
		}
	}
	pdf.SetFont(family, style, minSize)
	r := []rune(s)
	for len(r) > 1 && pdf.GetStringWidth(string(r)+"…") > width {
		r = r[:len(r)-1]
	}
	if string(r) != s {
		s = string(r) + "…"
	}
	pdf.Text(x, y, s)
}

// splitFingerprint teilt den Fingerprint in Vierergruppen auf zwei Zeilen
// (v4: 5 + 5 Gruppen, v6: 8 + 8).
func splitFingerprint(fp string) [2]string {
	var groups []string
	for i := 0; i < len(fp); i += 4 {
		end := i + 4
		if end > len(fp) {
			end = len(fp)
		}
		groups = append(groups, fp[i:end])
	}
	half := (len(groups) + 1) / 2
	return [2]string{strings.Join(groups[:half], " "), strings.Join(groups[half:], " ")}
}
