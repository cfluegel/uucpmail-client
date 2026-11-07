package ui

import (
	"context"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"github.com/cfluegel/uucpmail-client/pkg/pgpcard"
)

func sigPINText(c pgpcard.CardInfo) string {
	if c.SigPINForced {
		return i18n.T("pgpcard.detail.sigpin.each")
	}
	return i18n.T("pgpcard.detail.sigpin.once")
}

func kdfText(c pgpcard.CardInfo) string {
	switch {
	case c.KDFActive:
		return i18n.T("pgpcard.detail.on")
	case c.KDFSupported:
		return i18n.T("pgpcard.detail.off")
	}
	return i18n.T("pgpcard.detail.unsupported")
}

// showCardUnblock setzt mit der Admin-PIN eine neue Nutzer-PIN (RESET RETRY
// COUNTER) – der Weg nach drei Fehlversuchen.
func (u *UI) showCardUnblock(win fyne.Window, info pgpcard.CardInfo) {
	intro := widget.NewLabel(fmt.Sprintf(i18n.T("pgpcard.unblock.intro"), info.Serial))
	intro.Wrapping = fyne.TextWrapWord
	admin := widget.NewPasswordEntry()
	pin1 := widget.NewPasswordEntry()
	pin2 := widget.NewPasswordEntry()
	form := widget.NewForm(
		widget.NewFormItem(i18n.T("pgpcard.unblock.admin"), admin),
		widget.NewFormItem(i18n.T("pgpcard.wizard.pin.new"), pin1),
		widget.NewFormItem(i18n.T("pgpcard.wizard.pin.new2"), pin2),
	)
	d := dialog.NewCustomConfirm(i18n.T("pgpcard.title"), i18n.T("settings.save"), i18n.T("compose.cancel"),
		container.NewVBox(intro, form), func(ok bool) {
			if !ok {
				return
			}
			if pin1.Text != pin2.Text {
				u.showError(fmt.Errorf("%s", i18n.T("pgpcard.wizard.pin.mismatch")))
				return
			}
			if len(pin1.Text) < pgpcard.MinPINLen {
				u.showError(fmt.Errorf("%s", i18n.T("pgpcard.wizard.pin.short")))
				return
			}
			adminPIN, newPIN := admin.Text, pin1.Text
			go func() {
				sess, err := pgpcard.DefaultBackend().Open(context.Background(), info.Serial)
				if err == nil {
					err = sess.ResetPIN(adminPIN, newPIN)
					_ = sess.Close()
				}
				fyne.Do(func() {
					if err != nil {
						u.showError(err)
						return
					}
					if u.pgpCardKey != nil {
						u.pgpCardKey.Cache.Remove(info.Serial) // gecachte alte PIN verwerfen
					}
					dialog.ShowInformation(i18n.T("pgpcard.title"), i18n.T("pgpcard.unblock.done"), win)
				})
			}()
		}, win)
	d.Resize(fyne.NewSize(480, 300))
	d.Show()
	win.Canvas().Focus(admin)
}

// showCardFactoryReset setzt die Karte nach doppelter Bestätigung (Serial
// eintippen + Rückfrage) in den Werkszustand. War der Client auf diese
// Karte eingerichtet, wird die Kartennutzung ausgeschaltet.
func (u *UI) showCardFactoryReset(win fyne.Window, info pgpcard.CardInfo, done func()) {
	warn := widget.NewLabel(fmt.Sprintf(i18n.T("pgpcard.reset.warn"), info.Serial))
	warn.Wrapping = fyne.TextWrapWord
	serial := widget.NewEntry()
	serial.SetPlaceHolder(i18n.T("pgpcard.reset.serial"))
	var d dialog.Dialog
	resetBtn := widget.NewButton(i18n.T("pgpcard.reset.do"), func() {
		d.Hide()
		dialog.ShowConfirm(i18n.T("pgpcard.reset.btn"), fmt.Sprintf(i18n.T("pgpcard.reset.confirm"), info.Serial),
			func(ok bool) {
				if ok {
					u.cardFactoryReset(win, info, done)
				}
			}, win)
	})
	resetBtn.Importance = widget.DangerImportance
	resetBtn.Disable()
	serial.OnChanged = func(s string) {
		if strings.EqualFold(strings.TrimSpace(s), info.Serial) {
			resetBtn.Enable()
		} else {
			resetBtn.Disable()
		}
	}
	cancelBtn := widget.NewButton(i18n.T("compose.cancel"), func() { d.Hide() })
	d = dialog.NewCustomWithoutButtons(i18n.T("pgpcard.reset.btn"),
		container.NewVBox(warn, serial, container.NewCenter(container.NewHBox(cancelBtn, resetBtn))), win)
	d.Resize(fyne.NewSize(540, 300))
	d.Show()
	win.Canvas().Focus(serial)
}

func (u *UI) cardFactoryReset(win fyne.Window, info pgpcard.CardInfo, done func()) {
	prog := progressDialog(win, i18n.T("pgpcard.reset.btn"))
	go func() {
		sess, err := pgpcard.DefaultBackend().Open(context.Background(), info.Serial)
		if err == nil {
			err = sess.FactoryReset()
			_ = sess.Close()
		}
		fyne.Do(func() {
			prog.Hide()
			if err != nil {
				u.showError(err)
				return
			}
			text := fmt.Sprintf(i18n.T("pgpcard.reset.done"), info.Serial)
			if path, perr := config.PGPCardStubPath(); perr == nil {
				if stub, serr := pgpcard.LoadStub(path); serr == nil && stub.Serial == info.Serial && u.cfg.PGPCard {
					u.cfg.PGPCard = false
					u.saveConfig()
					u.installCardSource()
					text += "\n\n" + i18n.T("pgpcard.reset.stubhint")
				}
			}
			msg := widget.NewLabel(text)
			msg.Wrapping = fyne.TextWrapWord
			dd := dialog.NewCustom(i18n.T("pgpcard.title"), i18n.T("contacts.close"), msg, win)
			dd.Resize(fyne.NewSize(500, 220))
			dd.Show()
			done()
		})
	}()
}
