package main

import (
	"fmt"
	"image/color"
	"sort"
	"strconv"
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

type App struct {
	window      fyne.Window
	device      *Device
	statusLbl   *widget.Label
	portSelect  *widget.Select
	connectBtn  *widget.Button
	refreshBtn  *widget.Button
	resetBtn    *widget.Button
	manageBtn   *widget.Button
	mappingHost *fyne.Container
	changeBtns  []*widget.Button
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
	connection := container.NewBorder(nil, nil, widget.NewLabel("Port:"),
		container.NewHBox(a.refreshBtn, a.connectBtn), a.portSelect)

	a.mappingHost = container.NewStack()
	a.rebuildMappings()
	a.resetBtn = widget.NewButton("Reset All to Defaults", func() {
		dialog.ShowConfirm("Reset Defaults", "Reset Base mappings and remove every layer?", func(ok bool) {
			if ok {
				a.resetDefaults()
			}
		}, a.window)
	})
	a.manageBtn = widget.NewButton("Manage Layers", a.showLayerManager)
	a.manageBtn.Importance = widget.HighImportance
	actions := container.NewHBox(layout.NewSpacer(), a.manageBtn, a.resetBtn, layout.NewSpacer())
	mappingPanel := container.NewBorder(nil, actions, nil, nil, a.mappingHost)

	a.monitorLog = widget.NewList(
		func() int { return len(a.monEntries) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, object fyne.CanvasObject) { object.(*widget.Label).SetText(a.monEntries[id]) },
	)
	clearBtn := widget.NewButton("Clear", func() {
		a.monEntries = nil
		a.monitorLog.Refresh()
	})
	clearBtn.Importance = widget.LowImportance
	monitorHeader := container.NewBorder(nil, nil,
		widget.NewLabelWithStyle("Monitor", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), clearBtn)
	monitorFloor := canvas.NewRectangle(color.Transparent)
	monitorFloor.SetMinSize(fyne.NewSize(0, 138))
	monitorSection := container.NewBorder(container.NewVBox(widget.NewSeparator(), monitorHeader), nil, nil, nil,
		container.NewStack(monitorFloor, a.monitorLog))

	top := container.NewVBox(header, widget.NewSeparator(), connection, widget.NewSeparator())
	content := container.NewBorder(top, monitorSection, nil, nil, mappingPanel)
	a.refreshPorts()
	a.updateControls()
	go a.portRefreshLoop()
	return container.NewPadded(content)
}

func (a *App) rebuildMappings() {
	_, inputs, layerConfig, layouts := a.device.Snapshot()
	if len(inputs) == 0 {
		inputs = append([]InputDescriptor(nil), legacyInputs...)
		layouts = map[int][]Mapping{0: make([]Mapping, len(inputs))}
	}
	a.changeBtns = nil
	tabs := []*container.TabItem{container.NewTabItem("Base", a.mappingTable(0, -1, inputs, layouts[0]))}
	sort.Slice(layerConfig.Layers, func(i, j int) bool { return layerConfig.Layers[i].Slot < layerConfig.Layers[j].Slot })
	for _, layer := range layerConfig.Layers {
		name := fmt.Sprintf("Layer %d", layer.Slot)
		if layer.Trigger >= 0 && layer.Trigger < len(inputs) {
			name = inputs[layer.Trigger].Name + " Layer"
		}
		tabs = append(tabs, container.NewTabItem(name,
			a.mappingTable(layer.Slot, layer.Trigger, inputs, layouts[layer.Slot])))
	}
	appTabs := container.NewAppTabs(tabs...)
	appTabs.SetTabLocation(container.TabLocationTop)
	a.mappingHost.Objects = []fyne.CanvasObject{appTabs}
	a.mappingHost.Refresh()
	if a.connectBtn != nil && a.resetBtn != nil && a.manageBtn != nil {
		a.updateControls()
	}
}

func (a *App) mappingTable(layer, trigger int, inputs []InputDescriptor, mappings []Mapping) fyne.CanvasObject {
	rows := container.NewVBox()
	for i, input := range inputs {
		mappingLabel := widget.NewLabel("—")
		mappingLabel.TextStyle = fyne.TextStyle{Monospace: true}
		if i < len(mappings) {
			mappingLabel.SetText(FormatKey(mappings[i].Modifier, mappings[i].Keycode))
		}
		inputLabel := widget.NewLabel(input.Name)
		inputLabel.Truncation = fyne.TextTruncateEllipsis
		indexLabel := widget.NewLabel(strconv.Itoa(i))
		indexLabel.Alignment = fyne.TextAlignCenter
		index := i
		button := widget.NewButton("Edit", func() { a.showKeyEditor(layer, index) })
		if layer > 0 && index == trigger {
			mappingLabel.SetText("Layer trigger")
			button.SetText("—")
			button.Disable()
		}
		a.changeBtns = append(a.changeBtns, button)
		rows.Add(mappingRow(indexLabel, inputLabel, mappingLabel, button))
	}
	header := mappingRow(
		widget.NewLabelWithStyle("#", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Input", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Mapping", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Action", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
	)
	scroll := container.NewVScroll(rows)
	scroll.SetMinSize(fyne.NewSize(0, 230))
	return container.NewBorder(header, nil, nil, nil, scroll)
}

func mappingRow(index, input, mapping, action fyne.CanvasObject) *fyne.Container {
	indexCell := container.New(layout.NewGridWrapLayout(fyne.NewSize(42, 36)), index)
	actionCell := container.New(layout.NewGridWrapLayout(fyne.NewSize(76, 36)), action)
	return container.NewBorder(nil, nil, indexCell, actionCell, container.NewGridWithColumns(2, input, mapping))
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
		if connected && !a.busy && button.Text != "—" {
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
	if connected && !a.busy && a.device.ProtocolVersion() >= 2 {
		a.manageBtn.Enable()
	} else {
		a.manageBtn.Disable()
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
			err = a.device.LoadConfiguration()
		}
		if err != nil {
			_ = a.device.Disconnect()
			fyne.Do(func() {
				a.setBusy(false, "Disconnected")
				dialog.ShowError(err, a.window)
			})
			return
		}
		events, errors := a.device.EventChan(), a.device.ErrorChan()
		protocol := a.device.ProtocolVersion()
		fyne.Do(func() {
			fyne.CurrentApp().Preferences().SetString("lastPort", port)
			label := fmt.Sprintf("Connected: %s", port)
			if protocol < 2 {
				label += " (Base only)"
			}
			a.rebuildMappings()
			a.setBusy(false, label)
		})
		go a.monitorSession(port, events, errors)
	}()
}

func (a *App) disconnect() {
	a.setBusy(true, "Disconnecting…")
	go func() {
		_ = a.device.Disconnect()
		fyne.Do(func() {
			a.rebuildMappings()
			a.setBusy(false, "Disconnected")
		})
	}()
}

func (a *App) resetDefaults() {
	a.setBusy(true, "Resetting…")
	go func() {
		err := a.device.ResetDefaults()
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, a.window)
			} else {
				a.rebuildMappings()
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
	if device.ProtocolVersion() < 2 {
		status += " (Base only)"
	}
	return status
}

func (a *App) inputName(index int) string {
	_, inputs, _, _ := a.device.Snapshot()
	if index >= 0 && index < len(inputs) {
		return inputs[index].Name
	}
	return fmt.Sprintf("Input %d", index)
}

func (a *App) layerName(layer int) string {
	_, inputs, config, _ := a.device.Snapshot()
	for _, definition := range config.Layers {
		if definition.Slot == layer && definition.Trigger >= 0 && definition.Trigger < len(inputs) {
			return inputs[definition.Trigger].Name + " Layer"
		}
	}
	return fmt.Sprintf("Layer %d", layer)
}

func (a *App) monitorSession(port string, events <-chan DeviceEvent, errors <-chan error) {
	for events != nil || errors != nil {
		select {
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			var message string
			if event.Kind == "layer" {
				message = fmt.Sprintf("%s → %s", a.layerName(event.Layer), event.Action)
			} else {
				message = fmt.Sprintf("%s → %s %s", a.inputName(event.Index),
					FormatKey(event.Modifier, event.Keycode), event.Action)
			}
			entry := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), message)
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
					a.rebuildMappings()
					a.setBusy(false, "Disconnected unexpectedly")
					dialog.ShowError(err, a.window)
				}
			})
		}
	}
}

