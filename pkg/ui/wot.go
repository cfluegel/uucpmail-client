package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"github.com/cfluegel/uucpmail-client/pkg/keyslip"
	"github.com/cfluegel/uucpmail-client/pkg/pgp"
)

// showWoTWindow öffnet das Web-of-Trust-Fenster (Toolbar-Spinnennetz rechts neben
// dem Adressbuch): eigener Schlüssel (Zertifizierungen, Fingerprint-Zettel)
// und die öffentlichen Schlüssel anderer (Import, Zertifizieren, Löschen).
func (u *UI) showWoTWindow() {
	content := u.buildWoTView(u.win)
	d := dialog.NewCustom(i18n.T("wot.title"), i18n.T("contacts.close"), content, u.win)
	d.Resize(wotDialogSize)
	d.Show()
}

// wotDialogSize ist die gewünschte Größe des Web-of-Trust-Fensters.
var wotDialogSize = fyne.NewSize(720, 560)

// dialogPadHeight ist Fynes Abstand unter der Dialog-Titelzeile
// (dialog/base.go, padHeight).
const dialogPadHeight = 16

// dialogTitleHeight misst die Höhe einer Dialog-Titelzeile wie Fyne sie
// baut (fettes Label + Abstand). Das Label folgt Schriftgröße und Zoom
// des aktuellen Themes.
func dialogTitleHeight(title string) float32 {
	l := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return l.MinSize().Height + dialogPadHeight
}

// wotInnerSize ist die Größe für Dialoge über dem Web-of-Trust-Fenster:
// dessen sichtbare Größe (Fyne begrenzt Dialoge auf das Hauptfenster),
// rundherum um die Höhe seiner Titelzeile verkleinert – so bleibt der
// Titel „Web of Trust“ sichtbar und der Dialog sitzt in dessen Rahmen.
func wotInnerSize(win fyne.Window) fyne.Size {
	s := wotDialogSize
	if cs := win.Canvas().Size(); !cs.IsZero() {
		s = fyne.NewSize(fyne.Min(s.Width, cs.Width), fyne.Min(s.Height, cs.Height))
	}
	t := dialogTitleHeight(i18n.T("wot.title"))
	return fyne.NewSize(s.Width-2*t, s.Height-2*t)
}

// updateWoTAction gibt den Web-of-Trust-Knopf nur frei, wenn es einen
// eigenen privaten Schlüssel gibt – ohne ihn lässt sich nichts zertifizieren.
func (u *UI) updateWoTAction() {
	if u.wotAction == nil {
		return
	}
	if len(u.keyring.ListPrivate()) > 0 {
		u.wotAction.SetIcon(wotIcon)
		u.wotAction.Enable()
	} else {
		u.wotAction.SetIcon(wotIconDisabled)
		u.wotAction.Disable()
	}
}

