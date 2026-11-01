package ui

import (
	"bytes"
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/compose"
	"github.com/cfluegel/uucpmail-client/pkg/contacts"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	mailmime "github.com/cfluegel/uucpmail-client/pkg/mime"
	"github.com/cfluegel/uucpmail-client/pkg/pgp"
	"github.com/cfluegel/uucpmail-client/pkg/store"
)

// buildMainWindow setzt das Layout zusammen: Ordner als Tabs über der
// Nachrichtenliste, darunter die Inhaltsansicht.
func (u *UI) buildMainWindow() {
	u.messageView = newViewPanel(u)
	u.messageList = newMessagePanel(u)
	u.folderList = newFolderPanel(u)
	u.statusBar = newStatusPanel()

	toolbar := widget.NewToolbar(
		widget.NewToolbarAction(theme.DocumentCreateIcon(), func() { u.showCompose() }),
		widget.NewToolbarAction(theme.ViewRefreshIcon(), func() { u.runSync() }),
		widget.NewToolbarAction(theme.AccountIcon(), func() { u.showContactsWindow() }),
		widget.NewToolbarSeparator(),
		widget.NewToolbarAction(theme.ListIcon(), func() { u.showLogWindow() }),
		widget.NewToolbarAction(theme.SettingsIcon(), func() { u.showSettings(false) }),
	)

	u.listZone = newFocusZone(u.messageList.root,
		func(ev *fyne.KeyEvent, shift bool) { u.handleZoneKey(zoneList, ev, shift) },
		u.toggleZone, func() { u.activeZone = zoneList })
	u.contentZone = newFocusZone(u.messageView.widget,
		func(ev *fyne.KeyEvent, shift bool) { u.handleZoneKey(zoneContent, ev, shift) },
		u.toggleZone, func() { u.activeZone = zoneContent })
	split := container.NewVSplit(u.listZone, u.contentZone)
	split.SetOffset(u.state.SplitOffset)

	top := container.NewVBox(toolbar, u.folderList.widget)
	u.win.SetContent(container.NewBorder(top, u.statusBar.widget, nil, nil, split))

	// Fenstergröße, Vollbild und Split-Position über Neustarts erhalten.
	u.win.Resize(fyne.NewSize(float32(u.state.WinW), float32(u.state.WinH)))
	if u.state.Fullscreen {
		u.win.SetFullScreen(true)
	}
	u.win.SetCloseIntercept(func() {
		u.state.SplitOffset = split.Offset
		u.state.Fullscreen = u.win.FullScreen()
		if !u.win.FullScreen() {
			size := u.win.Canvas().Size()
			u.state.WinW = float64(size.Width)
			u.state.WinH = float64(size.Height)
		}
		u.state.save()
		u.win.Close()
	})

	u.setupKeyboard()
	u.refresh()
	// Start: Tastaturfokus in der Mailliste (Posteingang ist der Default-Ordner).
	u.win.Canvas().Focus(u.listZone)
}

// ---- Ordner-Tabs ----

type folderPanel struct {
	widget *container.AppTabs
	items  []*container.TabItem
	u      *UI
}

func newFolderPanel(u *UI) *folderPanel {
	p := &folderPanel{u: u}
	for _, name := range store.Folders {
		// Inhaltslos: die Tabs dienen nur als Ordner-Umschalter, die
		// eigentliche Liste liegt darunter im Hauptlayout.
		p.items = append(p.items,
			container.NewTabItem(i18n.T("folder."+name), container.NewWithoutLayout()))
	}
	p.widget = container.NewAppTabs(p.items...)
	p.widget.OnSelected = func(ti *container.TabItem) {
		for i, item := range p.items {
			if item == ti {
				u.currentFolder = store.Folders[i]
			}
		}
		u.messageList.load(u.currentFolder)
		u.messageView.clear()
	}
	return p
}

func (p *folderPanel) refresh() {
	// Inbox zählt Ungelesene, Outbox alle wartenden Nachrichten.
	unread, _ := p.u.store.UnreadCount(store.Inbox)
	outbox := 0
	if msgs, err := p.u.store.List(store.Outbox); err == nil {
		outbox = len(msgs)
	}
	for i, name := range store.Folders {
		label := i18n.T("folder." + name)
		switch {
		case name == store.Inbox && unread > 0:
			label = fmt.Sprintf("%s (%d)", label, unread)
		case name == store.Outbox && outbox > 0:
			label = fmt.Sprintf("%s (%d)", label, outbox)
		}
		p.items[i].Text = label
	}
	p.widget.Refresh()
}

// ---- Nachrichtenliste ----

type messagePanel struct {
	root    fyne.CanvasObject // Überschriften + Liste
	widget  *widget.List
	header  *fyne.Container
	cols    *mailColumns
	rows    []*fyne.Container // von der Liste wiederverwendete Zeilen-Container
	sortAsc bool              // false = neueste zuerst (Standard)
	u       *UI
	metas   []*store.Meta
	// Zusammenhängende Mehrfachauswahl: Bereich [anchor,cursor], -1 = keine.
	// Der Cursor ist zugleich die in der Ansicht gezeigte Nachricht.
	anchor int
	cursor int
}

// moveCursor bewegt den Listen-Cursor; extend=true (Shift) erweitert die
// Auswahl vom Anker aus, sonst kollabiert sie auf den Cursor.
func (p *messagePanel) moveCursor(delta int, extend bool) {
	if len(p.metas) == 0 {
		return
	}
	c := p.cursor
	if c < 0 {
		c = 0
	} else {
		c += delta
		if c < 0 {
			c = 0
		}
		if c >= len(p.metas) {
			c = len(p.metas) - 1
		}
	}
	p.cursor = c
	if !extend || p.anchor < 0 {
		p.anchor = c
	}
	// Select zeigt die Nachricht (OnSelected) und scrollt den Cursor sichtbar.
	p.widget.Select(c)
	p.widget.Refresh() // Bereichs-Hintergründe auf recycelten Zeilen neu malen
}

