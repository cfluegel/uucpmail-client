// Package ui ist die Fyne-Oberfläche des uucpmail-Clients (Winlink-Layout).
package ui

import (
	"fmt"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/lang"

	"github.com/cfluegel/uucpmail-client/pkg/config"
	"github.com/cfluegel/uucpmail-client/pkg/contacts"
	"github.com/cfluegel/uucpmail-client/pkg/i18n"
	"github.com/cfluegel/uucpmail-client/pkg/pgp"
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
	log           sessionLog
	keyring       *pgp.Keyring
	pgpCache      *pgp.Cache
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
	u.pgpCache = pgp.NewCache(pgpCacheTTL(cfg.PGPCacheMin))
	cPath, err := config.ContactsPath()
	if err != nil {
		return err
	}
	if u.contacts, err = contacts.Load(cPath); err != nil {
		// Beschädigtes Adressbuch blockiert den Start nicht.
		fmt.Fprintln(os.Stderr, "adressbuch nicht lesbar:", err)
		u.contacts, _ = contacts.Load(cPath + ".broken")
	}

	u.engine = &syncengine.Engine{Cfg: cfg, Store: st, Spool: spool,
		PGP: &pgp.MailSealer{Keyring: keyring, Cache: u.pgpCache}}
	u.applyTheme()
	u.win = u.app.NewWindow(i18n.T("app.title"))
	u.buildMainWindow()

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

// saveConfig persistiert die Config und meldet Fehler im Dialog.
func (u *UI) saveConfig() {
	if err := u.cfg.Save(); err != nil {
		u.showError(fmt.Errorf("konfiguration speichern: %w", err))
	}
}
