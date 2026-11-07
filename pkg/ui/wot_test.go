package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"path/filepath"
	"testing"
	"time"

	"github.com/cfluegel/uucpmail-client/pkg/pgp"
)

// Je Identität und Unterzeichner nur die neueste Signatur; Reihenfolge
// eigene → gültige → ungültige → unbekannte; gezählt werden Personen.
func TestLatestCertsAndSignerCount(t *testing.T) {
	t0 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	in := []pgp.Certification{
		{UID: "a", SignerKeyID: "U1", Status: pgp.CertUnknown, Created: t0},
		{UID: "a", SignerKeyID: "V1", SignerName: "Bert", Status: pgp.CertValid, Created: t0},
		{UID: "a", SignerKeyID: "V1", SignerName: "Bert", Status: pgp.CertValid, Created: t0.AddDate(1, 0, 0)},
		{UID: "a", SignerKeyID: "X1", Status: pgp.CertInvalid, Created: t0},
		{UID: "a", SignerKeyID: "M1", SignerName: "Ich", Status: pgp.CertValid, Mine: true, Created: t0},
		{UID: "b", SignerKeyID: "V1", SignerName: "Bert", Status: pgp.CertValid, Created: t0},
	}
	out := latestCerts(in)
	if len(out) != 5 {
		t.Fatalf("Dedupe: %d Einträge, erwartet 5", len(out))
	}
	want := []string{"M1", "V1", "V1", "X1", "U1"}
	for i, c := range out {
		if c.SignerKeyID != want[i] {
			t.Fatalf("Reihenfolge %d: %s, erwartet %s", i, c.SignerKeyID, want[i])
		}
	}
	for _, c := range out {
		if c.UID == "a" && c.SignerKeyID == "V1" && !c.Created.Equal(t0.AddDate(1, 0, 0)) {
			t.Fatal("nicht die neueste Signatur behalten")
		}
	}
	if n := signerCount(out); n != 4 {
		t.Fatalf("Personen: %d, erwartet 4", n)
	}
	if n := signerCount(knownOnly(out)); n != 3 {
		t.Fatalf("bekannte Personen: %d, erwartet 3", n)
	}
}

// Die Streifen-Angaben: primäre Identität, lange Key-ID, Typen.
func TestSlipInfo(t *testing.T) {
	d := t.TempDir()
	k, err := pgp.LoadKeyring(filepath.Join(d, "p"), filepath.Join(d, "s"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := k.Generate("Erika Muster", []string{"erika@example.org", "erika@work.example"}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	si := (&UI{keyring: k}).slipInfo(info)
	if si.Name != "Erika Muster" || si.Email != info.Email || si.KeyID != info.Fingerprint[24:] ||
		si.Types != "Ed25519 / ECDH cv25519" || si.Fingerprint != info.Fingerprint {
		t.Fatalf("slipInfo: %+v", si)
	}
}

// Auf- und Zuklappen darf keine Lücken hinterlassen: die Überschriften
// liegen danach wieder direkt untereinander, die Inhaltshöhe schrumpft.
func TestFoldListNoGaps(t *testing.T) {
	test.NewTempApp(t)
	f := newFoldList()
	var secs []foldSection
	for _, k := range []string{"a", "b", "c"} {
		rows := container.NewVBox()
		for j := 0; j < 15; j++ {
			rows.Add(widget.NewLabel("row"))
		}
		secs = append(secs, foldSection{key: k, title: k, detail: rows})
	}
	f.set(secs)
	w := test.NewTempWindow(t, f.scroll)
	w.Resize(fyne.NewSize(500, 400))
	headers := func() []*widget.Button {
		var out []*widget.Button
		for _, o := range f.box.Objects {
			if b, ok := o.(*widget.Button); ok {
				out = append(out, b)
			}
		}
		return out
	}
	closedHeight := f.box.MinSize().Height
	hs := headers()
	test.Tap(hs[0])
	test.Tap(hs[1])
	if f.box.Size().Height <= 400 {
		t.Fatalf("aufgeklappt sollte der Inhalt scrollen: %v", f.box.Size().Height)
	}
	test.Tap(hs[0])
	test.Tap(hs[1])
	if got := f.box.MinSize().Height; got != closedHeight {
		t.Fatalf("Mindesthöhe nach Zuklappen %v, erwartet %v", got, closedHeight)
	}
	// Abstand zwischen den Überschriften wie im Ausgangszustand.
	if d1, d2 := hs[1].Position().Y-hs[0].Position().Y, hs[2].Position().Y-hs[1].Position().Y; d1 != d2 || d1 > 60 {
		t.Fatalf("Lücke zwischen Überschriften: %v / %v", d1, d2)
	}
	if f.box.Size().Height > 400 {
		t.Fatalf("Inhalt nach Zuklappen nicht geschrumpft: %v", f.box.Size().Height)
	}
}

// Nur unbekannte Unterzeichner bekommen ein Kontextmenü; ein Rechtsklick
// öffnet es über der Zeile.
func TestCertRowContextMenu(t *testing.T) {
	test.NewTempApp(t)
	unknown := pgp.Certification{UID: "a", SignerKeyID: "1EEB928B64FF2B4E", Status: pgp.CertUnknown}
	known := pgp.Certification{UID: "a", SignerKeyID: "AAAA", SignerName: "Bert", Status: pgp.CertValid}
	opened := false
	menu := func() *fyne.Menu {
		opened = true
		return fyne.NewMenu("", fyne.NewMenuItem("x", func() {}))
	}
	if _, ok := certRow(known, nil).(*contextWrap); ok {
		t.Fatal("bekannte Zeile ohne Menü sollte nicht umhüllt sein")
	}
	row, ok := certRow(unknown, menu).(*contextWrap)
	if !ok {
		t.Fatal("unbekannte Zeile ohne Kontextmenü")
	}
	w := test.NewTempWindow(t, row)
	w.Resize(fyne.NewSize(400, 60))
	test.TapSecondaryAt(row, fyne.NewPos(5, 5))
	if !opened || w.Canvas().Overlays().Top() == nil {
		t.Fatal("Rechtsklick öffnet kein Menü")
	}
}

// Der Signaturen-Dialog ist rundherum um eine Titelzeilenhöhe kleiner als
// das (sichtbare) Web-of-Trust-Fenster; die Titelhöhe wächst mit dem Zoom.
func TestWoTInnerSize(t *testing.T) {
	var prevT float32
	for _, scale := range []float32{1, 1.5} {
		a := test.NewTempApp(t)
		a.Settings().SetTheme(&appTheme{variant: "light", scale: scale})
		w := test.NewTempWindow(t, widget.NewLabel(""))
		w.Resize(fyne.NewSize(1200, 900))
		th := dialogTitleHeight("Web of Trust")
		if th <= dialogPadHeight || th <= prevT {
			t.Fatalf("Zoom %v: Titelhöhe %v (vorher %v)", scale, th, prevT)
		}
		prevT = th
		got := wotInnerSize(w)
		want := fyne.NewSize(wotDialogSize.Width-2*th, wotDialogSize.Height-2*th)
		if got != want {
			t.Fatalf("Zoom %v: %v, erwartet %v", scale, got, want)
		}
		// Hauptfenster kleiner als das WoT-Fenster: von dessen Größe aus rechnen.
		w.Resize(fyne.NewSize(600, 450))
		cs := w.Canvas().Size()
		if got := wotInnerSize(w); got.Width != cs.Width-2*th || got.Height != cs.Height-2*th {
			t.Fatalf("kleines Fenster: %v bei Canvas %v", got, cs)
		}
	}
}