func (u *UI) buildWoTView(win fyne.Window) fyne.CanvasObject {
	ownBox := container.NewVBox()
	othersBox := container.NewVBox()
	ownTitle := widget.NewLabel(i18n.T("wot.own"))
	ownTitle.TextStyle.Bold = true
	othersTitle := widget.NewLabel(i18n.T("settings.pgp.others"))
	othersTitle.TextStyle.Bold = true

	var render func()

	renderOwn := func() {
		ownBox.Objects = nil
		privs := u.keyring.ListPrivate()
		if len(privs) == 0 {
			l := widget.NewLabel(i18n.T("wot.own.none"))
			l.Wrapping = fyne.TextWrapWord
			ownBox.Add(l)
			return
		}
		for _, info := range privs {
			info := info
			label := widget.NewLabel(fmt.Sprintf("%s <%s>\n%s\n%s", info.Name,
				strings.Join(info.Emails, ", "), groupFpr(info.Fingerprint),
				fmt.Sprintf(i18n.T("wot.certcount"), signerCount(u.keyring.Certifications(info.Fingerprint)))))
			label.Wrapping = fyne.TextWrapWord
			sigsBtn := widget.NewButton(i18n.T("wot.sigs.btn"), func() {
				u.showCertificationsDialog(win, info, render)
			})
			sheetBtn := widget.NewButton(i18n.T("wot.sheet.btn"), func() {
				u.saveFingerprintSheet(win, info)
			})
			ownBox.Add(container.NewBorder(nil, nil, nil, container.NewHBox(sigsBtn, sheetBtn), label))
		}
	}

	renderOthers := func() {
		othersBox.Objects = nil
		pubs := u.keyring.ListPublic()
		if len(pubs) == 0 {
			othersBox.Add(widget.NewLabel(i18n.T("wot.others.none")))
		}
		for _, info := range pubs {
			info := info
			head := fmt.Sprintf("%s <%s>", info.Name, info.Email)
			if info.Verified {
				head = "✓ " + head + "  (" + i18n.T("wot.verified") + ")"
			}
			label := widget.NewLabel(fmt.Sprintf("%s\n%s\n%s", head, groupFpr(info.Fingerprint),
				fmt.Sprintf(i18n.T("wot.certcount"), signerCount(u.keyring.Certifications(info.Fingerprint)))))
			label.Wrapping = fyne.TextWrapWord
			certBtn := widget.NewButton(i18n.T("wot.certify.btn"), func() {
				u.showCertifyDialog(win, info, render)
			})
			sendBtn := widget.NewButton(i18n.T("wot.sendcert.btn"), func() {
				u.showSendCertsDialog(win, info)
			})
			if !info.Verified { // = gültige eigene Zertifizierung vorhanden
				sendBtn.Disable()
			}
			sigsBtn := widget.NewButton(i18n.T("wot.sigs.btn"), func() {
				u.showCertificationsDialog(win, info, render)
			})
			infoBtn := widget.NewButtonWithIcon("", theme.QuestionIcon(), func() {
				u.showKeyInfoDialog(win, info)
			})
			// Wie im PGP-Tab: Mülleimer, rot.
			delBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
				dialog.ShowConfirm(i18n.T("settings.pgp.delete"), i18n.T("settings.pgp.delete.confirm"),
					func(ok bool) {
						if !ok {
							return
						}
						if err := u.keyring.DeletePublic(info.Fingerprint); err != nil {
							u.showError(err)
							return
						}
						render()
					}, win)
			})
			delBtn.Importance = widget.DangerImportance
			othersBox.Add(container.NewBorder(nil, nil, nil,
				container.NewHBox(certBtn, sendBtn, sigsBtn, infoBtn, delBtn), label))
		}
		othersBox.Add(container.NewHBox(
			widget.NewButton(i18n.T("settings.pgp.import.pub"), func() {
				u.importPubKeyDialog(win, render)
			}),
			widget.NewButton(i18n.T("wot.ks.btn"), func() {
				u.showKeyserverDialog(win, render)
			}),
		))
	}

	// Ganzer Inhalt wird bei Änderungen neu befüllt (Muster PGP-Tab).
	root := container.NewVBox()
	render = func() {
		renderOwn()
		renderOthers()
		root.Objects = []fyne.CanvasObject{
			ownTitle, ownBox,
			widget.NewSeparator(),
			othersTitle, othersBox,
		}
		root.Refresh()
	}
	render()
	return container.NewVScroll(root)
}

