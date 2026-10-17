package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// appTheme erzwingt wahlweise die Hell-/Dunkel-Variante und skaliert alle
// Theme-Größen (Schriftgröße/Zoom). variant "" = Systemeinstellung.
type appTheme struct {
	variant string  // "", "light", "dark"
	scale   float32 // 1.0 = 100 %
}

// Warme, blendfreie Palette für die helle Variante (Papier-Ton statt Weiß).
var warmLight = map[fyne.ThemeColorName]color.Color{
	theme.ColorNameBackground:        color.NRGBA{R: 0xF3, G: 0xF0, B: 0xE9, A: 0xFF}, // warmes Papier
	theme.ColorNameOverlayBackground: color.NRGBA{R: 0xF3, G: 0xF0, B: 0xE9, A: 0xFF},
	theme.ColorNameMenuBackground:    color.NRGBA{R: 0xEF, G: 0xEB, B: 0xE2, A: 0xFF},
	theme.ColorNameInputBackground:   color.NRGBA{R: 0xFA, G: 0xF8, B: 0xF2, A: 0xFF},
	theme.ColorNameHeaderBackground:  color.NRGBA{R: 0xEA, G: 0xE5, B: 0xDB, A: 0xFF},
	theme.ColorNameHover:             color.NRGBA{R: 0xE7, G: 0xE1, B: 0xD4, A: 0xFF},
	theme.ColorNameSeparator:         color.NRGBA{R: 0xDC, G: 0xD5, B: 0xC6, A: 0xFF},
	theme.ColorNameInputBorder:       color.NRGBA{R: 0xC9, G: 0xC1, B: 0xB0, A: 0xFF},
	theme.ColorNameForeground:        color.NRGBA{R: 0x33, G: 0x2E, B: 0x27, A: 0xFF}, // warmes Dunkelbraun
	theme.ColorNamePlaceHolder:       color.NRGBA{R: 0x8A, G: 0x82, B: 0x74, A: 0xFF},
	theme.ColorNamePrimary:           color.NRGBA{R: 0x3A, G: 0x6E, B: 0x8F, A: 0xFF}, // ruhiges Taubenblau
	theme.ColorNameSelection:         color.NRGBA{R: 0xD4, G: 0xE0, B: 0xE8, A: 0xFF},
	theme.ColorNameButton:            color.NRGBA{R: 0xEC, G: 0xE7, B: 0xDD, A: 0xFF},
	theme.ColorNameScrollBar:         color.NRGBA{R: 0xB5, G: 0xAD, B: 0x9E, A: 0x99},
	theme.ColorNameShadow:            color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x20},
}

func (t *appTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch t.variant {
	case "dark":
		variant = theme.VariantDark
	case "light":
		variant = theme.VariantLight
	}
	// Der Mail-Lesebereich ist ein deaktiviertes Entry – die Disabled-Farbe
	// muss daher voll lesbar sein (dieser Client nutzt sonst keine
	// deaktivierten Widgets mit Text).
	if name == theme.ColorNameDisabled {
		if variant == theme.VariantLight {
			return color.NRGBA{R: 0x33, G: 0x2E, B: 0x27, A: 0xFF}
		}
		return color.NRGBA{R: 0xE8, G: 0xE6, B: 0xE3, A: 0xFF}
	}
	if t.variant == "light" || (t.variant == "" && variant == theme.VariantLight) {
		if c, ok := warmLight[name]; ok {
			return c
		}
	}
	return theme.DefaultTheme().Color(name, variant)
}

func (t *appTheme) Font(style fyne.TextStyle) fyne.Resource { return theme.DefaultTheme().Font(style) }
func (t *appTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t *appTheme) Size(name fyne.ThemeSizeName) float32 {
	s := theme.DefaultTheme().Size(name)
	if t.scale > 0 {
		return s * t.scale
	}
	return s
}

// applyTheme setzt Farbschema und Schriftgrößen-Faktor aus der Config um.
func (u *UI) applyTheme() {
	scale := float32(1)
	if u.cfg.UIScale > 0 {
		scale = float32(u.cfg.UIScale)
	}
	u.app.Settings().SetTheme(&appTheme{variant: u.cfg.Theme, scale: scale})
}
