package main

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// InputNames holds the human-readable names for each input.
var InputNames = [numInputs]string{
	"Switch 1 (GPIO2)",
	"Switch 2 (GPIO3)",
	"Switch 3 (GPIO4)",
	"Switch 4 (GPIO5)",
	"Enc 1 Click (GPIO8)",
	"Enc 2 Click (GPIO12)",
	"Encoder 1 CW",
	"Encoder 1 CCW",
	"Encoder 2 CW",
	"Encoder 2 CCW",
}

// App holds the application state and UI components.
type App struct {
	window     fyne.Window
	device     *Device
	keyLabels  [numInputs]*widget.Label
	statusLbl  *widget.Label
	portSelect *widget.Select
	connectBtn *widget.Button
	monitorLog *widget.List
	monEntries []string
}

// NewApp creates a new App instance.
func NewApp(window fyne.Window, device *Device) *App {
	return &App{window: window, device: device}
}

// BuildUI creates the main UI layout.
func (a *App) BuildUI() fyne.CanvasObject {
	// Header
	title := widget.NewLabel("⌨ WirelessTourbox Config Tool")
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter
	a.statusLbl = widget.NewLabel("Disconnected")
	a.statusLbl.Alignment = fyne.TextAlignTrailing
	header := container.NewBorder(nil, nil, title, a.statusLbl)

	// Connection panel
	a.portSelect = widget.NewSelect(nil, nil)
	a.portSelect.PlaceHolder = "Select port..."
	a.refreshPorts()
	refreshBtn := widget.NewButton("↻", a.refreshPorts)
	refreshBtn.Importance = widget.LowImportance
	a.connectBtn = widget.NewButton("Connect", func() {
		if a.device.IsConnected() {
			a.disconnect()
		} else {
			a.connect()
		}
	})
	connRow := container.NewBorder(nil, nil,
		widget.NewLabel("Port:"),
		container.NewHBox(refreshBtn, a.connectBtn),
		a.portSelect,
	)

	// Input rows
	rows := container.NewVBox()
	for i := 0; i < numInputs; i++ {
		a.keyLabels[i] = widget.NewLabel("—")
		a.keyLabels[i].TextStyle = fyne.TextStyle{Monospace: true}
		idx := i
		changeBtn := widget.NewButton("Change", func() { a.showKeyCapture(idx) })
		row := container.NewHBox(
			widget.NewLabel(fmt.Sprintf("%d", i)),
			container.New(layout.NewGridWrapLayout(fyne.NewSize(200, 0)),
				widget.NewLabel(InputNames[i])),
			container.New(layout.NewGridWrapLayout(fyne.NewSize(150, 0)),
				a.keyLabels[i]),
			changeBtn,
		)
		rows.Add(row)
	}
	inputList := container.NewVScroll(rows)
	inputList.SetMinSize(fyne.NewSize(0, 300))

	// Reset button
	resetBtn := widget.NewButton("Reset All to Defaults", func() {
		dialog.ShowConfirm("Reset Defaults", "Reset all keys to F13-F22 defaults?",
			func(ok bool) {
				if ok {
					a.resetDefaults()
				}
			}, a.window)
	})

	// Monitor panel
	a.monitorLog = widget.NewList(
		func() int { return len(a.monEntries) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			obj.(*widget.Label).SetText(a.monEntries[id])
		},
	)
	monitorScroll := container.NewVScroll(a.monitorLog)
	monitorScroll.SetMinSize(fyne.NewSize(0, 150))
	monitorTitle := widget.NewLabel("Monitor")
	monitorTitle.TextStyle = fyne.TextStyle{Bold: true}

	content := container.NewVBox(
		header, widget.NewSeparator(),
		connRow, widget.NewSeparator(),
		inputList, widget.NewSeparator(),
		container.NewHBox(layout.NewSpacer(), resetBtn, layout.NewSpacer()),
		widget.NewSeparator(),
		monitorTitle, monitorScroll,
	)
	return container.NewPadded(content)
}

func (a *App) refreshPorts() {
	ports, _ := ListPorts()
	names := []string{}
	for _, p := range ports {
		names = append(names, p.Name)
	}
	a.portSelect.Options = names
	a.portSelect.Refresh()
}

func (a *App) connect() {
	port := a.portSelect.Selected
	if port == "" {
		dialog.ShowError(fmt.Errorf("select a port first"), a.window)
		return
	}
	if err := a.device.Connect(port); err != nil {
		dialog.ShowError(err, a.window)
		return
	}
	a.statusLbl.SetText("Connected: " + port)
	a.connectBtn.SetText("Disconnect")
	a.portSelect.Disable()
	if err := a.device.GetLayout(); err != nil {
		dialog.ShowError(err, a.window)
		a.disconnect()
		return
	}
	a.refreshLayout()
	go a.monitorLoop()
}

