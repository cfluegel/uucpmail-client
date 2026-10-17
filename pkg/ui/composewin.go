package ui

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/compose"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	mailmime "github.com/cfluegel/uucpmail-client/pkg/mime"
	"github.com/cfluegel/uucpmail-client/pkg/store"
)

// prefill belegt das Compose-Fenster vor (Antworten/Weiterleiten/Entwurf).
type prefill struct {
	From        string
	To, Cc, Bcc string // Komma-separiert, wie in den Eingabefeldern
	Subject     string
	Body        string
	InReplyTo   string
	References  []string
	Attachments []compose.Attachment
	Sign        bool
	Encrypt     bool
	FocusBody   bool
}

// showCompose öffnet das Fenster für eine neue Nachricht.
func (u *UI) showCompose() { u.showComposeWindow(nil, nil) }

// showComposeDraft öffnet einen vorhandenen Entwurf im Editiermodus.
func (u *UI) showComposeDraft(m *store.Meta) {
	raw, err := u.store.Raw(store.Drafts, m.ID)
	if err != nil {
		u.showError(err)
		return
	}
	pf := &prefill{Subject: m.Subject}
	if pm, err := mailmime.Parse(raw); err == nil {
		pf.From = pm.Header.Get("From")
		pf.To = pm.Header.Get("To")
		pf.Cc = pm.Header.Get("Cc")
		pf.Bcc = pm.Header.Get("X-Uucpmail-Bcc")
		pf.Subject = pm.Header.Get("Subject")
		if dec, err := new(mime.WordDecoder).DecodeHeader(pf.Subject); err == nil {
			pf.Subject = dec
		}
		pgpFlags := pm.Header.Get("X-Uucpmail-PGP")
		pf.Sign = strings.Contains(pgpFlags, "sign")
		pf.Encrypt = strings.Contains(pgpFlags, "encrypt")
		pf.InReplyTo = pm.Header.Get("In-Reply-To")
		if refs := strings.Fields(pm.Header.Get("References")); len(refs) > 0 {
			pf.References = refs
		}
		pf.Body = strings.ReplaceAll(pm.TextPlain, "\r\n", "\n")
		for _, att := range pm.Attachments {
			pf.Attachments = append(pf.Attachments, compose.Attachment{
				Filename: att.Filename, Data: att.Data, MIMEType: att.MediaType,
			})
		}
	}
	u.showComposeWindow(m, pf)
}