func (p *messagePanel) selRange() (int, int) {
	if p.anchor < 0 || p.cursor < 0 {
		return -1, -1
	}
	lo, hi := p.anchor, p.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

func (p *messagePanel) isSelected(i int) bool {
	lo, hi := p.selRange()
	return lo >= 0 && i >= lo && i <= hi
}

func (p *messagePanel) selectionCount() int {
	lo, hi := p.selRange()
	if lo < 0 {
		return 0
	}
	return hi - lo + 1
}

// selectedMetas liefert die markierten Nachrichten (Listenreihenfolge).
func (p *messagePanel) selectedMetas() []*store.Meta {
	lo, hi := p.selRange()
	if lo < 0 {
		return nil
	}
	if hi >= len(p.metas) {
		hi = len(p.metas) - 1
	}
	return p.metas[lo : hi+1]
}

func (p *messagePanel) resetSelection() {
	p.anchor, p.cursor = -1, -1
}

func newMessagePanel(u *UI) *messagePanel {
	p := &messagePanel{u: u, anchor: -1, cursor: -1}
	p.cols = &mailColumns{
		fromW: float32(u.state.ColFrom),
		toW:   float32(u.state.ColTo),
		dateW: float32(u.state.ColDate),
	}
	p.widget = widget.NewList(
		func() int { return len(p.metas) },
		func() fyne.CanvasObject {
			unread := widget.NewLabel("●")
			from := widget.NewLabel("from")
			from.Truncation = fyne.TextTruncateEllipsis
			to := widget.NewLabel("to")
			to.Truncation = fyne.TextTruncateEllipsis
			subject := widget.NewLabel("subject")
			subject.Truncation = fyne.TextTruncateEllipsis
			date := widget.NewLabel("02.01.06 15:04")
			date.TextStyle.Monospace = true
			date.Truncation = fyne.TextTruncateClip
			row := container.New(&mailRowLayout{c: p.cols}, unread, from, to, subject, date)
			p.rows = append(p.rows, row)
			wrap := newTapRow(row)
			wrap.onSingle = func() {
				if wrap.index >= 0 {
					p.anchor, p.cursor = wrap.index, wrap.index
					p.widget.Select(wrap.index)
					p.widget.Refresh()
					// Klick in die Liste holt den Tastaturfokus zurück.
					if u.listZone != nil {
						u.win.Canvas().Focus(u.listZone)
					}
				}
			}
			wrap.onDouble = func() {
				i := wrap.index
				if p.u.currentFolder == store.Drafts && i >= 0 && i < len(p.metas) {
					p.u.showComposeDraft(p.metas[i])
				}
			}
			return wrap
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= len(p.metas) {
				return
			}
			m := p.metas[i]
			wrap := o.(*tapRow)
			wrap.index = i
			wrap.setSelected(p.isSelected(i))
			row := wrap.content.(*fyne.Container)
			unread := row.Objects[0].(*widget.Label)
			from := row.Objects[1].(*widget.Label)
			to := row.Objects[2].(*widget.Label)
			subject := row.Objects[3].(*widget.Label)
			date := row.Objects[4].(*widget.Label)

			switch {
			case !m.Read:
				unread.SetText("●")
			case m.HasAttach:
				unread.SetText("📎")
			default:
				unread.SetText(" ")
			}
			from.SetText(m.From)
			to.SetText(m.To)
			subj := m.Subject
			if m.LastError != "" {
				subj = "⚠ " + subj
			}
			subject.SetText(subj)
			date.SetText(m.Date.Local().Format("02.01.06 15:04"))
		},
	)
	p.widget.OnSelected = func(i widget.ListItemID) {
		if i < len(p.metas) {
			p.u.messageView.show(p.metas[i])
		}
	}

	// Überschriftenzeile mit ziehbaren Spaltentrennern; die Datum-Überschrift
	// ist klickbar und schaltet die Sortierrichtung um.
	p.sortAsc = u.state.SortDateAsc
	hFrom := widget.NewLabel(i18n.T("list.from"))
	hFrom.TextStyle.Bold = true
	hTo := widget.NewLabel(i18n.T("list.to"))
	hTo.TextStyle.Bold = true
	hSubject := widget.NewLabel(i18n.T("list.subject"))
	hSubject.TextStyle.Bold = true
	hDate := widget.NewButton(p.dateHeaderText(), nil)
	hDate.Importance = widget.LowImportance
	hDate.Alignment = widget.ButtonAlignLeading
	hDate.OnTapped = func() {
		p.sortAsc = !p.sortAsc
		u.state.SortDateAsc = p.sortAsc
		u.state.save()
		hDate.SetText(p.dateHeaderText())
		p.load(u.currentFolder)
	}
	refresh := func() {
		p.header.Refresh()
		// List.Refresh() allein layoutet gecachte Zeilen nicht neu – die
		// wiederverwendeten Container explizit anstoßen.
		for _, row := range p.rows {
			row.Refresh()
		}
		p.widget.Refresh()
	}
	persist := func() {
		u.state.ColFrom = float64(p.cols.fromW)
		u.state.ColTo = float64(p.cols.toW)
		u.state.ColDate = float64(p.cols.dateW)
		u.state.save()
	}
	divFrom := newColDivider(func(dx float32) {
		p.cols.fromW = clamp(p.cols.fromW+dx, colMinFrom, 500)
		refresh()
	}, persist)
	divTo := newColDivider(func(dx float32) {
		p.cols.toW = clamp(p.cols.toW+dx, colMinTo, 500)
		refresh()
	}, persist)
	divDate := newColDivider(func(dx float32) {
		p.cols.dateW = clamp(p.cols.dateW-dx, colMinDate, 300)
		refresh()
	}, persist)
	spacer := widget.NewLabel(" ")
	p.header = container.New(&mailHeaderLayout{c: p.cols},
		spacer, hFrom, hTo, hSubject, hDate, divFrom, divTo, divDate)
	p.root = container.NewBorder(
		container.NewVBox(p.header, widget.NewSeparator()), nil, nil, nil, p.widget)
	return p
}

// dateHeaderText liefert die Datum-Überschrift samt Sortier-Indikator.
func (p *messagePanel) dateHeaderText() string {
	if p.sortAsc {
		return i18n.T("list.date") + " ▲"
	}
	return i18n.T("list.date") + " ▼"
}

func (p *messagePanel) load(folder string) {
	metas, err := p.u.store.List(folder) // neueste zuerst
	if err != nil {
		p.u.showError(err)
		return
	}
	if p.sortAsc {
		for i, j := 0, len(metas)-1; i < j; i, j = i+1, j-1 {
			metas[i], metas[j] = metas[j], metas[i]
		}
	}
	p.metas = metas
	p.resetSelection()
	p.widget.UnselectAll()
	p.widget.Refresh()
}

// ---- Nachrichtenansicht ----

type viewPanel struct {
	widget     fyne.CanvasObject
	header     *widget.Label
	pgpStatus  *widget.Label
	body       *widget.Label
	bodyWrap   *container.Scroll
	htmlBody   *widget.RichText
	htmlWrap   *container.Scroll
	attachBar  *fyne.Container
	actions    *fyne.Container
	editBtn    *widget.Button
	restoreBtn *widget.Button
	unreadBtn  *widget.Button
	resendBtn  *widget.Button
	srcBtn     *widget.Button
	decBtn     *widget.Button
	u          *UI
	current    *store.Meta
	raw        []byte
	pmOuter    *mailmime.ParsedMessage
	decrypted  *mailmime.ParsedMessage // nur im Speicher, nie auf Platte
	decFor     string                  // Store-ID, zu der decrypted gehört
	decSig     pgp.SigResult
	showSrc    bool
}

func newViewPanel(u *UI) *viewPanel {
	p := &viewPanel{u: u}
	p.header = widget.NewLabel(i18n.T("view.empty"))
	p.header.Wrapping = fyne.TextWrapWord
	p.header.Selectable = true // Headerdaten müssen kopierbar sein
	p.pgpStatus = widget.NewLabel("")
	p.pgpStatus.Wrapping = fyne.TextWrapWord
	p.pgpStatus.TextStyle.Bold = true
	p.pgpStatus.Hide()
	p.body = widget.NewLabel("")
	p.body.TextStyle.Monospace = true
	p.body.Wrapping = fyne.TextWrapWord
	p.body.Selectable = true
	p.bodyWrap = container.NewVScroll(p.body)
	p.htmlBody = widget.NewRichText()
	p.htmlBody.Wrapping = fyne.TextWrapWord
	p.htmlWrap = container.NewVScroll(p.htmlBody)
	p.htmlWrap.Hide()
	p.attachBar = container.NewHBox()
	p.attachBar.Hide()

	deleteBtn := widget.NewButtonWithIcon(i18n.T("msg.delete")+" ("+i18n.T("key.del")+")", theme.DeleteIcon(), func() {
		// Wirkt wie die Entf-Taste auf die gesamte Markierung.
		u.deleteSelection()
	})
	p.editBtn = widget.NewButton(i18n.T("compose.edit"), func() {
		if p.current != nil && p.current.Folder == store.Drafts {
			u.showComposeDraft(p.current)
		}
	})
	p.srcBtn = widget.NewButton(i18n.T("view.source"), func() {
		p.showSrc = !p.showSrc
		p.renderCurrent()
	})
	replyBtn := widget.NewButtonWithIcon(i18n.T("msg.reply")+" (r)", theme.MailReplyIcon(), func() {
		p.composeReply(false)
	})
	replyAllBtn := widget.NewButtonWithIcon(i18n.T("msg.replyall")+" (Shift+r)", theme.MailReplyAllIcon(), func() {
		p.composeReply(true)
	})
	forwardBtn := widget.NewButtonWithIcon(i18n.T("msg.forward")+" (f)", theme.MailForwardIcon(), func() {
		p.composeForward()
	})
	p.decBtn = widget.NewButton(i18n.T("pgp.decrypt.btn"), func() {
		p.decryptWithPrompt()
	})
	p.decBtn.Hide()
	p.restoreBtn = widget.NewButtonWithIcon(i18n.T("msg.restore"), theme.ContentUndoIcon(), func() {
		u.restoreSelected()
	})
	p.restoreBtn.Hide()
	p.unreadBtn = widget.NewButtonWithIcon(i18n.T("msg.unread")+" (u)", theme.VisibilityOffIcon(), func() {
		u.markSelectionUnread()
	})
	p.unreadBtn.Hide()
	p.resendBtn = widget.NewButtonWithIcon(i18n.T("msg.editasnew"), theme.DocumentCreateIcon(), func() {
		p.composeEditAsNew()
	})
	p.resendBtn.Hide()
	p.actions = container.NewHBox(replyBtn, replyAllBtn, forwardBtn, p.decBtn, p.editBtn, p.restoreBtn, p.unreadBtn, p.resendBtn, p.srcBtn, deleteBtn)
	p.actions.Hide()
	bodyStack := container.NewStack(p.bodyWrap, p.htmlWrap)
	p.widget = container.NewBorder(
		container.NewVBox(p.header, p.pgpStatus, widget.NewSeparator()),
		container.NewVBox(container.NewHScroll(p.attachBar), p.actions), nil, nil, bodyStack)
	return p
}

func (p *viewPanel) clear() {
	p.current = nil
	p.raw = nil
	p.pmOuter = nil
	p.decrypted = nil
	p.decFor = ""
	p.decSig = pgp.SigResult{}
	p.showSrc = false
	p.pgpStatus.Hide()
	p.decBtn.Hide()
	p.header.SetText(i18n.T("view.empty"))
	p.body.SetText("")
	p.bodyWrap.Show()
	p.htmlWrap.Hide()
	p.attachBar.Hide()
	p.actions.Hide()
}

func (p *viewPanel) show(m *store.Meta) {
	p.current = m
	raw, err := p.u.store.Raw(m.Folder, m.ID)
	if err != nil {
		p.u.showError(err)
		return
	}
	var hdr strings.Builder
	fmt.Fprintf(&hdr, "%s: %s\n", i18n.T("list.from"), m.From)
	fmt.Fprintf(&hdr, "%s: %s\n", i18n.T("list.to"), m.To)
	if m.Cc != "" {
		fmt.Fprintf(&hdr, "%s: %s\n", i18n.T("list.cc"), m.Cc)
	}
	fmt.Fprintf(&hdr, "%s: %s\n", i18n.T("list.subject"), m.Subject)
	fmt.Fprintf(&hdr, "%s: %s", i18n.T("list.date"), m.Date.Local().Format(time.RFC1123))
	if m.LastError != "" {
		fmt.Fprintf(&hdr, "\n%s: %s", i18n.T("view.error"), m.LastError)
	}
	p.header.SetText(hdr.String())
	p.raw = raw
	if p.decFor != m.ID {
		p.decrypted = nil
		p.decSig = pgp.SigResult{}
	}
	p.showSrc = false
	p.renderCurrent()
	// Verschlüsselt und noch nicht entschlüsselt: erst still über den Cache
	// versuchen, sonst einmalig fragen (nur hier – nicht bei jedem Render).
	if p.pmOuter != nil && p.pmOuter.Encrypted && p.decrypted == nil {
		if !p.tryCachedDecrypt() {
			p.askDecrypt()
		}
	}
	if m.Folder == store.Drafts {
		p.editBtn.Show()
	} else {
		p.editBtn.Hide()
	}
	if m.Folder == store.Trash {
		p.restoreBtn.Show()
	} else {
		p.restoreBtn.Hide()
	}
	if m.Folder == store.Inbox {
		p.unreadBtn.Show()
	} else {
		p.unreadBtn.Hide()
	}
	if m.Folder == store.Sent {
		p.resendBtn.Show()
	} else {
		p.resendBtn.Hide()
	}
	p.actions.Show()

	if !m.Read {
		if err := p.u.store.SetRead(m.Folder, m.ID, true); err == nil {
			m.Read = true
			p.u.folderList.refresh()
			p.u.messageList.widget.Refresh()
		}
	}
}

// renderCurrent zeigt die aktuelle Nachricht: text/plain bevorzugt, sonst
// sicher gerendertes HTML; Quelltext-Toggle zeigt die Rohbytes. Anhänge
// erscheinen als Speichern-Buttons.
func (p *viewPanel) renderCurrent() {
	if p.raw == nil {
		return
	}
	p.attachBar.Objects = nil

	if p.showSrc {
		p.htmlWrap.Hide()
		p.body.SetText(strings.ReplaceAll(string(p.raw), "\r\n", "\n"))
		p.bodyWrap.Show()
		p.attachBar.Hide()
		return
	}

	pm, err := mailmime.Parse(p.raw)
	if err != nil {
		// Kaputte Header: Fallback auf naive Darstellung.
		p.pmOuter = nil
		p.htmlWrap.Hide()
		p.body.SetText(bodyText(p.raw))
		p.bodyWrap.Show()
		p.attachBar.Hide()
		return
	}
	p.pmOuter = pm

	// Verschlüsselte Nachricht: entschlüsselt anzeigen (nur im Speicher)
	// oder Platzhalter; der Entschlüsseln-Flow hängt an show()/decBtn.
	if pm.Encrypted {
		p.decBtn.Show()
		if p.decrypted != nil && p.decFor == p.current.ID {
			p.setSigStatus(p.decSig, true)
			p.renderParsed(p.decrypted)
			return
		}
		p.pgpStatus.Hide()
		p.htmlWrap.Hide()
		p.body.SetText(i18n.T("pgp.view.encrypted"))
		p.bodyWrap.Show()
		p.attachBar.Hide()
		return
	}
	p.decBtn.Hide()

	switch {
	case pm.Signed:
		res := pgp.Verify(pm.SignedPart, pm.Signature, p.u.keyring.PublicEntities())
		if res.Status == pgp.SigInvalid {
			// Gateway hat CRLF→LF normalisiert: kanonisiert nachprüfen.
			res = pgp.Verify(pgp.NormalizeCRLF(pm.SignedPart), pm.Signature, p.u.keyring.PublicEntities())
		}
		p.setSigStatus(res, false)
	case pm.Clearsigned:
		plain, res := pgp.VerifyClearsigned(pm.SignedPart, p.u.keyring.PublicEntities())
		pm.TextPlain = string(plain)
		p.setSigStatus(res, false)
	default:
		p.pgpStatus.Hide()
	}

	p.renderParsed(pm)
}

// renderParsed zeigt ein geparstes (ggf. entschlüsseltes) Anzeigemodell.
func (p *viewPanel) renderParsed(pm *mailmime.ParsedMessage) {
	switch {
	case pm.TextPlain != "":
		p.htmlWrap.Hide()
		p.body.SetText(strings.ReplaceAll(pm.TextPlain, "\r\n", "\n"))
		p.bodyWrap.Show()
	case pm.TextHTML != "":
		p.bodyWrap.Hide()
		segs := RenderHTML(pm.TextHTML)
		note := widget.RichTextStyleParagraph
		note.TextStyle.Italic = true
		p.htmlBody.Segments = append([]widget.RichTextSegment{
			&widget.TextSegment{Text: i18n.T("view.htmlnote"), Style: note},
		}, segs...)
		p.htmlBody.Refresh()
		p.htmlWrap.Show()
	default:
		p.htmlWrap.Hide()
		p.body.SetText("")
		p.bodyWrap.Show()
	}

	p.attachBar.Objects = nil
	if len(pm.Attachments) > 0 {
		for _, att := range pm.Attachments {
			att := att
			label := fmt.Sprintf("%s (%s)", att.Filename, humanSize(int64(len(att.Data))))
			p.attachBar.Add(widget.NewButtonWithIcon(label, theme.DownloadIcon(), func() {
				p.saveAttachment(att)
			}))
			if isPublicKeyAttachment(att) {
				p.attachBar.Add(widget.NewButtonWithIcon(i18n.T("pgp.import.attach"),
					theme.ContentAddIcon(), func() {
						p.importKeyAttachment(att)
					}))
			}
		}
		p.attachBar.Show()
		p.attachBar.Refresh()
	} else {
		p.attachBar.Hide()
	}
}

// isPublicKeyAttachment erkennt Anhänge mit öffentlichem PGP-Schlüssel:
// deklarierter Content-Type oder Armor-Block am Inhaltsanfang (fängt auch
// Clients, die .asc als text/plain schicken).
func isPublicKeyAttachment(att mailmime.Part) bool {
	if att.MediaType == "application/pgp-keys" {
		return true
	}
	return bytes.HasPrefix(bytes.TrimSpace(att.Data), []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----"))
}

// importKeyAttachment übernimmt den Schlüssel aus dem Anhang in den
// Schlüsselbund und bietet an, den Absender ins Adressbuch zu übernehmen.
func (p *viewPanel) importKeyAttachment(att mailmime.Part) {
	u := p.u
	infos, err := u.keyring.ImportPublic(att.Data)
	if err != nil {
		if strings.Contains(err.Error(), "kein neuer Schlüssel") {
			dialog.ShowInformation(i18n.T("pgp.import.attach"), i18n.T("pgp.import.exists"), u.win)
		} else {
			u.showError(err)
		}
		return
	}
	u.log.append(fmt.Sprintf(i18n.T("pgp.import.done"), len(infos)))

	// Absender anbieten: vorhandenen Kontakt um den Schlüssel ergänzen,
	// sonst den Editierdialog vorbefüllt öffnen.
	if p.current == nil {
		return
	}
	addr, err := mail.ParseAddress(p.current.From)
	if err != nil {
		return
	}
	u.showConfirmKeyboard(i18n.T("pgp.import.attach"),
		fmt.Sprintf(i18n.T("pgp.import.contact.ask"), addr.Address), func(ok bool) {
			if !ok {
				return
			}
			fpr := pickKeyForEmail(infos, addr.Address)
			if existing, found := u.contacts.FindByEmail(addr.Address); found {
				existing.KeyFingerprint = fpr
				if err := u.contacts.Update(existing.Email, existing); err != nil {
					u.showError(contactErr(err))
				}
				return
			}
			first, last := splitDisplayName(addr.Name)
			u.showContactEditDialogPrefilled(u.win, nil, contacts.Contact{
				FirstName: first, LastName: last,
				Email: addr.Address, KeyFingerprint: fpr,
			}, "", func() {})
		})
}

// splitDisplayName teilt einen Anzeigenamen grob in Vor- und Nachname.
func splitDisplayName(name string) (string, string) {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return "", ""
	}
	return fields[0], strings.Join(fields[1:], " ")
}

// setSigStatus zeigt die Signatur-/Verschlüsselungs-Statuszeile.
func (p *viewPanel) setSigStatus(res pgp.SigResult, decrypted bool) {
	var parts []string
	if decrypted {
		parts = append(parts, i18n.T("pgp.sig.decrypted"))
	}
	switch res.Status {
	case pgp.SigValid:
		parts = append(parts, fmt.Sprintf(i18n.T("pgp.sig.valid"),
			res.SignerEmail, shortFpr(res.SignerFingerprint)))
	case pgp.SigInvalid:
		parts = append(parts, i18n.T("pgp.sig.invalid"))
	case pgp.SigUnknownKey:
		parts = append(parts, fmt.Sprintf(i18n.T("pgp.sig.unknownkey"), shortFpr(res.SignerFingerprint)))
	}
	if len(parts) == 0 {
		p.pgpStatus.Hide()
		return
	}
	p.pgpStatus.SetText(strings.Join(parts, "  ·  "))
	p.pgpStatus.Show()
}

func shortFpr(fpr string) string {
	if len(fpr) > 16 {
		return fpr[len(fpr)-16:]
	}
	return fpr
}

// cachedDecryptors liefert alle sofort nutzbaren privaten Schlüssel
// (passphrasefrei oder entsperrt im Cache).
func (p *viewPanel) cachedDecryptors() pgp.EntityList {
	var out pgp.EntityList
	for _, info := range p.u.keyring.ListPrivate() {
		if e := p.u.pgpCache.Get(info.Fingerprint); e != nil {
			out = append(out, e)
			continue
		}
		if ent := p.u.keyring.FindPrivateByFingerprint(info.Fingerprint); ent != nil && !pgp.NeedsPassphrase(ent) {
			out = append(out, ent)
		}
	}
	return out
}

// tryCachedDecrypt entschlüsselt still, wenn ein nutzbarer Schlüssel vorliegt.
func (p *viewPanel) tryCachedDecrypt() bool {
	ents := p.cachedDecryptors()
	if len(ents) == 0 {
		return false
	}
	return p.decryptWith(ents, false)
}

// askDecrypt stellt die Ja/Nein-Frage (einmalig beim Öffnen).
func (p *viewPanel) askDecrypt() {
	msg := widget.NewLabel(i18n.T("pgp.decrypt.msg"))
	msg.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustomConfirm(i18n.T("pgp.decrypt.title"),
		i18n.T("pgp.decrypt.yes"), i18n.T("pgp.decrypt.no"), msg,
		func(ok bool) {
			if ok {
				p.decryptWithPrompt()
			}
		}, p.u.win)
	d.Resize(fyne.NewSize(440, 180))
	d.Show()
}

// decryptWithPrompt entsperrt (falls nötig) den eigenen Schlüssel per
// Passphrase-Dialog und entschlüsselt dann. Gefragt wird gezielt nach dem
// Schlüssel, an den die Nachricht laut Paketkopf verschlüsselt ist – nicht
// pauschal nach dem ersten im Bund (wichtig bei mehreren Identitäten).
func (p *viewPanel) decryptWithPrompt() {
	if p.tryCachedDecrypt() {
		return
	}
	if len(p.u.keyring.ListPrivate()) == 0 || p.pmOuter == nil {
		p.u.showError(fmt.Errorf("%s", i18n.T("pgp.err.nopriv.decrypt")))
		return
	}
	candidates := pgp.DecryptionCandidates(p.pmOuter.EncryptedPayload, p.u.keyring)
	if len(candidates) == 0 {
		// Kein eigener Schlüssel passt zu den Empfänger-Key-IDs.
		p.u.showError(fmt.Errorf("%s", i18n.T("pgp.err.nopriv.decrypt")))
		return
	}
	info := candidates[0]
	ent := p.u.keyring.FindPrivateByFingerprint(info.Fingerprint)
	p.u.promptPassphrase(p.u.win, ent, info, "", func(unlocked *pgp.Entity, ok bool) {
		if !ok {
			return
		}
		p.decryptWith(pgp.EntityList{unlocked}, true)
	})
}

// decryptWith führt die Entschlüsselung aus und rendert bei Erfolg neu.
// showErrors steuert, ob Fehlschläge als Dialog gemeldet werden (stiller
// Cache-Versuch vs. expliziter Nutzerwunsch).
func (p *viewPanel) decryptWith(ents pgp.EntityList, showErrors bool) bool {
	if p.pmOuter == nil || !p.pmOuter.Encrypted || p.current == nil {
		return false
	}
	plain, sig, err := pgp.Decrypt(p.pmOuter.EncryptedPayload, ents, p.u.keyring.PublicEntities())
	if err != nil {
		if showErrors {
			if err == pgp.ErrNoPrivateKey {
				p.u.showError(fmt.Errorf("%s", i18n.T("pgp.err.nopriv.decrypt")))
			} else {
				p.u.showError(fmt.Errorf("%s: %v", i18n.T("pgp.err.corrupt"), err))
			}
		}
		return false
	}
	// Das innere Entity durch dieselbe Parse-Pipeline schicken (nur Speicher).
	inner, err := mailmime.Parse(append([]byte("From: pgp@local\r\n"), plain...))
	if err != nil {
		if showErrors {
			p.u.showError(fmt.Errorf("%s: %v", i18n.T("pgp.err.corrupt"), err))
		}
		return false
	}
	p.decrypted = inner
	p.decFor = p.current.ID
	p.decSig = sig
	p.renderCurrent()
	return true
}

// deleteCurrent löscht die ausgewählte Nachricht: außerhalb des Papierkorbs
// ohne Rückfrage in den Papierkorb (rückholbar), im Papierkorb endgültig mit
// Bestätigung. Bei Outbox-Mails wird der zugehörige Spool-Job mit storniert.
func (p *viewPanel) deleteCurrent() {
	if p.current == nil {
		return
	}
	u := p.u
	m := p.current
	if m.Folder != store.Trash {
		if err := u.store.MoveToTrash(m.Folder, m.ID); err != nil {
			u.showError(err)
			return
		}
		if m.Folder == store.Outbox {
			if err := u.engine.CancelByStoreID(m.ID); err != nil {
				u.showError(err)
			}
		}
		p.clear()
		u.refresh()
		return
	}
	u.showConfirmKeyboard(i18n.T("msg.delete"), i18n.T("msg.delete.confirm"), func(ok bool) {
		if !ok {
			return
		}
		if err := u.store.Delete(m.Folder, m.ID); err != nil {
			u.showError(err)
			return
		}
		p.clear()
		u.refresh()
	})
}

// scrollBody scrollt die sichtbare Inhaltsansicht um lines Textzeilen
// (negativ = nach oben). Der Body ist EIN Label/RichText – gescrollt wird
// daher über Offset-Arithmetik mit der gemessenen Zeilenhöhe.
func (p *viewPanel) scrollBody(lines int) {
	sc := p.bodyWrap
	if p.htmlWrap.Visible() {
		sc = p.htmlWrap
	}
	lineH := fyne.MeasureText("M", theme.TextSize(), fyne.TextStyle{Monospace: true}).Height
	y := sc.Offset.Y + lineH*float32(lines)
	if max := sc.Content.MinSize().Height - sc.Size().Height; y > max {
		y = max
	}
	if y < 0 {
		y = 0
	}
	sc.ScrollToOffset(fyne.NewPos(sc.Offset.X, y))
}

// deleteSelection löscht die aktuelle Auswahl: eine Mail läuft über den
// Einzel-Pfad (deleteCurrent), mehrere immer mit Rückfrage – außerhalb des
// Papierkorbs in den Papierkorb, im Papierkorb endgültig.
func (u *UI) deleteSelection() {
	metas := u.messageList.selectedMetas()
	if len(metas) <= 1 {
		u.messageView.deleteCurrent()
		return
	}
	inTrash := u.currentFolder == store.Trash
	msg := fmt.Sprintf(i18n.T("msg.delete.multi.confirm"), len(metas))
	if inTrash {
		msg = fmt.Sprintf(i18n.T("msg.trash.delete.multi.confirm"), len(metas))
	}
	u.showConfirmKeyboard(i18n.T("msg.delete"), msg, func(ok bool) {
		if !ok {
			return
		}
		for _, m := range metas {
			var err error
			if inTrash {
				err = u.store.Delete(m.Folder, m.ID)
			} else {
				err = u.store.MoveToTrash(m.Folder, m.ID)
			}
			if err != nil {
				u.showError(err)
				break
			}
			if m.Folder == store.Outbox {
				if err := u.engine.CancelByStoreID(m.ID); err != nil {
					u.showError(err)
				}
			}
		}
		u.messageView.clear()
		u.refresh()
	})
}

// forwardSelectedAsEML öffnet eine neue Mail mit allen markierten Nachrichten
// als message/rfc822-Anhängen (Original-Rohdaten, verschlüsselte als Chiffrat).
func (u *UI) forwardSelectedAsEML() {
	metas := u.messageList.selectedMetas()
	if len(metas) == 0 {
		return
	}
	var atts []compose.Attachment
	for _, m := range metas {
		raw, err := u.store.Raw(m.Folder, m.ID)
		if err != nil {
			u.showError(err)
			return
		}
		atts = append(atts, compose.Attachment{
			Filename: sanitizeFilename(m.Subject) + ".eml",
			Data:     raw,
			MIMEType: "message/rfc822",
		})
	}
	subject := fmt.Sprintf(i18n.T("fwd.multi.subject"), len(metas))
	u.showComposeWindow(nil, &prefill{Subject: subject, Attachments: atts, FocusBody: false})
}

// sanitizeFilename macht einen Betreff als Dateinamen verwendbar.
func sanitizeFilename(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "message"
	}
	repl := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "\x00", "_")
	s = repl.Replace(s)
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60])
	}
	return s
}

