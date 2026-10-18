package ui

import (
	"fmt"
	"math"
	"os"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/crypto/ssh"

	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"github.com/cfluegel/uucpmail-client/pkg/keys"
)

// showSettings öffnet die Einstellungen als MODALEN Dialog über dem
// Hauptfenster (blockiert es bis zum Schließen); firstRun zeigt zusätzlich
// den Einrichtungs-Hinweis. Abbrechen verwirft alle Änderungen.
func (u *UI) showSettings(firstRun bool) {
	win := u.win // Parent für alle Unterdialoge (Dateiauswahl, Keygen, …)

	// --- Tab Account ---
	nodeEntry := widget.NewEntry()
	nodeEntry.SetText(u.cfg.NodeName)
	hostEntry := widget.NewEntry()
	hostEntry.SetText(u.cfg.ServerHost)
	portEntry := widget.NewEntry()
	portEntry.SetText(strconv.Itoa(u.cfg.ServerPort))
	accountTab := widget.NewForm(
		widget.NewFormItem(i18n.T("settings.node"), nodeEntry),
		widget.NewFormItem(i18n.T("settings.host"), hostEntry),
		widget.NewFormItem(i18n.T("settings.port"), portEntry),
	)

	// --- Tab Identitäten ---
	ids := append([]string{}, u.cfg.Identities...)
	defaultID := u.cfg.DefaultIdentity
	var idList *widget.List
	selected := -1
	idList = widget.NewList(
		func() int { return len(ids) },
		func() fyne.CanvasObject { return widget.NewLabel("id") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			label := ids[i]
			if ids[i] == defaultID {
				label += "  ★"
			}
			o.(*widget.Label).SetText(label)
		},
	)
	idList.OnSelected = func(i widget.ListItemID) { selected = i }
	newID := widget.NewEntry()
	newID.SetPlaceHolder("name@uucpmail.de")
	addBtn := widget.NewButton(i18n.T("settings.id.add"), func() {
		if newID.Text == "" {
			return
		}
		if warn := config.CheckIdentity(newID.Text); warn != "" {
			dialog.ShowInformation(i18n.T("settings.ids"), warn, win)
		}
		ids = append(ids, newID.Text)
		if defaultID == "" {
			defaultID = newID.Text
		}
		newID.SetText("")
		idList.Refresh()
	})
	delBtn := widget.NewButton(i18n.T("settings.id.del"), func() {
		if selected < 0 || selected >= len(ids) {
			return
		}
		if ids[selected] == defaultID {
			defaultID = ""
		}
		ids = append(ids[:selected], ids[selected+1:]...)
		selected = -1
		idList.UnselectAll()
		idList.Refresh()
	})
	defBtn := widget.NewButton(i18n.T("settings.id.def"), func() {
		if selected >= 0 && selected < len(ids) {
			defaultID = ids[selected]
			idList.Refresh()
		}
	})
	idHint := widget.NewLabel(i18n.T("settings.ids.hint"))
	idHint.Wrapping = fyne.TextWrapWord
	idTab := container.NewBorder(
		container.NewBorder(nil, nil, nil, addBtn, newID),
		container.NewVBox(container.NewHBox(defBtn, delBtn), idHint), nil, nil, idList)

	// --- Tab Schlüssel (SSH) ---
	keyTab := u.buildKeyTab(win)

	// --- Tab PGP ---
	pgpTTL := widget.NewEntry()
	pgpTTL.SetText(strconv.Itoa(u.cfg.PGPCacheMin))
	pgpTab := u.buildPGPTab(win, pgpTTL)

	// --- Tab Darstellung (Sprache, Farbschema, Schriftgröße) & Profil ---
	langSelect := widget.NewSelect(
		[]string{i18n.T("settings.lang.auto"), "Deutsch", "English"}, nil)
	switch u.cfg.Language {
	case "de":
		langSelect.SetSelectedIndex(1)
	case "en":
		langSelect.SetSelectedIndex(2)
	default:
		langSelect.SetSelectedIndex(0)
	}
	themeSelect := widget.NewSelect(
		[]string{i18n.T("theme.system"), i18n.T("theme.light"), i18n.T("theme.dark")}, nil)
	switch u.cfg.Theme {
	case "light":
		themeSelect.SetSelectedIndex(1)
	case "dark":
		themeSelect.SetSelectedIndex(2)
	default:
		themeSelect.SetSelectedIndex(0)
	}
	fontScales := []float64{0.8, 0.9, 1.0, 1.1, 1.25}
	var fontLabels []string
	for _, f := range fontScales {
		fontLabels = append(fontLabels, fmt.Sprintf("%d %%", int(f*100+0.5)))
	}
	fontSelect := widget.NewSelect(fontLabels, nil)
	fontSelect.SetSelectedIndex(2) // 100 %
	for i, f := range fontScales {
		if u.cfg.UIScale > 0 && math.Abs(u.cfg.UIScale-f) < 0.01 {
			fontSelect.SetSelectedIndex(i)
		}
	}
	autosave := widget.NewEntry()
	autosave.SetText(strconv.Itoa(u.cfg.DraftAutosave))
	maxSize := widget.NewEntry()
	maxSize.SetText(strconv.FormatInt(u.cfg.ServiceProfile.MaxMessageBytes, 10))
	rate := widget.NewEntry()
	rate.SetText(strconv.Itoa(u.cfg.ServiceProfile.WarnMsgsPerHour))
	miscTab := widget.NewForm(
		widget.NewFormItem(i18n.T("settings.lang"), langSelect),
		widget.NewFormItem(i18n.T("settings.theme"), themeSelect),
		widget.NewFormItem(i18n.T("settings.fontsize"), fontSelect),
		widget.NewFormItem(i18n.T("settings.autosave"), autosave),
		widget.NewFormItem(i18n.T("settings.maxsize"), maxSize),
		widget.NewFormItem(i18n.T("settings.rate"), rate),
	)

	tabs := container.NewAppTabs(
		container.NewTabItem(i18n.T("settings.account"), accountTab),
		container.NewTabItem(i18n.T("settings.ids"), idTab),
		container.NewTabItem(i18n.T("settings.key"), keyTab),
		container.NewTabItem(i18n.T("settings.pgp"), pgpTab),
		container.NewTabItem(i18n.T("settings.tarif"), u.buildTarifTab(win)),
		container.NewTabItem(i18n.T("settings.display"), miscTab),
		container.NewTabItem(i18n.T("settings.backup"), u.buildBackupTab(win)),
	)

	applySave := func() {
		u.cfg.NodeName = nodeEntry.Text
		u.cfg.ServerHost = hostEntry.Text
		if p, err := strconv.Atoi(portEntry.Text); err == nil && p > 0 {
			u.cfg.ServerPort = p
		}
		u.cfg.Identities = ids
		u.cfg.DefaultIdentity = defaultID
		switch langSelect.SelectedIndex() {
		case 1:
			u.cfg.Language = "de"
		case 2:
			u.cfg.Language = "en"
		default:
			u.cfg.Language = ""
		}
		switch themeSelect.SelectedIndex() {
		case 1:
			u.cfg.Theme = "light"
		case 2:
			u.cfg.Theme = "dark"
		default:
			u.cfg.Theme = ""
		}
		if i := fontSelect.SelectedIndex(); i >= 0 && i < len(fontScales) {
			u.cfg.UIScale = fontScales[i]
		}
		if n, err := strconv.Atoi(autosave.Text); err == nil && n > 0 {
			u.cfg.DraftAutosave = n
		}
		if n, err := strconv.Atoi(pgpTTL.Text); err == nil && n >= 0 && n != u.cfg.PGPCacheMin {
			u.cfg.PGPCacheMin = n
			u.pgpCache.SetTTL(pgpCacheTTL(n))
			u.pgpCache.Clear()
		}
		if n, err := strconv.ParseInt(maxSize.Text, 10, 64); err == nil && n > 0 {
			u.cfg.ServiceProfile.MaxMessageBytes = n
		}
		if n, err := strconv.Atoi(rate.Text); err == nil && n > 0 {
			u.cfg.ServiceProfile.WarnMsgsPerHour = n
		}
		u.saveConfig()
		u.applyTheme()
	}

	var top fyne.CanvasObject
	if firstRun {
		intro := widget.NewLabel(i18n.T("wizard.intro"))
		intro.Wrapping = fyne.TextWrapWord
		top = intro
	}
	content := container.NewBorder(top, nil, nil, nil, tabs)
	d := dialog.NewCustomConfirm(i18n.T("settings.title"),
		i18n.T("settings.save"), i18n.T("compose.cancel"), content,
		func(ok bool) {
			if ok {
				applySave()
			}
		}, u.win)
	d.Resize(fyne.NewSize(700, 560))
	d.Show()
}

