package main

import (
	"fmt"
	"image/color"
	"sync"
	"time"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var InputNames = [numInputs]string{
	"Switch 1 (GPIO2)", "Switch 2 (GPIO3)", "Switch 3 (GPIO4)",
	"Switch 4 (GPIO5)", "Enc 1 Click (GPIO8)", "Enc 2 Click (GPIO12)",
	"Encoder 1 CW", "Encoder 1 CCW", "Encoder 2 CW", "Encoder 2 CCW",
}

type App struct {
	window      fyne.Window
	device      *Device
	keyLabels   [numInputs]*widget.Label
	changeBtns  [numInputs]*widget.Button
	statusLbl   *widget.Label
	portSelect  *widget.Select
	connectBtn  *widget.Button
	refreshBtn  *widget.Button
	resetBtn    *widget.Button
	monitorLog  *widget.List
	monEntries  []string
	busy        bool
	stopRefresh chan struct{}
	closeOnce   sync.Once
}

var defaultCompactWindowSize = fyne.NewSize(760, 640)

func NewApp(window fyne.Window, device *Device) *App {
	return &App{window: window, device: device, stopRefresh: make(chan struct{})}
}

func (a *App) BuildUI() fyne.CanvasObject {
	title := widget.NewLabel("⌨ WirelessTourbox Config Tool")
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter
	a.statusLbl = widget.NewLabel("Disconnected")
	a.statusLbl.Alignment = fyne.TextAlignTrailing
	a.statusLbl.Truncation = fyne.TextTruncateEllipsis
	header := container.NewBorder(nil, nil, nil, a.statusLbl, title)

	a.portSelect = widget.NewSelect(nil, nil)
	a.portSelect.PlaceHolder = "Select port..."
	a.refreshBtn = widget.NewButton("↻", a.refreshPorts)
	a.refreshBtn.Importance = widget.LowImportance
	a.connectBtn = widget.NewButton("Connect", func() {
		if a.device.IsConnected() {
			a.disconnect()
		} else {
			a.connect()
		}
	})
	connRow := container.NewBorder(nil, nil, widget.NewLabel("Port:"),
		container.NewHBox(a.refreshBtn, a.connectBtn), a.portSelect)

	rows := container.NewVBox()
	for i := 0; i < numInputs; i++ {
		a.keyLabels[i] = widget.NewLabel("—")
		a.keyLabels[i].TextStyle = fyne.TextStyle{Monospace: true}
		a.keyLabels[i].Truncation = fyne.TextTruncateEllipsis
		inputLabel := widget.NewLabel(InputNames[i])
		inputLabel.Truncation = fyne.TextTruncateEllipsis
		indexLabel := widget.NewLabel(fmt.Sprintf("%d", i))
		indexLabel.Alignment = fyne.TextAlignCenter
		index := i
		a.changeBtns[i] = widget.NewButton("Edit", func() { a.showKeyEditor(index) })
		rows.Add(mappingRow(indexLabel, inputLabel, a.keyLabels[i], a.changeBtns[i]))
	}
	inputList := container.NewVScroll(rows)
	inputList.SetMinSize(fyne.NewSize(0, 230))
	mappingHeader := mappingRow(
		widget.NewLabelWithStyle("#", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Input", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Mapping", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Action", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
	)

	a.resetBtn = widget.NewButton("Reset All to Defaults", func() {
		dialog.ShowConfirm("Reset Defaults", "Reset all keys to F13–F22 defaults?", func(ok bool) {
			if ok {
				a.resetDefaults()
			}
		}, a.window)
	})

	a.monitorLog = widget.NewList(
		func() int { return len(a.monEntries) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, object fyne.CanvasObject) {
			object.(*widget.Label).SetText(a.monEntries[id])
		},
	)
	clearBtn := widget.NewButton("Clear", func() {
		a.monEntries = nil
		a.monitorLog.Refresh()
	})
	clearBtn.Importance = widget.LowImportance
	monitorHeader := container.NewBorder(nil, nil, widget.NewLabelWithStyle(
		"Monitor", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), clearBtn)
	monitorFloor := canvas.NewRectangle(color.Transparent)
	monitorFloor.SetMinSize(fyne.NewSize(0, 145))
	monitorContent := container.NewStack(monitorFloor, a.monitorLog)
	monitorSection := container.NewBorder(
		container.NewVBox(widget.NewSeparator(), monitorHeader), nil, nil, nil, monitorContent)

	resetRow := container.NewHBox(layout.NewSpacer(), a.resetBtn, layout.NewSpacer())
	mappingPanel := container.NewBorder(mappingHeader, resetRow, nil, nil, inputList)
	top := container.NewVBox(header, widget.NewSeparator(), connRow, widget.NewSeparator())
	content := container.NewBorder(top, monitorSection, nil, nil, mappingPanel)
	a.refreshPorts()
	a.updateControls()
	go a.portRefreshLoop()
	return container.NewPadded(content)
}

func mappingRow(index, input, mapping, action fyne.CanvasObject) *fyne.Container {
	indexCell := container.New(layout.NewGridWrapLayout(fyne.NewSize(42, 36)), index)
	actionCell := container.New(layout.NewGridWrapLayout(fyne.NewSize(76, 36)), action)
	values := container.NewGridWithColumns(2, input, mapping)
	return container.NewBorder(nil, nil, indexCell, actionCell, values)
}

func (a *App) Close() {
	a.closeOnce.Do(func() { close(a.stopRefresh) })
	_ = a.device.Disconnect()
}

func (a *App) portRefreshLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			fyne.Do(func() {
				if !a.device.IsConnected() && !a.busy {
					a.refreshPorts()
				}
			})
		case <-a.stopRefresh:
			return
		}
	}
}