func (a *App) disconnect() {
	a.device.Disconnect()
	a.statusLbl.SetText("Disconnected")
	a.connectBtn.SetText("Connect")
	a.portSelect.Enable()
	for i := 0; i < numInputs; i++ {
		a.keyLabels[i].SetText("—")
	}
}

func (a *App) refreshLayout() {
	layout := a.device.GetLayoutData()
	for i := 0; i < numInputs; i++ {
		a.keyLabels[i].SetText(FormatKey(layout[i][0], layout[i][1]))
	}
}

func (a *App) resetDefaults() {
	if err := a.device.ResetDefaults(); err != nil {
		dialog.ShowError(err, a.window)
		return
	}
	a.refreshLayout()
}

func (a *App) monitorLoop() {
	ch := a.device.EventChan()
	for evt := range ch {
		name := InputNames[evt.Index]
		keyStr := FormatKey(evt.Modifier, evt.Keycode)
		entry := fmt.Sprintf("[%s] %s → %s %s",
			time.Now().Format("15:04:05"), name, keyStr, evt.Action)
		fyne.Do(func() {
			a.monEntries = append(a.monEntries, entry)
			if len(a.monEntries) > 100 {
				a.monEntries = a.monEntries[1:]
			}
			a.monitorLog.Refresh()
		})
	}
}

// showKeyCapture opens a dialog for capturing a keypress.
func (a *App) showKeyCapture(index int) {
	if !a.device.IsConnected() {
		dialog.ShowError(fmt.Errorf("connect to device first"), a.window)
		return
	}

	captureLbl := widget.NewLabel("Press any key or key combination...")
	captureLbl.Alignment = fyne.TextAlignCenter
	captureLbl.TextStyle = fyne.TextStyle{Bold: true}

	detailLbl := widget.NewLabel("")
	detailLbl.Alignment = fyne.TextAlignCenter

	statusLbl := widget.NewLabel("Waiting for keypress...")
	statusLbl.Alignment = fyne.TextAlignCenter

	var capturedMod uint8
	var capturedKeycode uint8
	var captured bool

	capture := newKeyCaptureCanvas(func(mod, keycode uint8) {
		capturedMod = mod
		capturedKeycode = keycode
		captured = true
		statusLbl.SetText("Key captured!")
		detailLbl.SetText(fmt.Sprintf("%s\nModifier: 0x%02X (%s)\nKeycode: 0x%02X (%s)",
			FormatKey(mod, keycode), mod, ModifierString(mod), keycode, HIDKeyName(keycode)))
	}, func() {
		statusLbl.SetText("Capture cancelled.")
	})

	content := container.NewVBox(
		widget.NewLabel(fmt.Sprintf("Remap: %s", InputNames[index])),
		widget.NewSeparator(),
		captureLbl, capture, statusLbl, detailLbl,
		widget.NewSeparator(),
	)

	d := dialog.NewCustomConfirm("Key Capture", "Apply", "Cancel", content,
		func(ok bool) {
			if ok && captured {
				layout := a.device.GetLayoutData()
				for i := 0; i < numInputs; i++ {
					if i != index && layout[i][0] == capturedMod && layout[i][1] == capturedKeycode {
						dialog.ShowError(fmt.Errorf("%s is already mapped to %s (index %d)",
							FormatKey(capturedMod, capturedKeycode), InputNames[i], i), a.window)
						return
					}
				}
				if err := a.device.SetKey(index, capturedMod, capturedKeycode); err != nil {
					dialog.ShowError(err, a.window)
					return
				}
				a.refreshLayout()
			}
		}, a.window)
	d.Show()
	d.Resize(fyne.NewSize(400, 300))
}

// keyCaptureCanvas is a custom widget that captures key events.
type keyCaptureCanvas struct {
	widget.BaseWidget
	onCapture func(mod, keycode uint8)
	onCancel  func()
	modState  uint8
	focused   bool
}

func newKeyCaptureCanvas(onCapture func(mod, keycode uint8), onCancel func()) *keyCaptureCanvas {
	k := &keyCaptureCanvas{onCapture: onCapture, onCancel: onCancel}
	k.ExtendBaseWidget(k)
	return k
}

func (k *keyCaptureCanvas) CreateRenderer() fyne.WidgetRenderer {
	label := widget.NewLabel("Click here, then press a key...")
	label.Alignment = fyne.TextAlignCenter
	bg := canvas.NewRectangle(theme.InputBackgroundColor())
	return &keyCaptureRenderer{canvas: k, label: label, bg: bg, objects: []fyne.CanvasObject{bg, label}}
}