// serverHostKeyLines liest die gespeicherten Server-Hostkeys aus der
// known_hosts und liefert Anzeigezeilen mit SHA256-Fingerprint.
func serverHostKeyLines() []string {
	khPath, err := config.KnownHostsPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(khPath)
	if err != nil || len(data) == 0 {
		return []string{i18n.T("settings.key.noserver")}
	}
	var out []string
	rest := data
	for len(rest) > 0 {
		_, hosts, pubKey, _, next, err := ssh.ParseKnownHosts(rest)
		if err != nil {
			break
		}
		rest = next
		host := ""
		if len(hosts) > 0 {
			host = hosts[0]
		}
		out = append(out, fmt.Sprintf(i18n.T("settings.key.server"),
			host, ssh.FingerprintSHA256(pubKey)))
	}
	if len(out) == 0 {
		return []string{i18n.T("settings.key.noserver")}
	}
	return out
}

// buildKeyTab zeigt Fingerprint/Pubkey bzw. bietet die Schlüsselerzeugung an.
func (u *UI) buildKeyTab(win fyne.Window) fyne.CanvasObject {
	content := container.NewVBox()
	var render func()
	render = func() {
		content.Objects = nil
		keyPath, err := config.KeyPath()
		if err != nil {
			content.Add(widget.NewLabel(err.Error()))
			return
		}
		signer, err := keys.Load(keyPath)
		if err != nil {
			content.Add(widget.NewLabel(i18n.T("settings.key.none")))
			genBtn := widget.NewButton(i18n.T("settings.key.gen"), func() {
				if err := keys.Generate(keyPath, "uucpmail"); err != nil {
					u.showError(err)
					return
				}
				render()
				content.Refresh()
			})
			content.Add(genBtn)
			return
		}
		pubLine := keys.AuthorizedLine(signer.PublicKey(), "uucpmail")
		fp := widget.NewLabel(i18n.T("settings.key.fp") + ": " + keys.Fingerprint(signer.PublicKey()))
		fp.Wrapping = fyne.TextWrapBreak
		fp.Selectable = true
		// Read-only, aber markier-/kopierbar – kein editierbares Textfeld.
		pub := widget.NewLabel(pubLine)
		pub.Wrapping = fyne.TextWrapBreak
		pub.Selectable = true
		pub.TextStyle.Monospace = true
		copyBtn := widget.NewButton(i18n.T("settings.key.copy"), func() {
			win.Clipboard().SetContent(pubLine)
		})
		regenBtn := widget.NewButton(i18n.T("settings.key.regen"), func() {
			dialog.ShowConfirm(i18n.T("settings.key.regen"),
				i18n.T("settings.key.regen.confirm"), func(ok bool) {
					if !ok {
						return
					}
					_ = os.Remove(keyPath)
					_ = os.Remove(keyPath + ".pub")
					if err := keys.Generate(keyPath, "uucpmail"); err != nil {
						u.showError(err)
						return
					}
					render()
					content.Refresh()
				}, win)
		})
		regenBtn.Importance = widget.DangerImportance
		hint := widget.NewLabel(i18n.T("settings.key.hint"))
		hint.Wrapping = fyne.TextWrapWord
		content.Add(fp)
		content.Add(pub)
		content.Add(container.NewHBox(copyBtn, regenBtn))
		content.Add(hint)
		content.Add(widget.NewSeparator())
		// Zur Abgrenzung: der Hostkey des SERVERS aus der known_hosts.
		for _, line := range serverHostKeyLines() {
			l := widget.NewLabel(line)
			l.Wrapping = fyne.TextWrapBreak
			l.Selectable = true
			content.Add(l)
		}
	}
	render()
	return content
}