// showCertificationsDialog zeigt die Zertifizierungen anderer: je Identität
// ein zugeklappter Eintrag mit Anzahl, aufgeklappt sortiert (eigene,
// gültige, ungültige, unbekannte). Mehrfache Signaturen derselben Person auf
// derselben Identität erscheinen nur einmal (die neueste).
// changed (darf nil sein) wird nach einem Schlüssel-Download aufgerufen,
// damit das aufrufende Fenster seine Liste auffrischt.
func (u *UI) showCertificationsDialog(win fyne.Window, info pgp.KeyInfo, changed func()) {
	uids := u.keyring.UIDs(info.Fingerprint)
	headLabel := widget.NewLabel("")
	headLabel.Wrapping = fyne.TextWrapWord
	fold := newFoldList()
	onlyKnown := widget.NewCheck(i18n.T("wot.sigs.onlyknown"), nil)

	var build func()
	// Rechtsklick auf einen unbekannten Unterzeichner: Schlüssel vom
	// Keyserver laden, danach Liste neu aufbauen (Signatur wird prüfbar).
	signerMenu := func(c pgp.Certification) func() *fyne.Menu {
		if c.Status != pgp.CertUnknown {
			return nil
		}
		return func() *fyne.Menu {
			return fyne.NewMenu("", fyne.NewMenuItem(i18n.T("wot.ks.ctx"), func() {
				u.fetchFromKeyserver(win, c.SignerRef, func(rep pgp.ImportReport) {
					build()
					if changed != nil {
						changed()
					}
					if len(rep.Keys) > 0 {
						dialog.ShowInformation(i18n.T("wot.ks.title"), fmt.Sprintf(i18n.T("wot.ks.signerdone"),
							fmt.Sprintf("%s <%s>", rep.Keys[0].Name, rep.Keys[0].Email)), win)
					}
				})
			}))
		}
	}

	build = func() {
		certs := latestCerts(u.keyring.Certifications(info.Fingerprint))
		known := signerCount(knownOnly(certs))
		head := fmt.Sprintf("%s <%s>\n%s\n%s", info.Name, info.Email, groupFpr(info.Fingerprint),
			fmt.Sprintf(i18n.T("wot.sigs.summary"), signerCount(certs), len(uids), known))
		if known < signerCount(certs) {
			head += "\n" + i18n.T("wot.sigs.ctxhint")
		}
		headLabel.SetText(head)
		if known == 0 {
			onlyKnown.Disable()
		} else {
			onlyKnown.Enable()
		}
		var sections []foldSection
		for _, uid := range uids {
			var rows []pgp.Certification
			for _, c := range certs {
				if c.UID == uid && (!onlyKnown.Checked || c.Status != pgp.CertUnknown) {
					rows = append(rows, c)
				}
			}
			list := container.NewVBox()
			if len(rows) == 0 {
				list.Add(widget.NewLabel(i18n.T("wot.sigs.none")))
			}
			for _, c := range rows {
				list.Add(certRow(c, signerMenu(c)))
			}
			sections = append(sections, foldSection{key: uid,
				title: fmt.Sprintf("%s  (%d)", uid, len(rows)), detail: list})
		}
		fold.set(sections)
	}
	onlyKnown.OnChanged = func(bool) { build() }
	build()

	content := container.NewBorder(container.NewVBox(headLabel, onlyKnown, widget.NewSeparator()), nil, nil, nil,
		fold.scroll)
	d := dialog.NewCustom(i18n.T("wot.sigs.title"), i18n.T("contacts.close"), content, win)
	d.Resize(wotInnerSize(win))
	d.Show()
}

// foldSection ist ein aufklappbarer Abschnitt einer foldList.
type foldSection struct {
	key, title string
	detail     fyne.CanvasObject
}

// foldList ist eine Liste aufklappbarer Abschnitte (standardmäßig zu) in
// einer Scroll-Fläche. Eigenbau statt widget.Accordion: dessen Layout gibt
// freien Platz an offene Einträge, und die Scroll-Fläche schrumpft nach
// dem Zuklappen nicht mit – es entstanden große Lücken zwischen den
// Einträgen.
type foldList struct {
	box    *fyne.Container
	scroll *container.Scroll
	open   map[string]bool // Offen-Zustand je key, bleibt bei set erhalten
}

func newFoldList() *foldList {
	f := &foldList{box: container.NewVBox(), open: map[string]bool{}}
	f.scroll = container.NewVScroll(f.box)
	return f
}

// set baut die Abschnitte neu auf.
func (f *foldList) set(sections []foldSection) {
	f.box.Objects = nil
	for i, s := range sections {
		s := s
		if i > 0 {
			f.box.Add(widget.NewSeparator())
		}
		btn := widget.NewButton(s.title, nil)
		btn.Alignment = widget.ButtonAlignLeading
		btn.Importance = widget.LowImportance
		apply := func() {
			if f.open[s.key] {
				btn.SetIcon(theme.MenuDropDownIcon())
				s.detail.Show()
			} else {
				btn.SetIcon(theme.MenuExpandIcon())
				s.detail.Hide()
			}
		}
		btn.OnTapped = func() {
			f.open[s.key] = !f.open[s.key]
			apply()
			f.relayout()
		}
		apply()
		f.box.Add(btn)
		f.box.Add(s.detail)
	}
	f.relayout()
}

// relayout lässt die Scroll-Fläche die neue Inhaltshöhe übernehmen.
func (f *foldList) relayout() {
	f.box.Refresh()
	f.scroll.Refresh()
}

