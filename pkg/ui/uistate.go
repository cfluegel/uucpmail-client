package ui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"

	"github.com/cfluegel/uucpmail-client/pkg/config"
)

// uiState persistiert Fenster-/Listen-Zustand im Datenverzeichnis
// (ui_state.json) statt in Fynes eigenem Preferences-Speicher – damit der
// Zustand im portablen Modus mitwandert und keine Spuren auf fremden
// Rechnern bleiben. Fehlt die Datei, werden die Startwerte einmalig aus den
// alten Fyne-Prefs übernommen (Migration), geschrieben wird nur noch hier.
type uiState struct {
	SplitOffset float64 `json:"split_offset"`
	WinW        float64 `json:"win_width"`
	WinH        float64 `json:"win_height"`
	Fullscreen  bool    `json:"fullscreen"`
	ColFrom     float64 `json:"col_from"`
	ColTo       float64 `json:"col_to"`
	ColDate     float64 `json:"col_date"`
	SortDateAsc bool    `json:"sort_date_asc"`
}

func uiStatePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ui_state.json"), nil
}

// loadUIState liest ui_state.json; fehlt sie, kommen die Werte aus den
// bisherigen Fyne-Prefs (bzw. deren Defaults).
func loadUIState(prefs fyne.Preferences) *uiState {
	st := &uiState{
		SplitOffset: prefs.FloatWithFallback("win.split", 0.45),
		WinW:        prefs.FloatWithFallback("win.width", 1000),
		WinH:        prefs.FloatWithFallback("win.height", 680),
		Fullscreen:  prefs.BoolWithFallback("win.fullscreen", false),
		ColFrom:     prefs.FloatWithFallback("col.from", 200),
		ColTo:       prefs.FloatWithFallback("col.to", 180),
		ColDate:     prefs.FloatWithFallback("col.date", 130),
		SortDateAsc: prefs.BoolWithFallback("sort.dateAsc", false),
	}
	p, err := uiStatePath()
	if err != nil {
		return st
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return st // Migration: Fyne-Prefs-Werte gelten
	}
	_ = json.Unmarshal(data, st) // defekte Datei → Fallback-Werte bleiben
	return st
}

// save schreibt den Zustand atomar (tmp+rename, wie config.Save).
func (st *uiState) save() {
	p, err := uiStatePath()
	if err != nil {
		return
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}