// showComposeWindow ist das Compose-Fenster; draft != nil = Entwurf bearbeiten,
// pf != nil = Felder vorbelegen (Entwurf, Antworten, Weiterleiten).
func (u *UI) showComposeWindow(draft *store.Meta, pf *prefill) {
	win := u.app.NewWindow(i18n.T("compose.title"))
	win.Resize(fyne.NewSize(720, 620))

	fromSelect := widget.NewSelect(u.cfg.Identities, nil)
	if def, err := u.cfg.Identity(""); err == nil {
		fromSelect.SetSelected(def)
	}
	toEntry := widget.NewEntry()
	toEntry.SetPlaceHolder(i18n.T("compose.to"))
	ccEntry := widget.NewEntry()
	ccEntry.SetPlaceHolder(i18n.T("compose.cc"))
	bccEntry := widget.NewEntry()
	bccEntry.SetPlaceHolder(i18n.T("compose.bcc"))
	subjectEntry := widget.NewEntry()
	subjectEntry.SetPlaceHolder(i18n.T("compose.subject"))
	body := widget.NewMultiLineEntry()
	body.Wrapping = fyne.TextWrapWord
	body.TextStyle.Monospace = true

	// BCC ist eingeklappt, bis es gebraucht wird.
	bccRow := container.NewStack(bccEntry)
	bccRow.Hide()
	bccToggle := widget.NewButton(i18n.T("compose.bcc.show"), nil)
	revealBcc := func() {
		bccRow.Show()
		bccToggle.Hide()
	}
	bccToggle.OnTapped = func() {
		revealBcc()
		win.Canvas().Focus(bccEntry)
	}
	abBtn := widget.NewButton(i18n.T("compose.addressbook"), func() {
		u.showAddressPicker(win, toEntry, ccEntry, bccEntry, revealBcc)
	})

	// PGP-Toggles (unabhängig, kombinierbar); Zustand über Importance sichtbar.
	signOn, encryptOn := false, false
	signBtn := widget.NewButton(i18n.T("compose.pgp.sign"), nil)
	encryptBtn := widget.NewButton(i18n.T("compose.pgp.encrypt"), nil)
	setToggle := func(btn *widget.Button, on bool) {
		if on {
			btn.Importance = widget.HighImportance
		} else {
			btn.Importance = widget.MediumImportance
		}
		btn.Refresh()
	}
	signBtn.OnTapped = func() { signOn = !signOn; setToggle(signBtn, signOn) }
	encryptBtn.OnTapped = func() { encryptOn = !encryptOn; setToggle(encryptBtn, encryptOn) }

	// Anhänge + Größenwarnung.
	var attachments []compose.Attachment
	attachList := container.NewVBox()
	sizeWarn := widget.NewLabel("")
	sizeWarn.Hide()

	dirty := false
	markDirty := func(string) { dirty = true }

	buildMessage := func() *compose.Message {
		return &compose.Message{
			Identity:    fromSelect.Selected,
			To:          splitRecipients(toEntry.Text),
			Cc:          splitRecipients(ccEntry.Text),
			Bcc:         splitRecipients(bccEntry.Text),
			Subject:     subjectEntry.Text,
			Body:        body.Text,
			NodeName:    u.cfg.NodeName,
			InReplyTo:   prefillStr(pf, func(p *prefill) string { return p.InReplyTo }),
			References:  prefillRefs(pf),
			Attachments: attachments,
			Sign:        signOn,
			Encrypt:     encryptOn,
		}
	}

	updateSizeWarn := func() {
		est := buildMessage().EstimatedWireSize()
		if max := u.cfg.ServiceProfile.MaxMessageBytes; max > 0 && est > max {
			sizeWarn.SetText(fmt.Sprintf(i18n.T("compose.attach.toolarge"), max))
			sizeWarn.Show()
		} else {
			sizeWarn.Hide()
		}
	}

	var refreshAttachList func()
	refreshAttachList = func() {
		attachList.Objects = nil
		for i, att := range attachments {
			i := i
			label := fmt.Sprintf("%s (%s)", att.Filename, humanSize(int64(len(att.Data))))
			remove := widget.NewButton("✕", func() {
				attachments = append(attachments[:i], attachments[i+1:]...)
				dirty = true
				refreshAttachList()
				updateSizeWarn()
			})
			attachList.Add(container.NewBorder(nil, nil, nil, remove, widget.NewLabel(label)))
		}
		attachList.Refresh()
	}

	attachBtn := widget.NewButton(i18n.T("compose.attach"), func() {
		fd := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
			if err != nil || rc == nil {
				return
			}
			defer rc.Close()
			data, err := readAll(rc)
			if err != nil {
				u.showError(err)
				return
			}
			name := rc.URI().Name()
			attachments = append(attachments, compose.Attachment{
				Filename: name, Data: data, MIMEType: detectMIME(name, data),
			})
			dirty = true
			refreshAttachList()
			updateSizeWarn()
		}, win)
		fd.Show()
	})

	// Prefill anwenden (vor dem Dirty-Tracking).
	if pf != nil {
		if pf.From != "" {
			fromSelect.SetSelected(pf.From)
		}
		toEntry.SetText(pf.To)
		ccEntry.SetText(pf.Cc)
		if pf.Bcc != "" {
			bccEntry.SetText(pf.Bcc)
			bccRow.Show()
			bccToggle.Hide()
		}
		subjectEntry.SetText(pf.Subject)
		body.SetText(pf.Body)
		signOn, encryptOn = pf.Sign, pf.Encrypt
		setToggle(signBtn, signOn)
		setToggle(encryptBtn, encryptOn)
		attachments = append(attachments, pf.Attachments...)
		refreshAttachList()
		updateSizeWarn()
	}
	toEntry.OnChanged = markDirty
	ccEntry.OnChanged = markDirty
	bccEntry.OnChanged = markDirty
	subjectEntry.OnChanged = markDirty
	body.OnChanged = func(string) { dirty = true }

	// saveDraft legt den Entwurf an bzw. aktualisiert ihn (gleiche ID).
	draftID := ""
	if draft != nil {
		draftID = draft.ID
	}
	saveDraft := func() error {
		msg := buildMessage()
		msg.Date = time.Now()
		raw := msg.BuildDraft("draft")
		if draftID == "" {
			meta, err := u.store.Add(store.Drafts, raw, msg.Identity, true)
			if err != nil {
				return err
			}
			draftID = meta.ID
		} else if _, err := u.store.Update(store.Drafts, draftID, raw); err != nil {
			return err
		}
		dirty = false
		u.refresh()
		return nil
	}

	// Autospeichern im konfigurierten Intervall (nur wenn geändert).
	interval := u.cfg.DraftAutosave
	if interval <= 0 {
		interval = 10
	}
	stopAutosave := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Duration(interval) * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stopAutosave:
				return
			case <-ticker.C:
				if dirty {
					fyne.Do(func() {
						if err := saveDraft(); err != nil {
							u.showError(err)
						}
					})
				}
			}
		}
	}()
	win.SetOnClosed(func() { close(stopAutosave) })

	// Markdown-Toolbar: Snippets als Text an der Cursorposition einfügen.
	// Die PGP-Toggles sitzen abgesetzt (vertikaler Strich) am Zeilenende –
	// nur sichtbar, wenn ein eigener PGP-Schlüssel existiert.
	var mdButtons []fyne.CanvasObject
	for _, sn := range compose.MarkdownSnippets {
		sn := sn
		mdButtons = append(mdButtons, widget.NewButton(i18n.T(sn.LabelKey), func() {
			text, cursor := insertAtCursor(body, sn)
			body.SetText(text)
			moveCursor(body, cursor)
			win.Canvas().Focus(body)
		}))
	}
	if len(u.keyring.ListPrivate()) > 0 {
		mdButtons = append(mdButtons, widget.NewSeparator(), signBtn, encryptBtn)
	}

	doEnqueue := func() {
		msg := buildMessage()
		if _, err := u.engine.Enqueue(msg); err != nil {
			u.showError(err)
			return
		}
		if draftID != "" {
			if err := u.store.Delete(store.Drafts, draftID); err != nil {
				u.showError(err)
			}
		}
		u.refresh()
		win.Close()
	}
	sendBtn := widget.NewButton(i18n.T("compose.send"), func() {
		// PGP-Vorprüfungen: Empfänger-Keys und entsperrter eigener Schlüssel.
		if encryptOn {
			msg := buildMessage()
			if missing := u.keyring.MissingRecipients(msg.EnvelopeRecipients()); len(missing) > 0 {
				dialog.ShowError(fmt.Errorf(i18n.T("pgp.err.missingkeys"),
					strings.Join(missing, ", ")), win)
				return
			}
		}
		if signOn {
			u.ensureUnlockedSigner(win, fromSelect.Selected, func(ok bool) {
				if ok {
					doEnqueue()
				}
			})
			return
		}
		doEnqueue()
	})
	sendBtn.Importance = widget.HighImportance
	saveBtn := widget.NewButton(i18n.T("compose.save"), func() {
		if err := saveDraft(); err != nil {
			u.showError(err)
		}
	})
	cancelBtn := widget.NewButton(i18n.T("compose.cancel"), win.Close)

	form := container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabel(i18n.T("compose.from")),
			container.NewHBox(abBtn, bccToggle), fromSelect),
		toEntry,
		ccEntry,
		bccRow,
		subjectEntry,
		container.NewHBox(mdButtons...),
	)
	bottom := container.NewVBox(
		container.NewBorder(nil, nil, attachBtn, nil, sizeWarn),
		attachList,
		container.NewCenter(container.NewHBox(cancelBtn, saveBtn, sendBtn)),
	)
	win.SetContent(container.NewBorder(form, bottom, nil, nil, body))
	if pf != nil && pf.FocusBody {
		win.Canvas().Focus(body)
		moveCursor(body, 0)
	} else {
		win.Canvas().Focus(toEntry)
	}
	win.Show()
}