// markSelectionUnread setzt alle markierten Posteingangs-Mails (Fallback:
// die angezeigte) wieder auf ungelesen. In anderen Ordnern No-op – nur dort
// haben Punkt und Zähler eine Bedeutung.
func (u *UI) markSelectionUnread() {
	if u.currentFolder != store.Inbox {
		return
	}
	metas := u.messageList.selectedMetas()
	if len(metas) == 0 && u.messageView.current != nil {
		metas = []*store.Meta{u.messageView.current}
	}
	changed := false
	for _, m := range metas {
		if m.Folder != store.Inbox || !m.Read {
			continue
		}
		if err := u.store.SetRead(m.Folder, m.ID, false); err != nil {
			u.showError(err)
			break
		}
		m.Read = false // in-memory-Meta treibt den ●-Punkt in der Liste
		changed = true
	}
	if changed {
		u.folderList.refresh()
		u.messageList.widget.Refresh()
	}
}

// restoreSelected verschiebt alle markierten Papierkorb-Nachrichten zurück in
// ihre Herkunftsordner (ohne Markierung: die angezeigte Nachricht).
func (u *UI) restoreSelected() {
	metas := u.messageList.selectedMetas()
	if len(metas) == 0 && u.messageView.current != nil {
		metas = []*store.Meta{u.messageView.current}
	}
	restored := false
	for _, m := range metas {
		if m.Folder != store.Trash {
			continue
		}
		if err := u.store.RestoreFromTrash(m.ID); err != nil {
			u.showError(err)
			break
		}
		restored = true
	}
	if restored {
		u.messageView.clear()
		u.refresh()
		u.focusZone(zoneList)
	}
}

