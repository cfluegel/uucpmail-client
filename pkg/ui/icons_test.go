package ui

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// Das Strich-Icon muss in heller UND dunkler Variante sichtbar sein
// (Fyne färbt nur fill um – ohne strokeIcon wären die Linien im
// Dunkelmodus schwarz auf dunkel).
func TestWoTIconVisibleInBothVariants(t *testing.T) {
	for _, variant := range []string{"light", "dark"} {
		a := test.NewTempApp(t)
		th := &appTheme{variant: variant, scale: 1}
		a.Settings().SetTheme(th)
		w := test.NewTempWindow(t, widget.NewIcon(wotIcon))
		w.Resize(fyne.NewSize(64, 64))
		img := w.Canvas().Capture()
		bg := color.NRGBAModel.Convert(th.Color("background", 0)).(color.NRGBA)
		fg := color.NRGBAModel.Convert(th.Color("foreground", 0)).(color.NRGBA)
		near := func(c, ref color.NRGBA) bool {
			d := func(x, y uint8) int {
				if x > y {
					return int(x - y)
				}
				return int(y - x)
			}
			return d(c.R, ref.R)+d(c.G, ref.G)+d(c.B, ref.B) < 60
		}
		strokes := 0
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				if near(c, fg) && !near(c, bg) {
					strokes++
				}
			}
		}
		if strokes < 30 {
			t.Errorf("%s: Spinnennetz-Symbol kaum sichtbar (%d Strich-Pixel)", variant, strokes)
		}
	}
}

// Deaktiviert muss das Netz grau (Platzhalterfarbe) erscheinen, nicht in der
// Vordergrundfarbe (Fyne graut Strich-Icons selbst nicht aus).
func TestWoTIconDisabledIsGrey(t *testing.T) {
	for _, variant := range []string{"light", "dark"} {
		a := test.NewTempApp(t)
		th := &appTheme{variant: variant, scale: 1}
		a.Settings().SetTheme(th)
		act := widget.NewToolbarAction(wotIconDisabled, func() {})
		act.Disable()
		w := test.NewTempWindow(t, widget.NewToolbar(act))
		w.Resize(fyne.NewSize(64, 64))
		img := w.Canvas().Capture()
		fg := color.NRGBAModel.Convert(th.Color("foreground", 0)).(color.NRGBA)
		dis := color.NRGBAModel.Convert(th.Color("placeholder", 0)).(color.NRGBA)
		dist := func(c, ref color.NRGBA) int {
			d := func(x, y uint8) int {
				if x > y {
					return int(x - y)
				}
				return int(y - x)
			}
			return d(c.R, ref.R) + d(c.G, ref.G) + d(c.B, ref.B)
		}
		nearFg, nearDis := 0, 0
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				if dist(c, fg) < 20 {
					nearFg++
				}
				if dist(c, dis) < 20 {
					nearDis++
				}
			}
		}
		if nearFg > 0 || nearDis < 20 {
			t.Errorf("%s: deaktiviertes Netz nicht grau (Vordergrund-Pixel %d, Grau-Pixel %d)", variant, nearFg, nearDis)
		}
	}
}