func (a *App) refreshPorts() {
	ports, err := ListPorts()
	if err != nil {
		a.statusLbl.SetText("Port scan failed")
		return
	}
	selected := a.portSelect.Selected
	options := make([]string, 0, len(ports))
	for _, port := range ports {
		options = append(options, port.Name)
	}
	a.portSelect.Options = options
	a.portSelect.Refresh()
	if contains(options, selected) {
		a.portSelect.SetSelected(selected)
		return
	}
	preferred := fyne.CurrentApp().Preferences().String("lastPort")
	if contains(options, preferred) {
		a.portSelect.SetSelected(preferred)
	} else if len(options) == 1 {
		a.portSelect.SetSelected(options[0])
	} else {
		a.portSelect.ClearSelected()
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (a *App) updateControls() {
	connected := a.device.IsConnected()
	if a.busy {
		a.portSelect.Disable()
		a.refreshBtn.Disable()
		a.connectBtn.Disable()
	} else {
		a.connectBtn.Enable()
		if connected {
			a.portSelect.Disable()
			a.refreshBtn.Disable()
			a.connectBtn.SetText("Disconnect")
		} else {
			a.portSelect.Enable()
			a.refreshBtn.Enable()
			a.connectBtn.SetText("Connect")
		}
	}
	for _, button := range a.changeBtns {
		if connected && !a.busy {
			button.Enable()
		} else {
			button.Disable()
		}
	}
	if connected && !a.busy {
		a.resetBtn.Enable()
	} else {
		a.resetBtn.Disable()
	}
}

func (a *App) setBusy(busy bool, status string) {
	a.busy = busy
	if status != "" {
		a.statusLbl.SetText(status)
	}
	a.updateControls()
}

func (a *App) connect() {
	port := a.portSelect.Selected
	if port == "" {
		dialog.ShowError(fmt.Errorf("select a port first"), a.window)
		return
	}
	a.setBusy(true, "Connecting…")
	go func() {
		err := a.device.Connect(port)
		if err == nil {
			err = a.device.Identify()
		}
		if err == nil {
			err = a.device.GetLayout()
		}
		if err != nil {
			_ = a.device.Disconnect()
			fyne.Do(func() {
				a.setBusy(false, "Disconnected")
				dialog.ShowError(err, a.window)
			})
			return
		}
		events := a.device.EventChan()
		errors := a.device.ErrorChan()
		protocol := a.device.ProtocolVersion()
		fyne.Do(func() {
			fyne.CurrentApp().Preferences().SetString("lastPort", port)
			label := fmt.Sprintf("Connected: %s", port)
			if protocol == 0 {
				label += " (legacy)"
			}
			a.setBusy(false, label)
			a.refreshLayout()
		})
		go a.monitorSession(port, events, errors)
	}()
}

func (a *App) disconnect() {
	a.setBusy(true, "Disconnecting…")
	go func() {
		_ = a.device.Disconnect()
		fyne.Do(func() {
			a.clearLayout()
			a.setBusy(false, "Disconnected")
		})
	}()
}

func (a *App) clearLayout() {
	for i := 0; i < numInputs; i++ {
		a.keyLabels[i].SetText("—")
	}
}

func (a *App) refreshLayout() {
	current := a.device.GetLayoutData()
	for i := 0; i < numInputs; i++ {
		a.keyLabels[i].SetText(FormatKey(current[i][0], current[i][1]))
	}
}

func (a *App) resetDefaults() {
	a.setBusy(true, "Resetting…")
	go func() {
		err := a.device.ResetDefaults()
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, a.window)
			} else {
				a.refreshLayout()
			}
			a.setBusy(false, connectedStatus(a.device))
		})
	}()
}

