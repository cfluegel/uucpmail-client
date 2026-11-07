package ui

import (
	"testing"

	"fyne.io/fyne/v2/test"
)

// Nach dem Schließen eines Compose-Fensters müssen Dialoge (z.B. die
// Karten-PIN beim Entschlüsseln) wieder im Hauptfenster erscheinen.
func TestDialogParentAfterComposeClosed(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	u := &UI{win: app.NewWindow("main")}
	a, b := app.NewWindow("a"), app.NewWindow("b")

	u.activeWin = a
	u.activeWin = b // zweites Compose-Fenster
	u.composeClosed(a)
	if u.dialogParent() != b {
		t.Fatal("offenes Fenster b muss Dialog-Elternteil bleiben")
	}
	u.composeClosed(b)
	if u.dialogParent() != u.win {
		t.Fatal("nach dem Schließen: Hauptfenster erwartet")
	}
}
