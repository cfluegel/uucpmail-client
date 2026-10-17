package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"github.com/cfluegel/uucpmail-client/pkg/sshtransport"
)

// sessionLog sammelt alle Log-Zeilen der laufenden Sitzung; das Protokoll-
// Fenster (Singleton) zeigt sie live an. Zugriff nur vom Fyne-Main-Thread.
type sessionLog struct {
	lines    []string
	window   fyne.Window
	logEntry *widget.Entry
}

func (l *sessionLog) append(line string) {
	stamped := time.Now().Format("15:04:05") + "  " + line
	l.lines = append(l.lines, stamped)
	if l.logEntry != nil {
		l.logEntry.SetText(strings.Join(l.lines, "\n"))
		l.logEntry.CursorRow = len(l.lines)
		l.logEntry.Refresh()
	}
}

// showLogWindow öffnet das Protokollfenster bzw. holt es in den Vordergrund.
func (u *UI) showLogWindow() {
	if u.log.window != nil {
		u.log.window.RequestFocus()
		return
	}
	win := u.app.NewWindow(i18n.T("log.title"))
	win.Resize(fyne.NewSize(640, 420))
	entry := widget.NewMultiLineEntry()
	entry.TextStyle.Monospace = true
	entry.Wrapping = fyne.TextWrapWord
	entry.SetText(strings.Join(u.log.lines, "\n"))
	entry.Disable()
	u.log.window = win
	u.log.logEntry = entry
	win.SetOnClosed(func() {
		u.log.window = nil
		u.log.logEntry = nil
	})
	win.SetContent(container.NewBorder(nil, nil, nil, nil, entry))
	win.Show()
}

// runSync fährt eine UUCP-Session im Hintergrund; Details landen im
// Sitzungsprotokoll, die Statusleiste zeigt die einzeilige Kurzfassung.
func (u *UI) runSync() {
	if !u.cfg.Complete() {
		u.showSettings(true)
		return
	}
	if u.syncRunning {
		return
	}
	u.syncRunning = true
	u.statusBar.setText(i18n.T("sync.running"))
	u.log.append(i18n.T("sync.running"))

	u.engine.Log = func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		fyne.Do(func() { u.log.append(line) })
	}

	go func() {
		res, err := u.engine.Run()
		fyne.Do(func() {
			u.syncRunning = false
			var hk *sshtransport.HostKeyChangedError
			if errors.As(err, &hk) {
				u.statusBar.setText(fmt.Sprintf(i18n.T("status.sync.fail"), time.Now().Format("15:04")))
				u.log.append(err.Error())
				u.askHostKeyChange(hk)
				return
			}
			now := time.Now().Format("15:04")
			if err == nil {
				short := fmt.Sprintf(i18n.T("status.sync.ok"),
					now, res.SentCount, res.ReceivedCount)
				if res.FailedCount > 0 {
					short += fmt.Sprintf(i18n.T("status.sync.failedpart"), res.FailedCount)
				}
				u.statusBar.setText(short)
				u.log.append(fmt.Sprintf(i18n.T("sync.done"),
					res.SentCount, res.FailedCount, res.ReceivedCount))
			} else {
				u.statusBar.setText(fmt.Sprintf(i18n.T("status.sync.fail"), now))
				u.log.append(fmt.Sprintf(i18n.T("sync.failed"), err))
			}
			u.refresh()
		})
	}()
}

// askHostKeyChange warnt bei geändertem Server-Hostkey (möglicher MITM) und
// übernimmt den neuen Schlüssel nur nach ausdrücklicher Bestätigung; danach
// wird der Sync automatisch erneut gestartet.
func (u *UI) askHostKeyChange(hk *sshtransport.HostKeyChangedError) {
	msg := fmt.Sprintf(i18n.T("hostkey.msg"), hk.Host, hk.Fingerprint)
	content := widget.NewLabel(msg)
	content.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustomConfirm(
		i18n.T("hostkey.title"),
		i18n.T("hostkey.accept"),
		i18n.T("hostkey.cancel"),
		content,
		func(accept bool) {
			if !accept {
				u.log.append(i18n.T("hostkey.kept"))
				return
			}
			khPath, err := config.KnownHostsPath()
			if err != nil {
				u.showError(err)
				return
			}
			if err := sshtransport.ForgetHost(khPath, hk.Host); err != nil {
				u.showError(err)
				return
			}
			u.log.append(fmt.Sprintf(i18n.T("hostkey.forgotten"), hk.Host))
			u.runSync()
		}, u.win)
	d.Resize(fyne.NewSize(520, 260))
	d.Show()
}
