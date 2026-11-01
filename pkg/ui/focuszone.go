package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// focusZone macht einen Bereich (Mailliste bzw. Mailansicht) fokussierbar:
// Tab wechselt zwischen den Zonen (fyne.Tabbable liefert Tab als TypedKey
// statt der Standard-Fokus-Traversierung), Cursortasten gehen an onKey.
// Shift wird über desktop.Keyable (KeyDown/KeyUp der Shift-Tasten) verfolgt,
// weil fyne.KeyEvent keine Modifier trägt. Bei Fokus zeichnet der Renderer
// einen dezenten Rahmen in der Akzentfarbe.
type focusZone struct {
	widget.BaseWidget
	content fyne.CanvasObject
	border  *canvas.Rectangle
	focused bool
	shift   bool
	onKey   func(ev *fyne.KeyEvent, shift bool)
	onTab   func()
	onFocus func()
}

var (
	_ fyne.Focusable  = (*focusZone)(nil)
	_ fyne.Tabbable   = (*focusZone)(nil)
	_ desktop.Keyable = (*focusZone)(nil)
)

func newFocusZone(content fyne.CanvasObject, onKey func(*fyne.KeyEvent, bool), onTab, onFocus func()) *focusZone {
	z := &focusZone{content: content, onKey: onKey, onTab: onTab, onFocus: onFocus}
	z.border = canvas.NewRectangle(color.Transparent)
	z.border.StrokeWidth = 2
	z.ExtendBaseWidget(z)
	return z
}

func (z *focusZone) CreateRenderer() fyne.WidgetRenderer {
	return &focusZoneRenderer{z: z}
}

func (z *focusZone) FocusGained() {
	z.focused = true
	if z.onFocus != nil {
		z.onFocus()
	}
	z.Refresh()
}

func (z *focusZone) FocusLost() {
	z.focused = false
	z.shift = false // KeyUp kann bei Fokusverlust verloren gehen
	z.Refresh()
}

func (z *focusZone) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyTab {
		if z.onTab != nil {
			z.onTab()
		}
		return
	}
	if z.onKey != nil {
		z.onKey(ev, z.shift)
	}
}

func (z *focusZone) TypedRune(rune) {}

// AcceptsTab sorgt dafür, dass Tab als TypedKey ankommt statt die
// Fyne-Fokus-Traversierung auszulösen.
func (z *focusZone) AcceptsTab() bool { return true }

func (z *focusZone) KeyDown(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyShiftLeft || ev.Name == desktop.KeyShiftRight {
		z.shift = true
	}
}

func (z *focusZone) KeyUp(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyShiftLeft || ev.Name == desktop.KeyShiftRight {
		z.shift = false
	}
}

type focusZoneRenderer struct {
	z *focusZone
}

func (r *focusZoneRenderer) Layout(size fyne.Size) {
	r.z.content.Resize(size)
	r.z.content.Move(fyne.NewPos(0, 0))
	r.z.border.Resize(fyne.NewSize(size.Width-2, size.Height-2))
	r.z.border.Move(fyne.NewPos(1, 1))
}

func (r *focusZoneRenderer) MinSize() fyne.Size { return r.z.content.MinSize() }

func (r *focusZoneRenderer) Refresh() {
	if r.z.focused {
		c := theme.Color(theme.ColorNamePrimary)
		if nrgba, ok := color.NRGBAModel.Convert(c).(color.NRGBA); ok {
			nrgba.A = 0x90
			r.z.border.StrokeColor = nrgba
		} else {
			r.z.border.StrokeColor = c
		}
	} else {
		r.z.border.StrokeColor = color.Transparent
	}
	r.z.border.Refresh()
	r.z.content.Refresh()
}

func (r *focusZoneRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.z.content, r.z.border}
}

func (r *focusZoneRenderer) Destroy() {}
