package main

import (
	"fmt"
	"os"

	"github.com/cfluegel/uucpmail-client/pkg/ui"
)

func runGUI() {
	if err := ui.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
}
