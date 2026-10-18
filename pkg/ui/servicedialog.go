package ui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
)

// buildServiceStatusBox rendert den zuletzt abgefragten Dienststatus
// (gemeinsam genutzt vom Dialog und vom Einstellungs-Reiter „Tarif").
func (u *UI) buildServiceStatusBox() fyne.CanvasObject {
	st := u.cfg.ServiceStatus
	line := func(key, value string) fyne.CanvasObject {
		l := widget.NewLabel(i18n.T(key) + ": " + value)
		l.Wrapping = fyne.TextWrapWord
		return l
	}
	asOf := func(ts time.Time) string {
		if ts.IsZero() {
			return i18n.T("tarif.never")
		}
		return fmt.Sprintf(i18n.T("tarif.asof"), ts.Local().Format("02.01.2006 15:04"))
	}

	box := container.NewVBox()
	if st.Tier == "" && st.LastQuota.IsZero() && st.LastUsage.IsZero() {
		box.Add(line("tarif.tier", i18n.T("tarif.never")))
		return box
	}
	box.Add(line("tarif.tier", st.Tier))
	if st.QuotaBytes > 0 {
		quota := humanSize(st.QuotaBytes)
		if !st.LastUsage.IsZero() {
			quota = fmt.Sprintf("%s / %s", humanSize(st.UsedBytes), quota)
		}
		box.Add(line("tarif.quota", quota))
	}
	if st.MaxMessages > 0 {
		q := fmt.Sprintf("%d", st.MaxMessages)
		if !st.LastUsage.IsZero() {
			q = fmt.Sprintf("%d / %d", st.QueuedMessages, st.MaxMessages)
		}
		box.Add(line("tarif.queue", q))
	}
	if st.MaxAddresses > 0 {
		ids := fmt.Sprintf("%d", st.MaxAddresses)
		if !st.LastUsage.IsZero() {
			ids = fmt.Sprintf("%d / %d", st.UsedAddresses, st.MaxAddresses)
		}
		box.Add(line("tarif.ids", ids))
	}
	if st.RetentionDays > 0 {
		box.Add(line("tarif.retention", fmt.Sprintf("%d %s", st.RetentionDays, i18n.T("tarif.days"))))
	}
	if st.MaxMessageBytes > 0 {
		box.Add(line("tarif.maxsize", humanSize(st.MaxMessageBytes)))
	}
	box.Add(line("tarif.quota.asof", asOf(st.LastQuota)))
	box.Add(line("tarif.usage.asof", asOf(st.LastUsage)))
	return box
}

// buildTarifTab: Einstellungs-Reiter „Tarif" – Statusanzeige + Abfrage-Buttons.
func (u *UI) buildTarifTab(win fyne.Window) fyne.CanvasObject {
	request := func(req string) {
		if err := u.engine.EnqueueControl(req); err != nil {
			u.showError(err)
			return
		}
		dialog.ShowInformation(i18n.T("service.title"), i18n.T("service.queued"), win)
	}
	quotaBtn := widget.NewButton(i18n.T("service.quota"), func() { request("GET quota") })
	usageBtn := widget.NewButton(i18n.T("service.usage"), func() { request("GET usage") })
	hint := widget.NewLabel(i18n.T("service.hint"))
	hint.Wrapping = fyne.TextWrapWord

	return container.NewVScroll(container.NewVBox(
		u.buildServiceStatusBox(),
		widget.NewSeparator(),
		hint,
		container.NewHBox(quotaBtn, usageBtn),
	))
}
