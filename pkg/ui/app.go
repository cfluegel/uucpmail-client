// Package ui ist die Fyne-Oberfläche des uucpmail-Clients (Winlink-Layout).
package ui

import (
	"fmt"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/lang"
	"fyne.io/fyne/v2/widget"

	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/contacts"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"github.com/cfluegel/uucpmail-client/pkg/pgp"
	"github.com/cfluegel/uucpmail-client/pkg/pgpcard"
	"github.com/cfluegel/uucpmail-client/pkg/store"
	syncengine "github.com/cfluegel/uucpmail-client/pkg/sync"
	"github.com/cfluegel/uucpmail-client/pkg/uucp"
)

// UI hält den gemeinsamen Zustand der Oberfläche.
type UI struct {
	app    fyne.App
	win    fyne.Window
	cfg    *config.Config
	store  *store.Store
	spool  *uucp.Spool
	engine *syncengine.Engine

	folderList    *folderPanel
	messageList   *messagePanel
	messageView   *viewPanel
	statusBar     *statusPanel
	wotAction     *widget.ToolbarAction // Web-of-Trust-Knopf, nur mit eigenem Schlüssel aktiv
	listZone      *focusZone
	contentZone   *focusZone
	activeZone    zoneID
	state         *uiState
	touchDlg      dialog.Dialog // Touch-Hinweis-Popup, nur im Fyne-Thread (fyne.Do)
	log           sessionLog
	keyring       *pgp.Keyring
	pgpCache      *pgp.Cache
	sealer        *pgp.MailSealer
	pgpCardKey    *pgpcard.CardKey // nil = kein kartengestützter Schlüssel aktiv
	activeWin     fyne.Window      // vorderstes Fenster (z.B. Compose) für Dialoge; nil = Hauptfenster
	contacts      *contacts.Book
	syncRunning   bool
	currentFolder string
}

// Run startet die GUI (blockiert bis zum Fenster-Schließen).
func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	i18n.Init(cfg.Language)

	mailDir, err := config.MailDir()
	if err != nil {
		return err
	}
	st, err := store.Open(mailDir)
	if err != nil {
		return err
	}
	defer st.Close()
	spoolDir, err := config.SpoolDir()
	if err != nil {
		return err
	}
	spool, err := uucp.OpenSpool(spoolDir)
	if err != nil {
		return err
	}

	// Fynes deutschem Sprachpaket (v2.8.2) fehlt die Select-Platzhalter-
	// Übersetzung – nachreichen, sonst loggt jedes widget.Select einen
	// "Translation failure".
	_ = lang.AddTranslationsForLocale([]byte(`{"(Select one)": "(Bitte wählen)"}`), "de")

	u := &UI{
		app:           app.NewWithID("de.uucpmail.client"),
		cfg:           cfg,
		store:         st,
		spool:         spool,
		currentFolder: store.Inbox,
	}
	// Papierkorb-Aufräumung beim Start (0 = nie automatisch leeren).
	if cfg.TrashDays > 0 {
		if n, err := st.PurgeTrash(time.Duration(cfg.TrashDays) * 24 * time.Hour); err != nil {
			fmt.Fprintln(os.Stderr, "trash purge:", err)
		} else if n > 0 {
			u.log.append(fmt.Sprintf(i18n.T("trash.purged"), n, cfg.TrashDays))
		}
	}
	pubPath, err := config.PubringPath()
	if err != nil {
		return err
	}
	secPath, err := config.SecringPath()
	if err != nil {
		return err
	}
	keyring, err := pgp.LoadKeyring(pubPath, secPath)
	if err != nil {
		return err
	}
	u.keyring = keyring
	if vPath, err := config.PGPVerifiedPath(); err == nil {
		if err := keyring.LoadVerified(vPath); err != nil {
			// Beschädigte Prüfvermerke blockieren den Start nicht.
			fmt.Fprintln(os.Stderr, "pgp verified marks not readable:", err)
		}
	}
	u.pgpCache = pgp.NewCache(pgpCacheTTL(cfg.PGPCacheMin))
	cPath, err := config.ContactsPath()
	if err != nil {
		return err
	}
	if u.contacts, err = contacts.Load(cPath); err != nil {
		// Beschädigtes Adressbuch blockiert den Start nicht.
		fmt.Fprintln(os.Stderr, "contacts not readable:", err)
		u.contacts, _ = contacts.Load(cPath + ".broken")
	}

	u.sealer = &pgp.MailSealer{Keyring: keyring, Cache: u.pgpCache}
	u.engine = &syncengine.Engine{Cfg: cfg, Store: st, Spool: spool, PGP: u.sealer}
	u.applyTheme()
	u.state = loadUIState(u.app.Preferences())
	u.win = u.app.NewWindow(i18n.T("app.title"))
	u.buildMainWindow()
	u.installSignerSource()
	u.installCardSource()
	u.cardExpiryWarning()

	if !cfg.Complete() {
		u.showSettings(true)
	}
	u.win.ShowAndRun()
	return nil
}

// showError zeigt einen Fehlerdialog.
func (u *UI) showError(err error) {
	dialog.ShowError(err, u.win)
}

// refresh lädt Ordnerzähler und die Liste des aktiven Ordners neu.
func (u *UI) refresh() {
	u.folderList.refresh()
	u.messageList.load(u.currentFolder)
}

// dialogParent liefert das Fenster, auf dem Dialoge erscheinen sollen: das
// aktive Fenster (z.B. das Compose-Fenster), sonst das Hauptfenster. Wichtig
// für den Karten-PIN-Dialog beim Senden – sonst erschiene er hinter dem
// Compose-Fenster und das Fenster wirkte eingefroren.
func (u *UI) dialogParent() fyne.Window {
	if u.activeWin != nil {
		return u.activeWin
	}
	return u.win
}

// saveConfig persistiert die Config und meldet Fehler im Dialog.
func (u *UI) saveConfig() {
	if err := u.cfg.Save(); err != nil {
		u.showError(i18n.Wrap(err, "err.ui.savecfg"))
	}
}
