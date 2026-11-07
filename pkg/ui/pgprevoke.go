package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"github.com/cfluegel/uucpmail-client/pkg/pgp"
)

// showRevokeDialog erzeugt ein Widerrufszertifikat wie „gpg --gen-revoke“:
// Grund (GnuPG-Menü 0–3) und optionale Beschreibung wählen, Schlüssel
// entsperren, Zertifikat zum Kopieren/Speichern anzeigen. Der Schlüssel
// selbst bleibt gültig.
func (u *UI) showRevokeDialog(win fyne.Window, info pgp.KeyInfo) {
	intro := widget.NewLabel(i18n.T("settings.pgp.revoke.intro"))
	intro.Wrapping = fyne.TextWrapWord
	var reasons []string
	for i := range pgp.RevocationReasons {
		reasons = append(reasons, i18n.T("settings.pgp.revoke.r"+string(rune('0'+i))))
	}
	reason := widget.NewSelect(reasons, nil)
	reason.SetSelectedIndex(0) // wie gpg: „kein Grund angegeben“
	desc := widget.NewMultiLineEntry()
	desc.SetMinRowsVisible(3)
	form := widget.NewForm(
		widget.NewFormItem(i18n.T("settings.pgp.revoke.reason"), reason),
		widget.NewFormItem(i18n.T("settings.pgp.revoke.desc"), desc),
	)
	d := dialog.NewCustomConfirm(i18n.T("settings.pgp.revoke.title"), i18n.T("settings.pgp.revoke.do"),
		i18n.T("compose.cancel"), container.NewVBox(intro, form), func(ok bool) {
			if !ok {
				return
			}
			r := pgp.RevocationReasons[0]
			if i := reason.SelectedIndex(); i >= 0 && i < len(pgp.RevocationReasons) {
				r = pgp.RevocationReasons[i]
			}
			text := desc.Text
			u.unlockForCertify(win, info, func(ent *pgp.Entity) {
				cert, err := pgp.RevocationCertificate(ent, r, text)
				if err != nil {
					u.showError(err)
					return
				}
				hint := widget.NewLabel(i18n.T("settings.pgp.revoke.saved"))
				hint.Wrapping = fyne.TextWrapWord
				showArmorDialog(win, i18n.T("settings.pgp.revoke.title"), hint, cert, info.Fingerprint+".rev")
			})
		}, win)
	d.Resize(fyne.NewSize(520, 400))
	d.Show()
}