func (k *keyCaptureCanvas) Tapped(_ *fyne.PointEvent) {
	canvas := fyne.CurrentApp().Driver().CanvasForObject(k)
	if canvas != nil {
		canvas.Focus(k)
	}
}

func (k *keyCaptureCanvas) FocusGained() {
	k.focused = true
	k.Refresh()
}

func (k *keyCaptureCanvas) FocusLost() {
	k.focused = false
	k.Refresh()
}

func (k *keyCaptureCanvas) TypedRune(r rune) {
	keycode, ok := RuneToHID[r]
	if !ok {
		return
	}
	mod := k.modState
	if r >= 'A' && r <= 'Z' {
		mod |= MOD_LSHIFT
	}
	k.onCapture(mod, keycode)
	k.modState = 0
}

func (k *keyCaptureCanvas) TypedKey(event *fyne.KeyEvent) {
	if event.Name == fyne.KeyEscape {
		k.onCancel()
		return
	}
	keycode, ok := FyneToHID[event.Name]
	if !ok {
		return
	}
	// Query current modifier state from the driver (same pattern as Fyne's Entry widget)
	var mod uint8
	if dd, ok := fyne.CurrentApp().Driver().(desktop.Driver); ok {
		mods := dd.CurrentKeyModifiers()
		if mods&fyne.KeyModifierControl != 0 {
			mod |= MOD_LCTRL
		}
		if mods&fyne.KeyModifierShift != 0 {
			mod |= MOD_LSHIFT
		}
		if mods&fyne.KeyModifierAlt != 0 {
			mod |= MOD_LALT
		}
	}
	k.onCapture(mod, keycode)
}

// builtinShortcuts maps Fyne built-in shortcut names to HID keycodes.
// Fyne intercepts common Ctrl+key combinations (Ctrl+C → Copy, etc.)
// and dispatches them as specific shortcut types instead of CustomShortcut.
var builtinShortcuts = map[string]uint8{
	"SelectAll":   HID_KEY_A,
	"Copy":        HID_KEY_C,
	"Paste":       HID_KEY_V,
	"Cut":         HID_KEY_X,
	"Undo":        HID_KEY_Z,
	"Redo":        HID_KEY_Y,
	"Find":        HID_KEY_F,
	"New":         HID_KEY_N,
	"Open":        HID_KEY_O,
	"Save":        HID_KEY_S,
	"Print":       HID_KEY_P,
	"CloseWindow": HID_KEY_W,
	"Quit":        HID_KEY_Q,
}

// TypedShortcut implements fyne.Shortcutable to capture Ctrl/Alt/Super+key combinations.
// Fyne intercepts these as shortcuts before they reach TypedKey.
func (k *keyCaptureCanvas) TypedShortcut(shortcut fyne.Shortcut) {
	name := shortcut.ShortcutName()

	// Check for Fyne built-in shortcuts (Ctrl+C → Copy, etc.)
	if keycode, ok := builtinShortcuts[name]; ok {
		k.onCapture(MOD_LCTRL, keycode)
		return
	}

	// Check for CustomShortcut (Alt+key, Super+key, non-standard Ctrl+key)
	cs, ok := shortcut.(*desktop.CustomShortcut)
	if !ok {
		return
	}
	keycode, ok := FyneToHID[cs.KeyName]
	if !ok {
		return
	}
	var mod uint8
	if cs.Modifier&fyne.KeyModifierControl != 0 {
		mod |= MOD_LCTRL
	}
	if cs.Modifier&fyne.KeyModifierShift != 0 {
		mod |= MOD_LSHIFT
	}
	if cs.Modifier&fyne.KeyModifierAlt != 0 {
		mod |= MOD_LALT
	}
	k.onCapture(mod, keycode)
}

type keyCaptureRenderer struct {
	canvas  *keyCaptureCanvas
	label   *widget.Label
	bg      *canvas.Rectangle
	objects []fyne.CanvasObject
}

func (r *keyCaptureRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))
	r.label.Resize(size)
	r.label.Move(fyne.NewPos(0, 0))
}

func (r *keyCaptureRenderer) MinSize() fyne.Size {
	return fyne.NewSize(300, 50)
}

func (r *keyCaptureRenderer) Refresh() {
	if r.canvas.focused {
		r.bg.FillColor = theme.FocusColor()
	} else {
		r.bg.FillColor = theme.InputBackgroundColor()
	}
	r.bg.Refresh()
	r.label.Refresh()
}

func (r *keyCaptureRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *keyCaptureRenderer) Destroy()              {}
