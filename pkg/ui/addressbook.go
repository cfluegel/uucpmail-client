package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
)

// appendRecipient fügt email an eine Komma-Liste an. Duplikate (case-
// insensitive, bereits in DIESEM Feld) lassen die Liste unverändert;
// Trailing-Separatoren werden vorher bereinigt.
func appendRecipient(current, email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return current
	}
	trimmed := strings.TrimRight(current, " ,;\t")
	if strings.TrimSpace(trimmed) == "" {
		return email
	}
	for _, existing := range splitRecipients(trimmed) {
		if strings.EqualFold(strings.TrimSpace(existing), email) {
			return current
		}
	}
	return trimmed + ", " + email
}

// showAddressPicker öffnet das Adressbuch-Auswahlfenster (Outlook-Stil)
// über dem Compose-Fenster. An/CC/BCC fügen die gewählte Adresse in das
// jeweilige Feld ein; der Dialog bleibt für Mehrfachauswahl offen.
func (u *UI) showAddressPicker(win fyne.Window, to, cc, bcc *widget.Entry, revealBcc func()) {
	items := u.contacts.List()
	selected := -1

	var list *widget.List
	list = widget.NewList(
		func() int { return len(items) },
		func() fyne.CanvasObject { return contactRowTemplate() },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < len(items) {
				fillContactRow(o, items[i])
			}
		},
	)
	list.OnSelected = func(i widget.ListItemID) { selected = i }

	reload := func() {
		items = u.contacts.List()
		selected = -1
		list.UnselectAll()
		list.Refresh()
	}

	addTo := func(entry *widget.Entry) {
		if selected >= 0 && selected < len(items) {
			entry.SetText(appendRecipient(entry.Text, items[selected].Email))
		}
	}
	toBtn := widget.NewButton(i18n.T("ab.addto"), func() { addTo(to) })
	ccBtn := widget.NewButton(i18n.T("ab.addcc"), func() { addTo(cc) })
	bccBtn := widget.NewButton(i18n.T("ab.addbcc"), func() {
		if selected >= 0 && selected < len(items) {
			revealBcc()
			addTo(bcc)
		}
	})
	newBtn := widget.NewButton(i18n.T("ab.new"), func() {
		u.showContactEditDialog(win, nil, reload)
	})

	var emptyHint fyne.CanvasObject = widget.NewLabel("")
	if len(items) == 0 {
		emptyHint = widget.NewLabel(i18n.T("contacts.empty"))
	}
	top := container.NewVBox(contactHeader(), widget.NewSeparator(), emptyHint)
	buttons := container.NewHBox(toBtn, ccBtn, bccBtn, widget.NewSeparator(), newBtn)
	content := container.NewBorder(top, buttons, nil, nil, list)

	d := dialog.NewCustom(i18n.T("ab.title"), i18n.T("contacts.close"), content, win)
	d.Resize(fyne.NewSize(640, 460))
	d.Show()

}
