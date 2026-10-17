package ui

import (
	"errors"
	"io"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/backup"
	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
)

// buildBackupTab: verschlüsselte Sicherung der Client-Identität erstellen
// bzw. auf einem anderen Client (destruktiv) importieren.
func (u *UI) buildBackupTab(win fyne.Window) fyne.CanvasObject {
	createHint := widget.NewLabel(i18n.T("backup.hint"))
	createHint.Wrapping = fyne.TextWrapWord
	createBtn := widget.NewButton(i18n.T("backup.create"), func() {
		u.backupCreateFlow(win)
	})

	importHint := widget.NewLabel(i18n.T("backup.import.hint"))
	importHint.Wrapping = fyne.TextWrapWord
	importHint.Importance = widget.DangerImportance
	importBtn := widget.NewButton(i18n.T("backup.import"), func() {
		u.backupImportFlow(win)
	})
	importBtn.Importance = widget.DangerImportance

	return container.NewVScroll(container.NewVBox(
		createHint, createBtn,
		widget.NewSeparator(),
		importHint, importBtn,
	))
}

// backupCreateFlow: Passphrase (2×) → Datei speichern.
func (u *UI) backupCreateFlow(win fyne.Window) {
	pass := widget.NewPasswordEntry()
	pass2 := widget.NewPasswordEntry()
	items := []*widget.FormItem{
		widget.NewFormItem(i18n.T("settings.pgp.gen.pass"), pass),
		widget.NewFormItem(i18n.T("settings.pgp.gen.pass2"), pass2),
	}
	d := dialog.NewForm(i18n.T("backup.create"), i18n.T("compose.save"), i18n.T("compose.cancel"),
		items, func(ok bool) {
			if !ok {
				return
			}
			if pass.Text == "" || pass.Text != pass2.Text {
				dialog.ShowError(errors.New(i18n.T("settings.pgp.gen.mismatch")), win)
				return
			}
			dir, err := config.Dir()
			if err != nil {
				u.showError(err)
				return
			}
			data, err := backup.Create(dir, pass.Text)
			if err != nil {
				u.showError(err)
				return
			}
			fs := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
				if err != nil || wc == nil {
					return
				}
				defer wc.Close()
				if _, err := wc.Write(data); err != nil {
					u.showError(err)
					return
				}
				dialog.ShowInformation(i18n.T("backup.create"), i18n.T("backup.done"), win)
			}, win)
			fs.SetFileName("uucpmail-backup.gpg")
			fs.Show()
		}, win)
	d.Resize(fyne.NewSize(460, 200))
	d.Show()
}

// backupImportFlow: deutliche Warnung → Datei wählen → Passphrase →
// wiederherstellen → Client beendet sich (Neustart lädt den neuen Zustand).
func (u *UI) backupImportFlow(win fyne.Window) {
	warn := widget.NewLabel(i18n.T("backup.import.confirm"))
	warn.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustomConfirm(i18n.T("backup.import"),
		i18n.T("backup.import.yes"), i18n.T("compose.cancel"), warn,
		func(ok bool) {
			if !ok {
				return
			}
			fd := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
				if err != nil || rc == nil {
					return
				}
				defer rc.Close()
				data, err := io.ReadAll(rc)
				if err != nil {
					u.showError(err)
					return
				}
				u.backupRestoreWithPass(win, data, "")
			}, win)
			fd.Show()
		}, win)
	d.Resize(fyne.NewSize(520, 240))
	d.Show()
}

func (u *UI) backupRestoreWithPass(win fyne.Window, data []byte, errText string) {
	pass := widget.NewPasswordEntry()
	rows := []fyne.CanvasObject{widget.NewLabel(i18n.T("backup.pass.label")), pass}
	if errText != "" {
		errLabel := widget.NewLabel(errText)
		errLabel.Importance = widget.DangerImportance
		rows = append([]fyne.CanvasObject{errLabel}, rows...)
	}
	d := dialog.NewCustomConfirm(i18n.T("backup.import"),
		i18n.T("backup.import.yes"), i18n.T("compose.cancel"),
		container.NewVBox(rows...), func(ok bool) {
			if !ok {
				return
			}
			dir, err := config.Dir()
			if err != nil {
				u.showError(err)
				return
			}
			if err := backup.Restore(dir, data, pass.Text); err != nil {
				if errors.Is(err, backup.ErrWrongPassphrase) {
					u.backupRestoreWithPass(win, data, i18n.T("pgp.err.wrongpass"))
					return
				}
				u.showError(err)
				return
			}
			info := dialog.NewInformation(i18n.T("backup.import"), i18n.T("backup.import.done"), win)
			info.SetOnClosed(func() { u.app.Quit() })
			info.Show()
		}, win)
	d.Resize(fyne.NewSize(480, 200))
	d.Show()
	win.Canvas().Focus(pass)
}
