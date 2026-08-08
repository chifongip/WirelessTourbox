package main

import (
	"testing"

	fynetest "fyne.io/fyne/v2/test"
)

func TestMainUIFitsCompactWindow(t *testing.T) {
	testApp := fynetest.NewApp()
	defer testApp.Quit()
	window := testApp.NewWindow("WirelessTourbox test")
	ui := NewApp(window, &Device{})
	content := ui.BuildUI()
	defer ui.Close()

	minimum := content.MinSize()
	if minimum.Width > defaultCompactWindowSize.Width || minimum.Height > defaultCompactWindowSize.Height {
		t.Fatalf("UI minimum %v exceeds compact window %v", minimum, defaultCompactWindowSize)
	}
}
