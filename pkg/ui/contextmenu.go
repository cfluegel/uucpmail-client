package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// contextWrap legt ein Kontextmenü (rechte Maustaste, macOS auch
// Ctrl-/Zwei-Finger-Klick) um ein beliebiges Element. menu wird erst beim
// Klick aufgerufen; nil = kein Menü.
type contextWrap struct {
	widget.BaseWidget
	content fyne.CanvasObject
	menu    func() *fyne.Menu
}

func withContextMenu(content fyne.CanvasObject, menu func() *fyne.Menu) fyne.CanvasObject {
	c := &contextWrap{content: content, menu: menu}
	c.ExtendBaseWidget(c)
	return c
}

func (c *contextWrap) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.content)
}

func (c *contextWrap) TappedSecondary(e *fyne.PointEvent) {
	m := c.menu()
	if m == nil {
		return
	}
	if cv := fyne.CurrentApp().Driver().CanvasForObject(c); cv != nil {
		widget.ShowPopUpMenuAtPosition(m, cv, e.AbsolutePosition)
	}
}