func (a *App) showLayerManager() {
	if !a.device.IsConnected() || a.device.ProtocolVersion() < 2 {
		return
	}
	caps, inputs, config, _ := a.device.Snapshot()
	threshold := widget.NewEntry()
	threshold.SetText(strconv.Itoa(config.HoldMS))
	threshold.Validator = func(value string) error {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < minimumHoldMS || parsed > maximumHoldMS {
			return fmt.Errorf("enter %d–%d ms", minimumHoldMS, maximumHoldMS)
		}
		return nil
	}
	usedTriggers, usedSlots := map[int]bool{}, map[int]bool{}
	rows := container.NewVBox()
	var manager dialog.Dialog
	for _, layer := range config.Layers {
		definition := layer
		usedTriggers[layer.Trigger], usedSlots[layer.Slot] = true, true
		name := fmt.Sprintf("Layer %d", layer.Slot)
		if layer.Trigger < len(inputs) {
			name = inputs[layer.Trigger].Name + " Layer"
		}
		remove := widget.NewButton("Remove", func() {
			dialog.ShowConfirm("Remove Layer", "Remove this layer and clear all of its mappings?", func(ok bool) {
				if !ok {
					return
				}
				manager.Hide()
				a.runConfigChange("Removing layer…", func() error { return a.device.RemoveLayer(definition.Slot) })
			}, a.window)
		})
		remove.Importance = widget.DangerImportance
		rows.Add(container.NewBorder(nil, nil, widget.NewLabel(name), remove))
	}

	eligibleNames := []string{}
	triggerByName := map[string]int{}
	for _, input := range inputs {
		if input.LayerEligible && !usedTriggers[input.Index] {
			eligibleNames = append(eligibleNames, input.Name)
			triggerByName[input.Name] = input.Index
		}
	}
	triggerSelect := widget.NewSelect(eligibleNames, nil)
	triggerSelect.PlaceHolder = "Select a layer switch"
	if len(eligibleNames) > 0 {
		triggerSelect.SetSelected(eligibleNames[0])
	}
	add := widget.NewButton("Add Layer", func() {
		trigger, ok := triggerByName[triggerSelect.Selected]
		if !ok {
			dialog.ShowError(fmt.Errorf("select an unused layer switch"), a.window)
			return
		}
		slot := 0
		for candidate := 1; candidate <= caps.MaxLayers; candidate++ {
			if !usedSlots[candidate] {
				slot = candidate
				break
			}
		}
		if slot == 0 {
			dialog.ShowError(fmt.Errorf("maximum layer count reached"), a.window)
			return
		}
		manager.Hide()
		a.runConfigChange("Adding layer…", func() error { return a.device.SetLayer(slot, trigger) })
	})
	if len(eligibleNames) == 0 || len(config.Layers) >= caps.MaxLayers {
		triggerSelect.Disable()
		add.Disable()
	}
	applyThreshold := widget.NewButton("Apply", func() {
		value, err := strconv.Atoi(threshold.Text)
		if err != nil || value < minimumHoldMS || value > maximumHoldMS {
			dialog.ShowError(fmt.Errorf("hold threshold must be %d–%d ms", minimumHoldMS, maximumHoldMS), a.window)
			return
		}
		manager.Hide()
		a.runConfigChange("Updating hold threshold…", func() error { return a.device.SetHoldMS(value) })
	})
	content := container.NewVBox(
		widget.NewLabel("Hold a configured switch to activate its layer. Using another control activates it immediately."),
		container.NewBorder(nil, nil, widget.NewLabel("Hold threshold (ms)"), applyThreshold, threshold),
		widget.NewSeparator(), rows, widget.NewSeparator(),
		container.NewBorder(nil, nil, nil, add, triggerSelect),
	)
	manager = dialog.NewCustom("Manage Layers", "Close", content, a.window)
	manager.Resize(fyne.NewSize(560, 360))
	manager.Show()
}

