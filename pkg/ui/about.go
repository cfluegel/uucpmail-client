package ui

import (
	"fmt"
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	syncengine "github.com/cfluegel/uucpmail-client/pkg/sync"
)

// Projektseite und daraus abgeleitete Links für den About-Dialog.
const (
	aboutRepoURL    = "https://github.com/cfluegel/uucpmail-client"
	aboutLicenseURL = aboutRepoURL + "/blob/main/LICENSE"
	aboutDepsURL    = aboutRepoURL + "/blob/main/go.mod"
)

// aboutVersion liefert die Version im Format der GitHub-Tags (v1.4.1).
func aboutVersion() string {
	return fmt.Sprintf(i18n.T("about.version"), "v"+syncengine.Version)
}

// showAbout zeigt Name, Version, Projektseite und Lizenz (Muster: GNOME-/
// Thunderbird-About). Links öffnen im Standardbrowser.
func (u *UI) showAbout() {
	link := func(text, raw string) *widget.Hyperlink {
		parsed, _ := url.Parse(raw) // Konstanten, im Test geprüft
		return widget.NewHyperlink(text, parsed)
	}
	center := func(o fyne.CanvasObject) fyne.CanvasObject { return container.NewCenter(o) }

	name := widget.NewLabel("uucpmail-Client")
	name.TextStyle.Bold = true
	name.SizeName = theme.SizeNameSubHeadingText
	desc := widget.NewLabel(i18n.T("about.desc"))
	desc.Wrapping = fyne.TextWrapWord
	desc.Alignment = fyne.TextAlignCenter
	third := widget.NewLabel(i18n.T("about.thirdparty"))
	third.Wrapping = fyne.TextWrapWord
	third.Alignment = fyne.TextAlignCenter

	content := container.NewVBox(
		center(name),
		desc,
		center(widget.NewLabel(aboutVersion())),
		center(link("github.com/cfluegel/uucpmail-client", aboutRepoURL)),
		widget.NewSeparator(),
		center(container.NewHBox(widget.NewLabel(i18n.T("about.license")),
			link("Apache License 2.0", aboutLicenseURL))),
		third,
		center(link(i18n.T("about.deps"), aboutDepsURL)),
	)
	d := dialog.NewCustom(i18n.T("about.title"), i18n.T("contacts.close"), content, u.win)
	d.Resize(fyne.NewSize(460, 380))
	d.Show()
}
