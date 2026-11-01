package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

	portableHint := widget.NewLabel(i18n.T("backup.portable.hint"))
	portableHint.Wrapping = fyne.TextWrapWord
	portableBtn := widget.NewButton(i18n.T("backup.portable.create"), func() {
		u.portableCreateFlow(win)
	})

	return container.NewVScroll(container.NewVBox(
		createHint, createBtn,
		widget.NewSeparator(),
		portableHint, portableBtn,
		widget.NewSeparator(),
		importHint, importBtn,
	))
}

// portableCreateFlow: Zielordner wählen → dort uucpmail-data anlegen und die
// Identitätsdateien (backup.Files-Whitelist + ui_state.json) hineinkopieren.
// Mail-Store und Spool werden bewusst nicht kopiert (frischer Stand).
func (u *UI) portableCreateFlow(win fyne.Window) {
	fd := dialog.NewFolderOpen(func(lu fyne.ListableURI, err error) {
		if err != nil || lu == nil {
			return
		}
		target := filepath.Join(lu.Path(), config.PortableDirName)
		if _, err := os.Stat(target); err == nil {
			u.showError(fmt.Errorf(i18n.T("backup.portable.exists"), target))
			return
		}
		if err := os.MkdirAll(target, 0o700); err != nil {
			u.showError(err)
			return
		}
		srcDir, err := config.Dir()
		if err != nil {
			u.showError(err)
			return
		}
		for _, name := range append(append([]string{}, backup.Files...), "ui_state.json") {
			if err := copyFileIfExists(filepath.Join(srcDir, name), filepath.Join(target, name)); err != nil {
				u.showError(err)
				return
			}
		}
		dialog.ShowInformation(i18n.T("backup.portable.create"),
			fmt.Sprintf(i18n.T("backup.portable.done"), target), win)
	}, win)
	fd.Show()
}

// copyFileIfExists kopiert eine Datei (0600); fehlende Quellen sind kein Fehler.
func copyFileIfExists(src, dst string) error {
	data, err := os.ReadFile(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
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