func (a *App) runConfigChange(status string, operation func() error) {
	a.setBusy(true, status)
	go func() {
		err := operation()
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, a.window)
			} else {
				a.rebuildMappings()
			}
			a.setBusy(false, connectedStatus(a.device))
		})
	}()
}

func (a *App) showKeyEditor(layer, index int) {
	if !a.device.IsConnected() {
		return
	}
	_, inputs, _, layouts := a.device.Snapshot()
	currentLayout := layouts[layer]
	if index < 0 || index >= len(inputs) || index >= len(currentLayout) {
		return
	}
	current := currentLayout[index]
	categorySelect := widget.NewSelect(KeyCategories, nil)
	keySelect := widget.NewSelect(nil, nil)
	categorySelect.OnChanged = func(category string) {
		keySelect.Options = KeyChoices(category)
		keySelect.Refresh()
		if !contains(keySelect.Options, keySelect.Selected) && len(keySelect.Options) > 0 {
			keySelect.SetSelected(keySelect.Options[0])
		}
	}
	categorySelect.SetSelected(KeyCategory(current.Keycode))
	keySelect.SetSelected(HIDKeyName(current.Keycode))

	modifierChecks := make([]*widget.Check, len(ModifierNames))
	for i, modifier := range ModifierNames {
		modifierChecks[i] = widget.NewCheck(modifier.Name, nil)
		modifierChecks[i].SetChecked(current.Modifier&modifier.Bit != 0)
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
	content := container.NewVBox(
		widget.NewLabelWithStyle("Remap: "+inputs[index].Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(), container.NewGridWithColumns(2, categorySelect, keySelect),
		container.NewHBox(modifierChecks[0], modifierChecks[1], modifierChecks[2], modifierChecks[3]),
		container.NewHBox(modifierChecks[4], modifierChecks[5], modifierChecks[6], modifierChecks[7]),
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
		if keycode != 0 {
			for i, item := range ModifierNames {
				if modifierChecks[i].Checked {
					modifier |= item.Bit
				}
			}
		}
		duplicate := -1
		for i, mapping := range currentLayout {
			if i != index && mapping.Modifier == modifier && mapping.Keycode == keycode {
				duplicate = i
				break
			}
		}
		apply := func() { a.applyMapping(layer, index, modifier, keycode) }
		if duplicate >= 0 && keycode != 0 {
			message := fmt.Sprintf("%s already uses %s in this layer. Apply the duplicate mapping?",
				inputs[duplicate].Name, FormatKey(modifier, keycode))
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

func (a *App) applyMapping(layer, index int, modifier, keycode uint8) {
	a.setBusy(true, "Applying mapping…")
	go func() {
		err := a.device.SetLayerKey(layer, index, modifier, keycode)
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, a.window)
			} else {
				a.rebuildMappings()
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

func (r *keyCaptureRenderer) Layout(size fyne.Size) { r.bg.Resize(size); r.label.Resize(size) }
func (r *keyCaptureRenderer) MinSize() fyne.Size    { return fyne.NewSize(300, 50) }
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
