package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
)

func main() {
	a := app.New()
	w := a.NewWindow("WirelessTourbox Config Tool")
	w.Resize(fyne.NewSize(700, 600))
	w.SetFixedSize(true)

	device := &Device{}
	ui := NewApp(w, device)
	w.SetContent(ui.BuildUI())

	w.ShowAndRun()
}