// certRow ist eine Zeile im Signaturen-Dialog: Unterzeichner links, Datum
// und Status rechts; menu (darf nil sein) ist das Kontextmenü.
func certRow(c pgp.Certification, menu func() *fyne.Menu) fyne.CanvasObject {
	who := c.SignerName
	if who == "" {
		who = "0x" + c.SignerKeyID
	} else {
		who += "  (0x" + c.SignerKeyID + ")"
	}
	var status string
	switch c.Status {
	case pgp.CertValid:
		status = "✓ " + i18n.T("wot.sigs.valid")
	case pgp.CertInvalid:
		status = "✗ " + i18n.T("wot.sigs.invalid")
	default:
		status = "? " + i18n.T("wot.sigs.unknown")
	}
	if c.Revoked {
		status += ", " + i18n.T("wot.sigs.revoked")
	}
	if c.Mine {
		status += ", " + i18n.T("wot.sigs.mine")
	}
	name := widget.NewLabel(who)
	name.Truncation = fyne.TextTruncateEllipsis
	row := container.NewBorder(nil, nil, nil,
		widget.NewLabel(c.Created.Format(i18n.T("date.short"))+"   "+status), name)
	if menu == nil {
		return row
	}
	return withContextMenu(row, menu)
}

// latestCerts behält je Identität und Unterzeichner nur die neueste
// Zertifizierung und sortiert: eigene, gültige, ungültige, unbekannte;
// innerhalb nach Name bzw. Key-ID.
func latestCerts(in []pgp.Certification) []pgp.Certification {
	idx := map[string]int{}
	var out []pgp.Certification
	for _, c := range in {
		key := c.UID + "\x00" + c.SignerKeyID
		if i, ok := idx[key]; ok {
			if c.Created.After(out[i].Created) {
				out[i] = c
			}
			continue
		}
		idx[key] = len(out)
		out = append(out, c)
	}
	rank := func(c pgp.Certification) int {
		switch {
		case c.Mine:
			return 0
		case c.Status == pgp.CertValid:
			return 1
		case c.Status == pgp.CertInvalid:
			return 2
		}
		return 3
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank(out[i]), rank(out[j]); ri != rj {
			return ri < rj
		}
		ni, nj := strings.ToLower(out[i].SignerName), strings.ToLower(out[j].SignerName)
		if ni != nj {
			return ni < nj
		}
		return out[i].SignerKeyID < out[j].SignerKeyID
	})
	return out
}

// signerCount zählt verschiedene Unterzeichner (Personen statt Signaturen).
func signerCount(certs []pgp.Certification) int {
	seen := map[string]bool{}
	for _, c := range certs {
		seen[c.SignerKeyID] = true
	}
	return len(seen)
}

func knownOnly(certs []pgp.Certification) []pgp.Certification {
	var out []pgp.Certification
	for _, c := range certs {
		if c.Status != pgp.CertUnknown {
			out = append(out, c)
		}
	}
	return out
}

// saveFingerprintSheet speichert eine A4-Seite (PDF) mit den eigenen
// Schlüsseldaten als Streifen zum Abschneiden – zum Verteilen z. B. auf
// Konferenzen (pkg/keyslip).
func (u *UI) saveFingerprintSheet(win fyne.Window, info pgp.KeyInfo) {
	data, err := keyslip.A4(u.slipInfo(info), keyslip.Fonts{
		Regular: theme.DefaultTextFont().Content(),
		Bold:    theme.DefaultTextBoldFont().Content(),
		Mono:    theme.DefaultTextMonospaceFont().Content(),
	})
	if err != nil {
		u.showError(err)
		return
	}
	fd := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
		if err != nil || wc == nil {
			return
		}
		defer wc.Close()
		if _, err := wc.Write(data); err != nil {
			u.showError(err)
			return
		}
		dialog.ShowInformation(i18n.T("wot.title"),
			fmt.Sprintf(i18n.T("wot.sheet.saved"), wc.URI().Name(), keyslip.PerPage), win)
	}, win)
	fd.SetFileName("fingerprint-0x" + u.keyring.KeyID(info.Fingerprint) + ".pdf")
	fd.SetFilter(storage.NewExtensionFileFilter([]string{".pdf"}))
	fd.Show()
}

// slipInfo stellt die Angaben eines Streifens zusammen: Name und E-Mail der
// primären (ersten) Identität, lange Key-ID, Typen, voller Fingerprint.
func (u *UI) slipInfo(info pgp.KeyInfo) keyslip.Info {
	return keyslip.Info{
		Name:        info.Name,
		Email:       info.Email,
		KeyID:       u.keyring.KeyID(info.Fingerprint),
		Types:       u.keyring.KeyTypes(info.Fingerprint),
		Fingerprint: info.Fingerprint,
	}
}

