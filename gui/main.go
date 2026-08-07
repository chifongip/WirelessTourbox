package main

import (
	"fyne.io/fyne/v2/app"
)

func main() {
	a := app.New()
	w := a.NewWindow("WirelessTourbox Config Tool")
	w.Resize(defaultCompactWindowSize)

	device := &Device{}
	ui := NewApp(w, device)
	w.SetContent(ui.BuildUI())
	w.SetCloseIntercept(func() {
		ui.Close()
		a.Quit()
	})

	w.ShowAndRun()
}