func prefillStr(pf *prefill, get func(*prefill) string) string {
	if pf == nil {
		return ""
	}
	return get(pf)
}

func prefillRefs(pf *prefill) []string {
	if pf == nil {
		return nil
	}
	return pf.References
}

// detectMIME ermittelt den Content-Type: Endung → Inhalts-Sniffing → octet-stream.
func detectMIME(name string, data []byte) string {
	if t := mime.TypeByExtension(filepath.Ext(name)); t != "" {
		return t
	}
	if t := http.DetectContentType(firstN(data, 512)); t != "" {
		return t
	}
	return "application/octet-stream"
}

func firstN(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func readAll(rc fyne.URIReadCloser) ([]byte, error) {
	return io.ReadAll(rc)
}

// insertAtCursor berechnet den neuen Text samt Cursorposition.
func insertAtCursor(e *widget.Entry, sn compose.MarkdownSnippet) (string, int) {
	cursor := cursorOffset(e)
	return compose.InsertSnippet(e.Text, cursor, sn)
}

// cursorOffset rechnet Zeile/Spalte des Entry in einen Byte-Offset um.
func cursorOffset(e *widget.Entry) int {
	lines := strings.Split(e.Text, "\n")
	offset := 0
	for i := 0; i < e.CursorRow && i < len(lines); i++ {
		offset += len(lines[i]) + 1
	}
	if e.CursorRow < len(lines) {
		col := e.CursorColumn
		if col > len(lines[e.CursorRow]) {
			col = len(lines[e.CursorRow])
		}
		offset += col
	}
	return offset
}

// moveCursor setzt den Cursor auf den Byte-Offset.
func moveCursor(e *widget.Entry, offset int) {
	if offset > len(e.Text) {
		offset = len(e.Text)
	}
	row, col := 0, 0
	for _, r := range e.Text[:offset] {
		if r == '\n' {
			row++
			col = 0
		} else {
			col++
		}
	}
	e.CursorRow = row
	e.CursorColumn = col
	e.Refresh()
}

func splitRecipients(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' }) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