// importPublicData importiert öffentliche Schlüssel samt neuer
// Zertifizierungen (Merge) und meldet das Ergebnis. ok=false bei Fehler.
func (u *UI) importPublicData(win fyne.Window, data []byte) (pgp.ImportReport, bool) {
	rep, err := u.keyring.ImportPublicReport(data)
	if err != nil {
		if rep.OnlyOwn {
			dialog.ShowInformation(i18n.T("wot.title"), i18n.T("pgp.import.ownnochange"), win)
			return rep, false
		}
		if errors.Is(err, pgp.ErrNoNewKeys) {
			dialog.ShowInformation(i18n.T("wot.title"), i18n.T("pgp.import.exists"), win)
			return rep, false
		}
		u.showError(err)
		return rep, false
	}
	if rep.Merged > 0 {
		text := fmt.Sprintf(i18n.T("wot.import.merged"), rep.Merged)
		if rep.MergedOwn {
			text += "\n\n" + i18n.T("wot.import.ownhint")
		}
		u.log.append(fmt.Sprintf(i18n.T("wot.import.merged"), rep.Merged))
		msg := widget.NewLabel(text)
		msg.Wrapping = fyne.TextWrapWord
		d := dialog.NewCustom(i18n.T("wot.title"), i18n.T("contacts.close"), msg, win)
		d.Resize(fyne.NewSize(480, 200))
		d.Show()
	}
	return rep, true
}

// showKeyserverDialog fragt einen vollständigen Fingerprint ab, holt den
// Schlüssel vom eingestellten Keyserver und importiert ihn. Zertifiziert
// wird danach wie gewohnt über „Zertifizieren …“ am Schlüssel.
func (u *UI) showKeyserverDialog(win fyne.Window, done func()) {
	server, err := pgp.KeyserverURL(u.cfg.PGPKeyserver)
	if err != nil {
		u.showError(err)
		return
	}
	intro := widget.NewLabel(fmt.Sprintf(i18n.T("wot.ks.intro"), server))
	intro.Wrapping = fyne.TextWrapWord
	fprEntry := widget.NewEntry()
	fprEntry.SetPlaceHolder("0x1EEB928B64FF2B4E")
	fprEntry.TextStyle = fyne.TextStyle{Monospace: true}

	var d dialog.Dialog
	var okBtn *widget.Button
	okBtn = widget.NewButton(i18n.T("wot.ks.do"), func() {
		fpr, err := pgp.NormalizeKeyRef(fprEntry.Text)
		if err != nil {
			u.showError(err)
			return
		}
		d.Hide()
		u.fetchFromKeyserver(win, fpr, func(rep pgp.ImportReport) {
			done()
			if len(rep.Keys) == 0 {
				return
			}
			info := rep.Keys[0]
			text := fmt.Sprintf(i18n.T("wot.ks.done"),
				fmt.Sprintf("%s <%s>", info.Name, strings.Join(info.Emails, ", ")), groupFpr(info.Fingerprint))
			if len(info.Emails) == 0 {
				text += "\n\n" + i18n.T("wot.ks.noemail")
			}
			u.log.append(fmt.Sprintf("%s: %s", i18n.T("wot.ks.title"), info.Fingerprint))
			msg := widget.NewLabel(text)
			msg.Wrapping = fyne.TextWrapWord
			res := dialog.NewCustom(i18n.T("wot.ks.title"), i18n.T("contacts.close"), msg, win)
			res.Resize(fyne.NewSize(520, 240))
			res.Show()
		})
	})
	okBtn.Importance = widget.HighImportance
	okBtn.Disable()
	// Live-Hinweis, warum „Herunterladen“ noch gesperrt ist.
	hint := widget.NewLabel("")
	hint.Wrapping = fyne.TextWrapWord
	hint.Importance = widget.LowImportance
	fprEntry.OnChanged = func(s string) {
		if _, err := pgp.NormalizeKeyRef(s); err == nil {
			okBtn.Enable()
			hint.SetText("")
		} else {
			okBtn.Disable()
			if strings.TrimSpace(s) != "" {
				hint.SetText(err.Error())
			} else {
				hint.SetText("")
			}
		}
	}
	fprEntry.OnSubmitted = func(string) {
		if !okBtn.Disabled() {
			okBtn.OnTapped()
		}
	}
	cancelBtn := widget.NewButton(i18n.T("compose.cancel"), func() { d.Hide() })
	content := container.NewVBox(intro,
		container.NewBorder(nil, nil, widget.NewLabel(i18n.T("wot.ks.fpr")), nil, fprEntry),
		hint,
		container.NewCenter(container.NewHBox(cancelBtn, okBtn)))
	d = dialog.NewCustomWithoutButtons(i18n.T("wot.ks.title"), content, win)
	d.Resize(fyne.NewSize(620, 260))
	d.Show()
	win.Canvas().Focus(fprEntry)
}