// effectiveParsed liefert das anzuzeigende Modell: bei entschlüsselten
// Nachrichten der innere Inhalt mit den ÄUSSEREN Headern (From/To/Subject
// stehen außen), sonst das normale Parse-Ergebnis.
func (p *viewPanel) effectiveParsed() (*mailmime.ParsedMessage, bool, error) {
	if p.decrypted != nil && p.current != nil && p.decFor == p.current.ID && p.pmOuter != nil {
		merged := *p.decrypted
		merged.Header = p.pmOuter.Header
		return &merged, true, nil
	}
	pm, err := mailmime.Parse(p.raw)
	return pm, false, err
}

// displayText liefert den zitierfähigen Text der aktuellen Nachricht
// (text/plain, sonst Textfassung des HTML).
func displayText(pm *mailmime.ParsedMessage) string {
	if pm.TextPlain != "" {
		return strings.ReplaceAll(pm.TextPlain, "\r\n", "\n")
	}
	if pm.TextHTML != "" {
		return htmlToText(pm.TextHTML)
	}
	return ""
}

func decodedSubject(pm *mailmime.ParsedMessage) string {
	subj := pm.Header.Get("Subject")
	if dec, err := new(mime.WordDecoder).DecodeHeader(subj); err == nil {
		return dec
	}
	return subj
}

