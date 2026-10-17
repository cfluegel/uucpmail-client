package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// mailColumns hält die (per Drag veränderbaren) Spaltenbreiten der Mail-Liste.
type mailColumns struct {
	fromW float32
	toW   float32
	dateW float32
}

const (
	colUnreadW    = 24
	colMinFrom    = 80
	colMinTo      = 80
	colMinDate    = 90
	colMinSubject = 60
	colDividerW   = 8
)

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// mailRowLayout ordnet eine Listenzeile: [Punkt | Von | An | Betreff … | Datum].
// Die Datumsspalte ist rechts im sichtbaren Bereich verankert, der Betreff
// bekommt den verbleibenden Platz.
type mailRowLayout struct{ c *mailColumns }

func (l *mailRowLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var h float32
	for _, o := range objs {
		if mh := o.MinSize().Height; mh > h {
			h = mh
		}
	}
	return fyne.NewSize(colUnreadW+l.c.fromW+l.c.toW+colMinSubject+l.c.dateW, h)
}

func (l *mailRowLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) < 5 {
		return
	}
	h := size.Height
	fromW := clamp(l.c.fromW, colMinFrom, size.Width/3)
	toW := clamp(l.c.toW, colMinTo, size.Width/3)
	dateW := clamp(l.c.dateW, colMinDate, size.Width/3)
	dateX := size.Width - dateW
	toX := colUnreadW + fromW
	subjX := toX + toW
	if dateX < subjX+colMinSubject {
		dateX = subjX + colMinSubject
	}
	objs[0].Move(fyne.NewPos(0, 0))
	objs[0].Resize(fyne.NewSize(colUnreadW, h))
	objs[1].Move(fyne.NewPos(colUnreadW, 0))
	objs[1].Resize(fyne.NewSize(fromW, h))
	objs[2].Move(fyne.NewPos(toX, 0))
	objs[2].Resize(fyne.NewSize(toW, h))
	objs[3].Move(fyne.NewPos(subjX, 0))
	objs[3].Resize(fyne.NewSize(dateX-subjX, h))
	objs[4].Move(fyne.NewPos(dateX, 0))
	objs[4].Resize(fyne.NewSize(dateW, h))
}

// mailHeaderLayout ordnet die Überschriften wie mailRowLayout und legt die
// drei Trenner auf die Spaltengrenzen:
// [spacer, Von, An, Betreff, Datum, divFrom, divTo, divDate].
type mailHeaderLayout struct{ c *mailColumns }

func (l *mailHeaderLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	return (&mailRowLayout{c: l.c}).MinSize(objs[:5])
}

func (l *mailHeaderLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) < 8 {
		return
	}
	(&mailRowLayout{c: l.c}).Layout(objs[:5], size)
	h := size.Height
	toX := objs[2].Position().X   // Grenze Von/An
	subjX := objs[3].Position().X // Grenze An/Betreff
	dateX := objs[4].Position().X // Grenze Betreff/Datum
	objs[5].Move(fyne.NewPos(toX-colDividerW/2, 0))
	objs[5].Resize(fyne.NewSize(colDividerW, h))
	objs[6].Move(fyne.NewPos(subjX-colDividerW/2, 0))
	objs[6].Resize(fyne.NewSize(colDividerW, h))
	objs[7].Move(fyne.NewPos(dateX-colDividerW/2, 0))
	objs[7].Resize(fyne.NewSize(colDividerW, h))
}

// tapRow macht eine Listenzeile doppelklick-fähig (Entwurf öffnen); einfache
// Klicks laufen weiter über die List-Selektion.
type tapRow struct {
	widget.BaseWidget
	content  fyne.CanvasObject
	index    int
	onSingle func()
	onDouble func()
}

func newTapRow(content fyne.CanvasObject) *tapRow {
	r := &tapRow{content: content, index: -1}
	r.ExtendBaseWidget(r)
	return r
}

func (r *tapRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.content)
}

// MouseDown selektiert sofort – Tapped würde wegen der Doppelklick-
// Disambiguierung ~300 ms verzögern (fühlbar träge).
func (r *tapRow) MouseDown(*desktop.MouseEvent) {
	if r.onSingle != nil {
		r.onSingle()
	}
}

func (r *tapRow) MouseUp(*desktop.MouseEvent) {}

// Tapped muss implementiert bleiben, damit der Klick nicht als unbehandelt
// gilt; die Arbeit passiert bereits in MouseDown.
func (r *tapRow) Tapped(*fyne.PointEvent) {}

func (r *tapRow) DoubleTapped(*fyne.PointEvent) {
	if r.onDouble != nil {
		r.onDouble()
	}
}

// colDivider ist der greifbare Spaltentrenner in der Überschriftenzeile.
type colDivider struct {
	widget.BaseWidget
	onDrag    func(dx float32)
	onDragEnd func()
}

func newColDivider(onDrag func(dx float32), onDragEnd func()) *colDivider {
	d := &colDivider{onDrag: onDrag, onDragEnd: onDragEnd}
	d.ExtendBaseWidget(d)
	return d
}

func (d *colDivider) CreateRenderer() fyne.WidgetRenderer {
	line := canvas.NewRectangle(theme.Color(theme.ColorNameSeparator))
	return &colDividerRenderer{d: d, line: line}
}

func (d *colDivider) Dragged(e *fyne.DragEvent) {
	if d.onDrag != nil {
		d.onDrag(e.Dragged.DX)
	}
}

func (d *colDivider) DragEnd() {
	if d.onDragEnd != nil {
		d.onDragEnd()
	}
}

// Cursor zeigt den horizontalen Größenänderungs-Cursor.
func (d *colDivider) Cursor() desktop.Cursor { return desktop.HResizeCursor }

type colDividerRenderer struct {
	d    *colDivider
	line *canvas.Rectangle
}

func (r *colDividerRenderer) Layout(size fyne.Size) {
	// Schmale sichtbare Linie mittig in der (breiteren) Griff-Fläche.
	r.line.Move(fyne.NewPos(size.Width/2-1, 2))
	r.line.Resize(fyne.NewSize(2, size.Height-4))
}
func (r *colDividerRenderer) MinSize() fyne.Size { return fyne.NewSize(colDividerW, 8) }
func (r *colDividerRenderer) Refresh() {
	r.line.FillColor = theme.Color(theme.ColorNameSeparator)
	r.line.Refresh()
}
func (r *colDividerRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.line} }
func (r *colDividerRenderer) Destroy()                     {}