func connectedStatus(device *Device) string {
	if !device.IsConnected() {
		return "Disconnected"
	}
	status := "Connected: " + device.PortName()
	if device.ProtocolVersion() == 0 {
		status += " (legacy)"
	}
	return status
}

func (a *App) monitorSession(port string, events <-chan KeyEvent, errors <-chan error) {
	for events != nil || errors != nil {
		select {
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			entry := fmt.Sprintf("[%s] %s → %s %s", time.Now().Format("15:04:05"),
				InputNames[event.Index], FormatKey(event.Modifier, event.Keycode), event.Action)
			fyne.Do(func() {
				a.monEntries = append(a.monEntries, entry)
				if len(a.monEntries) > 100 {
					a.monEntries = a.monEntries[len(a.monEntries)-100:]
				}
				a.monitorLog.Refresh()
				a.monitorLog.ScrollToBottom()
			})
		case err, ok := <-errors:
			if !ok {
				errors = nil
				continue
			}
			fyne.Do(func() {
				if !a.device.IsConnected() && a.device.PortName() == port {
					a.clearLayout()
					a.setBusy(false, "Disconnected unexpectedly")
					dialog.ShowError(err, a.window)
				}
			})
		}
	}
}

func (a *App) showKeyEditor(index int) {
	if !a.device.IsConnected() {
		return
	}
	current := a.device.GetLayoutData()[index]
	categorySelect := widget.NewSelect(KeyCategories, nil)
	keySelect := widget.NewSelect(nil, nil)
	categorySelect.OnChanged = func(category string) {
		keySelect.Options = KeyChoices(category)
		keySelect.Refresh()
		if !contains(keySelect.Options, keySelect.Selected) && len(keySelect.Options) > 0 {
			keySelect.SetSelected(keySelect.Options[0])
		}
	}
	categorySelect.SetSelected(KeyCategory(current[1]))
	keySelect.SetSelected(HIDKeyName(current[1]))

	modifierChecks := make([]*widget.Check, len(ModifierNames))
	for i, modifier := range ModifierNames {
		bit := modifier.Bit
		modifierChecks[i] = widget.NewCheck(modifier.Name, nil)
		modifierChecks[i].SetChecked(current[0]&bit != 0)
	}

	status := widget.NewLabel("Use the picker, or click the capture area and press a shortcut.")
	status.Wrapping = fyne.TextWrapWord
	setEditorValue := func(modifier, keycode uint8) {
		categorySelect.SetSelected(KeyCategory(keycode))
		keySelect.SetSelected(HIDKeyName(keycode))
		for i, item := range ModifierNames {
			modifierChecks[i].SetChecked(modifier&item.Bit != 0)
		}
		status.SetText("Captured: " + FormatKey(modifier, keycode))
	}
	capture := newKeyCaptureCanvas(setEditorValue, func() {
		status.SetText("Capture cancelled; picker selection is unchanged.")
	})

	leftModifiers := container.NewHBox(modifierChecks[0], modifierChecks[1], modifierChecks[2], modifierChecks[3])
	rightModifiers := container.NewHBox(modifierChecks[4], modifierChecks[5], modifierChecks[6], modifierChecks[7])
	content := container.NewVBox(
		widget.NewLabelWithStyle("Remap: "+InputNames[index], fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		container.NewGridWithColumns(2, categorySelect, keySelect),
		leftModifiers, rightModifiers,
		widget.NewSeparator(), capture, status,
	)

	d := dialog.NewCustomConfirm("Edit Mapping", "Apply", "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		keycode, found := KeyCodeByName(keySelect.Selected)
		if !found {
			dialog.ShowError(fmt.Errorf("select a key"), a.window)
			return
		}
		var modifier uint8
		for i, item := range ModifierNames {
			if modifierChecks[i].Checked {
				modifier |= item.Bit
			}
		}
		duplicate := -1
		layoutData := a.device.GetLayoutData()
		for i, mapping := range layoutData {
			if i != index && mapping[0] == modifier && mapping[1] == keycode {
				duplicate = i
				break
			}
		}
		apply := func() { a.applyMapping(index, modifier, keycode) }
		if duplicate >= 0 {
			message := fmt.Sprintf("%s already uses %s. Apply the duplicate mapping?",
				InputNames[duplicate], FormatKey(modifier, keycode))
			dialog.ShowConfirm("Duplicate Mapping", message, func(confirmed bool) {
				if confirmed {
					apply()
				}
			}, a.window)
			return
		}
		apply()
	}, a.window)
	d.Resize(fyne.NewSize(520, 360))
	d.Show()
}