// composeReply öffnet das Compose-Fenster als Antwort (all = Allen antworten).
func (p *viewPanel) composeReply(all bool) {
	if p.current == nil || p.raw == nil {
		return
	}
	pm, encrypted, err := p.effectiveParsed()
	if err != nil {
		p.u.showError(err)
		return
	}
	replyTo := decodeAddressList(pm.Header.Get("Reply-To"))
	if replyTo == "" {
		replyTo = decodeAddressList(pm.Header.Get("From"))
	}
	toList := []string{replyTo}
	ccJoined := ""
	if all {
		extra := compose.FilterOwn(splitRecipients(pm.Header.Get("To")), p.u.cfg.Identities)
		toList = compose.FilterOwn(append(toList, extra...), p.u.cfg.Identities)
		if len(toList) == 0 {
			toList = []string{replyTo}
		}
		ccJoined = strings.Join(
			compose.FilterOwn(splitRecipients(pm.Header.Get("Cc")), p.u.cfg.Identities), ", ")
	}
	msgID := pm.Header.Get("Message-ID")
	refs := compose.AppendReference(strings.Fields(pm.Header.Get("References")), msgID)
	fromDisplay := pm.Header.Get("From")
	if dec, err := new(mime.WordDecoder).DecodeHeader(fromDisplay); err == nil {
		fromDisplay = dec
	}
	attribution := fmt.Sprintf(i18n.T("quote.attribution"),
		p.current.Date.Local().Format("02.01.2006 15:04"), fromDisplay)
	p.u.showComposeWindow(nil, &prefill{
		To:         strings.Join(toList, ", "),
		Cc:         ccJoined,
		Subject:    compose.ReplySubject(decodedSubject(pm)),
		Body:       compose.QuoteBody(attribution, displayText(pm)),
		InReplyTo:  msgID,
		References: refs,
		Encrypt:    encrypted, // Antwort auf Verschlüsseltes wieder verschlüsseln
		Sign:       encrypted,
		FocusBody:  true,
	})
}