// fetchFromKeyserver holt den Schlüssel ref (Fingerprint oder lange Key-ID)
// vom eingestellten Keyserver mit Fortschrittsanzeige und importiert ihn.
// after läuft nur nach erfolgreichem Import (auch bloßem Merge). Ist der
// Schlüssel dort nicht vorhanden, gibt es einen Hinweis statt eines Fehlers.
func (u *UI) fetchFromKeyserver(win fyne.Window, ref string, after func(pgp.ImportReport)) {
	server, err := pgp.KeyserverURL(u.cfg.PGPKeyserver)
	if err != nil {
		u.showError(err)
		return
	}
	wait := dialog.NewCustomWithoutButtons(i18n.T("wot.ks.title"), container.NewVBox(
		widget.NewLabel(i18n.T("wot.ks.loading")), widget.NewProgressBarInfinite()), win)
	wait.Show()
	go func() {
		data, err := pgp.FetchKey(context.Background(), nil, server, ref)
		fyne.Do(func() {
			wait.Hide()
			if pgp.IsKeyNotOnServer(err) {
				dialog.ShowInformation(i18n.T("wot.ks.title"),
					fmt.Sprintf(i18n.T("wot.ks.notfound"), "0x"+ref, server), win)
				return
			}
			if err != nil {
				u.showError(err)
				return
			}
			if rep, ok := u.importPublicData(win, data); ok {
				after(rep)
			}
		})
	}()
}

// showKeyInfoDialog zeigt Haupt- und Unterschlüssel mit Algorithmus,
// Verwendung, Erstellungs- und Ablaufdatum – abgelaufene oder widerrufene
// Teile deutlich markiert.
func (u *UI) showKeyInfoDialog(win fyne.Window, info pgp.KeyInfo) {
	date := func(t time.Time) string { return t.Format(i18n.T("date.short")) }
	box := container.NewVBox()
	head := widget.NewLabel(fmt.Sprintf("%s <%s>\n%s", info.Name, info.Email, groupFpr(info.Fingerprint)))
	head.TextStyle.Bold = true
	head.Wrapping = fyne.TextWrapWord
	box.Add(head)
	for _, p := range u.keyring.KeyParts(info.Fingerprint) {
		box.Add(widget.NewSeparator())
		title := i18n.T("wot.info.sub")
		if p.Primary {
			title = i18n.T("wot.info.primary")
		}
		t := widget.NewLabel(title)
		t.TextStyle.Bold = true
		box.Add(t)
		usage := p.Usage
		if usage == "" {
			usage = i18n.T("wot.info.nousage")
		}
		expiry := i18n.T("settings.pgp.noexpiry")
		if !p.Expiry.IsZero() {
			expiry = date(p.Expiry)
		}
		var status fyne.CanvasObject = widget.NewLabel(i18n.T("wot.info.valid"))
		switch {
		case p.Revoked:
			status = container.NewHBox(warnIcon(), widget.NewLabel(i18n.T("wot.info.revoked")))
		case p.Expired:
			status = container.NewHBox(warnIcon(), widget.NewLabel(i18n.T("wot.info.expired")))
		}
		fpr := widget.NewLabel(groupFpr(p.Fingerprint))
		fpr.TextStyle.Monospace = true
		box.Add(widget.NewForm(
			widget.NewFormItem(i18n.T("wot.info.algo"), widget.NewLabel(p.Algo)),
			widget.NewFormItem(i18n.T("wot.info.usage"), widget.NewLabel(usage)),
			widget.NewFormItem(i18n.T("wot.info.fpr"), fpr),
			widget.NewFormItem(i18n.T("wot.info.created"), widget.NewLabel(date(p.Created))),
			widget.NewFormItem(i18n.T("wot.info.expires"), widget.NewLabel(expiry)),
			widget.NewFormItem(i18n.T("wot.info.status"), status),
		))
	}
	d := dialog.NewCustom(i18n.T("wot.info.title"), i18n.T("contacts.close"), container.NewVScroll(box), win)
	d.Resize(fyne.NewSize(600, 520))
	d.Show()
}

// warnIcon ist das gelbe Fyne-Warndreieck (Theme-Warnfarbe) für
// abgelaufene oder widerrufene Schlüssel.
func warnIcon() *widget.Icon {
	return widget.NewIcon(theme.NewWarningThemedResource(theme.WarningIcon()))
}