func (a *App) applyMapping(index int, modifier, keycode uint8) {
	a.setBusy(true, "Applying mapping…")
	go func() {
		err := a.device.SetKey(index, modifier, keycode)
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, a.window)
			} else {
				a.refreshLayout()
			}
			a.setBusy(false, connectedStatus(a.device))
		})
	}()
}

type keyCaptureCanvas struct {
	widget.BaseWidget
	onCapture func(modifier, keycode uint8)
	onCancel  func()
	focused   bool
}

func newKeyCaptureCanvas(onCapture func(modifier, keycode uint8), onCancel func()) *keyCaptureCanvas {
	canvas := &keyCaptureCanvas{onCapture: onCapture, onCancel: onCancel}
	canvas.ExtendBaseWidget(canvas)
	return canvas
}

func (k *keyCaptureCanvas) CreateRenderer() fyne.WidgetRenderer {
	label := widget.NewLabel("Click here, then press a key or shortcut…")
	label.Alignment = fyne.TextAlignCenter
	background := canvas.NewRectangle(theme.InputBackgroundColor())
	return &keyCaptureRenderer{canvas: k, label: label, bg: background,
		objects: []fyne.CanvasObject{background, label}}
}

func (k *keyCaptureCanvas) Tapped(_ *fyne.PointEvent) {
	if target := fyne.CurrentApp().Driver().CanvasForObject(k); target != nil {
		target.Focus(k)
	}
}

func (k *keyCaptureCanvas) FocusGained() { k.focused = true; k.Refresh() }
func (k *keyCaptureCanvas) FocusLost()   { k.focused = false; k.Refresh() }

func (k *keyCaptureCanvas) TypedRune(r rune) {
	keycode, shifted := ShiftedRuneToHID[r]
	if shifted {
		k.onCapture(MOD_LSHIFT, keycode)
		return
	}
	keycode, ok := RuneToHID[unicode.ToLower(r)]
	if !ok {
		return
	}
	var modifier uint8
	if unicode.IsUpper(r) {
		modifier = MOD_LSHIFT
	}
	k.onCapture(modifier, keycode)
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
	var modifier uint8
	if driver, ok := fyne.CurrentApp().Driver().(desktop.Driver); ok {
		mods := driver.CurrentKeyModifiers()
		if mods&fyne.KeyModifierControl != 0 {
			modifier |= MOD_LCTRL
		}
		if mods&fyne.KeyModifierShift != 0 {
			modifier |= MOD_LSHIFT
		}
		if mods&fyne.KeyModifierAlt != 0 {
			modifier |= MOD_LALT
		}
		if mods&fyne.KeyModifierSuper != 0 {
			modifier |= MOD_LGUI
		}
	}
	k.onCapture(modifier, keycode)
}

var builtinShortcuts = map[string]uint8{
	"SelectAll": HID_KEY_A, "Copy": HID_KEY_C, "Paste": HID_KEY_V,
	"Cut": HID_KEY_X, "Undo": HID_KEY_Z, "Redo": HID_KEY_Y,
	"Find": HID_KEY_F, "New": HID_KEY_N, "Open": HID_KEY_O,
	"Save": HID_KEY_S, "Print": HID_KEY_P, "CloseWindow": HID_KEY_W, "Quit": HID_KEY_Q,
}

func (k *keyCaptureCanvas) TypedShortcut(shortcut fyne.Shortcut) {
	if keycode, ok := builtinShortcuts[shortcut.ShortcutName()]; ok {
		modifier := uint8(MOD_LCTRL)
		if fyne.KeyModifierShortcutDefault == fyne.KeyModifierSuper {
			modifier = MOD_LGUI
		}
		k.onCapture(modifier, keycode)
		return
	}
	custom, ok := shortcut.(*desktop.CustomShortcut)
	if !ok {
		return
	}
	keycode, ok := FyneToHID[custom.KeyName]
	if !ok {
		return
	}
	var modifier uint8
	if custom.Modifier&fyne.KeyModifierControl != 0 {
		modifier |= MOD_LCTRL
	}
	if custom.Modifier&fyne.KeyModifierShift != 0 {
		modifier |= MOD_LSHIFT
	}
	if custom.Modifier&fyne.KeyModifierAlt != 0 {
		modifier |= MOD_LALT
	}
	if custom.Modifier&fyne.KeyModifierSuper != 0 {
		modifier |= MOD_LGUI
	}
	k.onCapture(modifier, keycode)
}

type keyCaptureRenderer struct {
	canvas  *keyCaptureCanvas
	label   *widget.Label
	bg      *canvas.Rectangle
	objects []fyne.CanvasObject
}

func (r *keyCaptureRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.label.Resize(size)
}
func (r *keyCaptureRenderer) MinSize() fyne.Size { return fyne.NewSize(300, 50) }
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
func (r *keyCaptureRenderer) Destroy()                     {}