// composeForward öffnet das Compose-Fenster als Weiterleitung
// (inkl. Original-Anhängen).
func (p *viewPanel) composeForward() {
	if p.current == nil || p.raw == nil {
		return
	}
	pm, _, err := p.effectiveParsed()
	if err != nil {
		p.u.showError(err)
		return
	}
	headers := [][2]string{
		{i18n.T("list.from"), pm.Header.Get("From")},
		{i18n.T("list.to"), pm.Header.Get("To")},
		{i18n.T("list.cc"), pm.Header.Get("Cc")},
		{i18n.T("list.date"), pm.Header.Get("Date")},
		{i18n.T("list.subject"), decodedSubject(pm)},
	}
	var atts []compose.Attachment
	for _, a := range pm.Attachments {
		atts = append(atts, compose.Attachment{
			Filename: a.Filename, Data: a.Data, MIMEType: a.MediaType,
		})
	}
	p.u.showComposeWindow(nil, &prefill{
		Subject:     compose.ForwardSubject(decodedSubject(pm)),
		Body:        compose.ForwardBlock(i18n.T("fwd.separator"), headers, displayText(pm)),
		Attachments: atts,
	})
}

// composeEditAsNew öffnet eine gesendete Nachricht als NEUE Nachricht im
// Editiermodus (alle Felder vorbefüllt, neue Message-ID beim Senden) –
// z. B. um eine verlorene/abgelehnte Mail erneut zu verschicken.
// Grenze: BCC ist in der Gesendet-Kopie nicht enthalten (nur Envelope).
func (p *viewPanel) composeEditAsNew() {
	if p.current == nil || p.raw == nil {
		return
	}
	pm, encrypted, err := p.effectiveParsed()
	if err != nil {
		p.u.showError(err)
		return
	}
	if pm.Encrypted && p.decrypted == nil {
		// Chiffrierte Gesendet-Kopie, noch nicht entschlüsselt: erst der
		// Entschlüsseln-Fluss, sonst würde der Chiffretext zum Body.
		dialog.ShowInformation(i18n.T("msg.editasnew"), i18n.T("msg.editasnew.locked"), p.u.win)
		return
	}
	var atts []compose.Attachment
	for _, a := range pm.Attachments {
		atts = append(atts, compose.Attachment{
			Filename: a.Filename, Data: a.Data, MIMEType: a.MediaType,
		})
	}
	p.u.showComposeWindow(nil, &prefill{
		From:        p.current.From, // eigene Identität, nackt gespeichert
		To:          decodeAddressList(pm.Header.Get("To")),
		Cc:          decodeAddressList(pm.Header.Get("Cc")),
		Subject:     decodedSubject(pm),
		Body:        displayText(pm),
		Attachments: atts,
		Sign:        p.pmOuter != nil && p.pmOuter.Signed,
		Encrypt:     encrypted,
		NoSignature: true, // alter Body enthält die Signatur ggf. schon
	})
}

// saveAttachment öffnet den Speichern-Dialog für einen Anhang.
func (p *viewPanel) saveAttachment(att mailmime.Part) {
	fd := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
		if err != nil || wc == nil {
			return
		}
		defer wc.Close()
		if _, err := wc.Write(att.Data); err != nil {
			p.u.showError(err)
			return
		}
		p.u.log.append(fmt.Sprintf("%s: %s", i18n.T("view.attach.saved"), att.Filename))
	}, p.u.win)
	fd.SetFileName(att.Filename)
	fd.Show()
}

// bodyText trennt den Body von den Headern (einfacher RFC-5322-Split).
func bodyText(raw []byte) string {
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if _, body, ok := strings.Cut(s, "\n\n"); ok {
		return body
	}
	return s
}

// ---- Statusleiste ----

type statusPanel struct {
	widget *widget.Label
}

func newStatusPanel() *statusPanel {
	l := widget.NewLabel(i18n.T("status.never"))
	l.Truncation = fyne.TextTruncateEllipsis
	return &statusPanel{widget: l}
}

// setText zeigt die einzeilige Kurzmeldung in der Statusleiste.
func (s *statusPanel) setText(msg string) {
	s.widget.SetText(msg)
}
