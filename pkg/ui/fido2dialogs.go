package ui

import (
	"context"
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/fido2"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"github.com/cfluegel/uucpmail-client/pkg/keys"
	"github.com/cfluegel/uucpmail-client/pkg/sshtransport"

	"golang.org/x/crypto/ssh"
)

// uiPrompter überbrückt FIDO2-Interaktion aus der Sync-Goroutine in Fyne:
// RequestPIN zeigt einen modalen Dialog (fyne.Do) und blockiert bis zur
// Eingabe; die PIN wird nie zwischengespeichert.
type uiPrompter struct{ u *UI }

func (p uiPrompter) RequestPIN(ctx context.Context) (string, error) {
	type res struct {
		pin string
		err error
	}
	ch := make(chan res, 1)
	fyne.Do(func() {
		entry := widget.NewPasswordEntry()
		var d dialog.Dialog
		submit := func() {
			d.Hide()
			ch <- res{pin: entry.Text}
		}
		entry.OnSubmitted = func(string) { submit() }
		okBtn := widget.NewButton(i18n.T("settings.save"), submit)
		okBtn.Importance = widget.HighImportance
		cancelBtn := widget.NewButton(i18n.T("compose.cancel"), func() {
			d.Hide()
			ch <- res{err: errors.New(i18n.T("fido2.pin.cancelled"))}
		})
		content := container.NewVBox(
			widget.NewLabel(i18n.T("fido2.pin.label")), entry,
			container.NewCenter(container.NewHBox(cancelBtn, okBtn)))
		d = dialog.NewCustomWithoutButtons(i18n.T("fido2.title"), content, p.u.win)
		d.Show()
		p.u.win.Canvas().Focus(entry)
	})
	select {
	case r := <-ch:
		return r.pin, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// NotifyTouch zeigt ein modales Popup („Security-Key berühren …") mit
// Aktivitätsbalken. Bewusst ohne Schließen-Knopf: Es spiegelt einen
// Hardware-Wartezustand und verschwindet von selbst, sobald das Token
// berührt wurde bzw. die Operation fehlschlägt oder abläuft (TouchDone
// kommt per defer aus dem Backend).
func (p uiPrompter) NotifyTouch() {
	fyne.Do(func() {
		p.u.log.append(i18n.T("fido2.touch"))
		if p.u.touchDlg != nil {
			return
		}
		msg := widget.NewLabel(i18n.T("fido2.touch"))
		msg.Alignment = fyne.TextAlignCenter
		d := dialog.NewCustomWithoutButtons(i18n.T("fido2.title"),
			container.NewVBox(msg, widget.NewProgressBarInfinite()), p.u.win)
		d.Resize(fyne.NewSize(340, 130))
		d.Show()
		p.u.touchDlg = d
	})
}

func (p uiPrompter) TouchDone() {
	fyne.Do(func() {
		if p.u.touchDlg != nil {
			p.u.touchDlg.Hide()
			p.u.touchDlg = nil
		}
	})
}

// installSignerSource verdrahtet die SSH-Signer-Quelle gemäß Config.
func (u *UI) installSignerSource() {
	if u.cfg.SSHKeyMode != "fido2" {
		return
	}
	stubPath, err := config.FIDO2StubPath()
	if err != nil {
		return
	}
	stub, err := fido2.LoadStub(stubPath)
	if err != nil {
		fmt.Println("fido2 stub not loadable, using file key:", err)
		return
	}
	signer, err := fido2.NewSigner(stub, uiPrompter{u: u})
	if err != nil {
		return
	}
	sshtransport.SetSignerSource(func(*config.Config) (ssh.Signer, error) {
		return signer, nil
	})
}

// showFIDO2Wizard führt durch die Einrichtung: PIN → Resident Keys suchen →
// übernehmen oder neu erzeugen → Stub speichern, Modus aktivieren,
// Registrierungszeile anzeigen. done wird nach Erfolg aufgerufen.
func (u *UI) showFIDO2Wizard(win fyne.Window, done func()) {
	backend, err := fido2.DefaultBackend()
	if err != nil {
		u.showError(err)
		return
	}
	prompt := uiPrompter{u: u}
	ctx := context.Background()

	finish := func(stub *fido2.Stub) {
		fyne.Do(func() {
			stubPath, err := config.FIDO2StubPath()
			if err != nil {
				u.showError(err)
				return
			}
			if err := stub.Save(stubPath); err != nil {
				u.showError(err)
				return
			}
			u.cfg.SSHKeyMode = "fido2"
			u.saveConfig()
			u.installSignerSource()
			pub, err := stub.SSHPublicKey()
			if err != nil {
				u.showError(err)
				return
			}
			line := keys.AuthorizedLine(pub, u.cfg.NodeName)
			lineLabel := widget.NewLabel(line)
			lineLabel.Wrapping = fyne.TextWrapBreak
			lineLabel.TextStyle.Monospace = true
			lineLabel.Selectable = true
			copyBtn := widget.NewButton(i18n.T("settings.key.copy"), func() {
				win.Clipboard().SetContent(line)
			})
			hint := widget.NewLabel(i18n.T("fido2.done.hint"))
			hint.Wrapping = fyne.TextWrapWord
			d := dialog.NewCustom(i18n.T("fido2.title"), i18n.T("contacts.close"),
				container.NewVBox(hint, lineLabel, copyBtn), win)
			d.Resize(fyne.NewSize(560, 300))
			d.Show()
			done()
		})
	}

	offerCreate := func() {
		fyne.Do(func() {
			u.showConfirmKeyboard(i18n.T("fido2.title"), i18n.T("fido2.create.ask"), func(ok bool) {
				if !ok {
					return
				}
				go func() {
					stub, err := backend.MakeResidentCredential(ctx, fido2.SSHApplication,
						"uucpmail-"+u.cfg.NodeName, prompt)
					if err != nil {
						fyne.Do(func() { u.showError(err) })
						return
					}
					finish(stub)
				}()
			})
		})
	}

	// Suche läuft im Hintergrund; PIN fragt der Prompter.
	go func() {
		stubs, err := backend.EnumerateSSHCredentials(ctx, fido2.SSHApplication, prompt)
		if err != nil {
			if errors.Is(err, fido2.ErrNotSupported) {
				// z. B. Windows: keine Enumeration – direkt Neuanlage anbieten.
				offerCreate()
				return
			}
			fyne.Do(func() { u.showError(err) })
			return
		}
		if len(stubs) == 0 {
			offerCreate()
			return
		}
		fyne.Do(func() {
			var labels []string
			for _, st := range stubs {
				l := st.Algorithm
				if st.Comment != "" {
					l = st.Comment + " – " + l
				}
				labels = append(labels, l)
			}
			sel := widget.NewRadioGroup(labels, nil)
			sel.SetSelected(labels[0])
			var d dialog.Dialog
			useBtn := widget.NewButton(i18n.T("fido2.use"), func() {
				for i, l := range labels {
					if l == sel.Selected {
						d.Hide()
						st := stubs[i]
						finish(&st)
						return
					}
				}
			})
			useBtn.Importance = widget.HighImportance
			newBtn := widget.NewButton(i18n.T("fido2.create.btn"), func() {
				d.Hide()
				offerCreate()
			})
			cancelBtn := widget.NewButton(i18n.T("compose.cancel"), func() { d.Hide() })
			content := container.NewVBox(
				widget.NewLabel(i18n.T("fido2.found")), sel,
				container.NewHBox(cancelBtn, newBtn, useBtn))
			d = dialog.NewCustomWithoutButtons(i18n.T("fido2.title"), content, win)
			d.Resize(fyne.NewSize(480, 300))
			d.Show()
		})
	}()
}
