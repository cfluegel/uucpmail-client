package ui

import (
	"bytes"
	_ "embed"
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

//go:embed icons/spiderweb.svg
var spiderWebSVG []byte

// strokeIcon ist das Gegenstück zu theme.ThemedResource für Strich-Icons
// (z. B. das Spinnennetz): Fyne färbt bei Theme-Icons nur fill um, Linien-Icons blieben
// sonst im Dunkelmodus schwarz. Content ersetzt "currentColor" durch die
// aktuelle Vordergrundfarbe des Themes.
type strokeIcon struct {
	name  string
	svg   []byte
	color fyne.ThemeColorName // leer = Vordergrund
}

var _ fyne.ThemedResource = (*strokeIcon)(nil)

func (r *strokeIcon) Name() string { return string(r.ThemeColorName()) + "_" + r.name }

func (r *strokeIcon) ThemeColorName() fyne.ThemeColorName {
	if r.color != "" {
		return r.color
	}
	return theme.ColorNameForeground
}

func (r *strokeIcon) Content() []byte {
	c := color.NRGBAModel.Convert(theme.Color(r.ThemeColorName())).(color.NRGBA)
	hex := fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	return bytes.ReplaceAll(r.svg, []byte("currentColor"), []byte(hex))
}

// wotIcon ist das Spinnennetz-Symbol des Web-of-Trust-Knopfs.
var wotIcon fyne.Resource = &strokeIcon{name: "wot-spiderweb.svg", svg: spiderWebSVG}

// wotIconDisabled ist die ausgegraute Variante: Fyne graut deaktivierte
// Knöpfe nur über fill aus, Strich-Icons blieben sonst voll sichtbar. Grau
// = Platzhalterfarbe, weil appTheme die Disabled-Farbe bewusst auf volle
// Textfarbe setzt (lesbarer Mailbereich, theme.go).
var wotIconDisabled fyne.Resource = &strokeIcon{name: "wot-spiderweb.svg", svg: spiderWebSVG,
	color: theme.ColorNamePlaceHolder}
