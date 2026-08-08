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
	window          fyne.Window
	device          *Device
	statusLbl       *widget.Label
	portSelect      *widget.Select
	connectBtn      *widget.Button
	refreshBtn      *widget.Button
	resetBtn        *widget.Button
	manageBtn       *widget.Button
	mappingHost     *fyne.Container
	mappingTabs     *container.AppTabs
	tabByLayer      map[int]int
	layerByTab      []int
	mappingLists    map[int]*widget.List
	highlightedRows map[int]int
	selectedLayer   int
	monitorLog      *widget.List
	monEntries      []string
	busy            bool
	stopRefresh     chan struct{}
	closeOnce       sync.Once
}

var defaultCompactWindowSize = fyne.NewSize(760, 640)

func NewApp(window fyne.Window, device *Device) *App {
	return &App{
		window: window, device: device, stopRefresh: make(chan struct{}),
		tabByLayer: make(map[int]int), mappingLists: make(map[int]*widget.List),
		highlightedRows: make(map[int]int),
	}
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

func (a *App) rebuildMappings(targetLayer ...int) {
	if len(targetLayer) > 0 {
		a.selectedLayer = targetLayer[0]
	}
	_, inputs, layerConfig, layouts := a.device.Snapshot()
	if len(inputs) == 0 {
		inputs = append([]InputDescriptor(nil), legacyInputs...)
		layouts = map[int][]Mapping{0: make([]Mapping, len(inputs))}
	}
	a.tabByLayer = make(map[int]int)
	a.layerByTab = nil
	a.mappingLists = make(map[int]*widget.List)
	tabs := []*container.TabItem{container.NewTabItem("Base", a.mappingTable(0, -1, inputs, layouts[0]))}
	a.tabByLayer[0] = 0
	a.layerByTab = append(a.layerByTab, 0)
	sort.Slice(layerConfig.Layers, func(i, j int) bool { return layerConfig.Layers[i].Slot < layerConfig.Layers[j].Slot })
	for _, layer := range layerConfig.Layers {
		name := fmt.Sprintf("Layer %d", layer.Slot)
		if layer.Trigger >= 0 && layer.Trigger < len(inputs) {
			name = inputs[layer.Trigger].Name + " Layer"
		}
		tabs = append(tabs, container.NewTabItem(name,
			a.mappingTable(layer.Slot, layer.Trigger, inputs, layouts[layer.Slot])))
		a.tabByLayer[layer.Slot] = len(tabs) - 1
		a.layerByTab = append(a.layerByTab, layer.Slot)
	}
	appTabs := container.NewAppTabs(tabs...)
	appTabs.SetTabLocation(container.TabLocationTop)
	appTabs.OnSelected = func(_ *container.TabItem) {
		index := appTabs.SelectedIndex()
		if index >= 0 && index < len(a.layerByTab) {
			a.selectedLayer = a.layerByTab[index]
		}
	}
	a.mappingTabs = appTabs
	a.mappingHost.Objects = []fyne.CanvasObject{appTabs}
	a.mappingHost.Refresh()
	if tab, ok := a.tabByLayer[a.selectedLayer]; ok {
		appTabs.SelectIndex(tab)
	} else {
		a.selectedLayer = 0
		appTabs.SelectIndex(0)
	}
	for layer, input := range a.highlightedRows {
		if list := a.mappingLists[layer]; list != nil && input >= 0 && input < len(inputs) {
			list.Select(input)
		}
	}
	if a.connectBtn != nil && a.resetBtn != nil && a.manageBtn != nil {
		a.updateControls()
	}
}

func (a *App) mappingTable(layer, trigger int, inputs []InputDescriptor, mappings []Mapping) fyne.CanvasObject {
	list := widget.NewList(
		func() int { return len(inputs) },
		func() fyne.CanvasObject {
			indexLabel := widget.NewLabel("")
			indexLabel.Alignment = fyne.TextAlignCenter
			inputLabel := widget.NewLabel("")
			inputLabel.Truncation = fyne.TextTruncateEllipsis
			mappingLabel := widget.NewLabel("")
			mappingLabel.TextStyle = fyne.TextStyle{Monospace: true}
			button := widget.NewButton("Edit", nil)
			view := &mappingRowView{index: indexLabel, input: inputLabel,
				mapping: mappingLabel, button: button}
			view.Container = mappingRow(indexLabel, inputLabel, mappingLabel, button)
			return view
		},
		func(id widget.ListItemID, object fyne.CanvasObject) {
			view := object.(*mappingRowView)
			view.index.SetText(strconv.Itoa(id))
			view.input.SetText(inputs[id].Name)
			if id < len(mappings) {
				view.mapping.SetText(FormatMapping(mappings[id]))
			} else {
				view.mapping.SetText("—")
			}
			index := id
			view.button.SetText("Edit")
			view.button.OnTapped = func() {
				a.focusMapping(layer, index)
				a.showKeyEditor(layer, index)
			}
			if layer > 0 && id == trigger {
				view.mapping.SetText("Layer trigger")
				view.button.SetText("—")
				view.button.Disable()
			} else if a.device.IsConnected() && !a.busy {
				view.button.Enable()
			} else {
				view.button.Disable()
			}
		},
	)
	a.mappingLists[layer] = list
	header := mappingRow(
		widget.NewLabelWithStyle("#", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Input", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Mapping", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Action", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
	)
	listFloor := canvas.NewRectangle(color.Transparent)
	listFloor.SetMinSize(fyne.NewSize(0, 230))
	return container.NewBorder(header, nil, nil, nil, container.NewStack(listFloor, list))
}

type mappingRowView struct {
	*fyne.Container
	index   *widget.Label
	input   *widget.Label
	mapping *widget.Label
	button  *widget.Button
}

func (a *App) focusMapping(layer, input int) {
	tab, ok := a.tabByLayer[layer]
	if !ok || a.mappingTabs == nil {
		return
	}
	a.selectedLayer = layer
	a.mappingTabs.SelectIndex(tab)
	if list := a.mappingLists[layer]; list != nil {
		a.highlightedRows[layer] = input
		list.Select(input)
		list.ScrollTo(input)
	}
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
	for _, list := range a.mappingLists {
		list.Refresh()
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

func (a *App) layerTrigger(layer int) int {
	_, _, config, _ := a.device.Snapshot()
	for _, definition := range config.Layers {
		if definition.Slot == layer {
			return definition.Trigger
		}
	}
	return -1
}

func (a *App) eventMapping(event DeviceEvent) Mapping {
	_, _, _, layouts := a.device.Snapshot()
	if layout := layouts[event.Layer]; event.Index >= 0 && event.Index < len(layout) {
		return layout[event.Index]
	}
	return Mapping{Modifier: event.Modifier, Keys: event.Keys}
}

func (a *App) navigateForDeviceEvent(event DeviceEvent) {
	if event.Kind == "layer" && event.Action == "on" {
		a.focusMapping(event.Layer, a.layerTrigger(event.Layer))
	} else if event.Kind == "key" {
		a.focusMapping(event.Layer, event.Index)
	}
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
				mapping := a.eventMapping(event)
				message = fmt.Sprintf("%s → %s %s", a.inputName(event.Index),
					FormatMapping(mapping), event.Action)
			}
			entry := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), message)
			eventCopy := event
			entryCopy := entry
			fyne.Do(func() {
				a.navigateForDeviceEvent(eventCopy)
				a.monEntries = append(a.monEntries, entryCopy)
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
				a.runConfigChange("Removing layer…", 0, func() error { return a.device.RemoveLayer(definition.Slot) })
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
		a.runConfigChange("Adding layer…", slot, func() error { return a.device.SetLayer(slot, trigger) })
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
		a.runConfigChange("Updating hold threshold…", a.selectedLayer, func() error { return a.device.SetHoldMS(value) })
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

func (a *App) runConfigChange(status string, targetLayer int, operation func() error) {
	a.setBusy(true, status)
	go func() {
		err := operation()
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, a.window)
			} else {
				a.rebuildMappings(targetLayer)
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
	selectedKeys := make([]uint8, current.KeyCount())
	copy(selectedKeys, current.Keys[:current.KeyCount()])

	modifierChecks := make([]*widget.Check, len(ModifierNames))
	for i, modifier := range ModifierNames {
		modifierChecks[i] = widget.NewCheck(modifier.Name, nil)
		modifierChecks[i].SetChecked(current.Modifier&modifier.Bit != 0)
	}
	status := widget.NewLabel("Choose up to three keys, or capture one simultaneous chord.")
	status.Wrapping = fyne.TextWrapWord
	keyRows := container.NewVBox()
	addKey := widget.NewButton("Add Key", nil)
	var rebuildKeyRows func()
	rebuildKeyRows = func() {
		keyRows.RemoveAll()
		if len(selectedKeys) == 0 {
			keyRows.Add(widget.NewLabel("No Action"))
		}
		for slot, code := range selectedKeys {
			slotIndex := slot
			categorySelect := widget.NewSelect(KeyCategories[1:], nil)
			keySelect := widget.NewSelect(nil, nil)
			syncing := true
			categorySelect.OnChanged = func(category string) {
				keySelect.Options = KeyChoices(category)
				keySelect.Refresh()
				if !contains(keySelect.Options, keySelect.Selected) && len(keySelect.Options) > 0 {
					keySelect.SetSelected(keySelect.Options[0])
				}
			}
			keySelect.OnChanged = func(name string) {
				if syncing {
					return
				}
				if key, found := KeyCodeByName(name); found && slotIndex < len(selectedKeys) {
					selectedKeys[slotIndex] = key
				}
			}
			categorySelect.SetSelected(KeyCategory(code))
			keySelect.SetSelected(HIDKeyName(code))
			syncing = false
			remove := widget.NewButton("Remove", func() {
				selectedKeys = append(selectedKeys[:slotIndex], selectedKeys[slotIndex+1:]...)
				rebuildKeyRows()
			})
			remove.Importance = widget.LowImportance
			keyRows.Add(container.NewBorder(nil, nil,
				widget.NewLabel(fmt.Sprintf("Key %d", slot+1)), remove,
				container.NewGridWithColumns(2, categorySelect, keySelect)))
		}
		if len(selectedKeys) >= maxMappingKeys {
			addKey.Disable()
		} else {
			addKey.Enable()
		}
	}
	addKey.OnTapped = func() {
		if len(selectedKeys) >= maxMappingKeys {
			return
		}
		candidate := uint8(HID_KEY_A)
		for containsKeycode(selectedKeys, candidate) {
			candidate++
		}
		selectedKeys = append(selectedKeys, candidate)
		rebuildKeyRows()
	}
	clearKeys := widget.NewButton("No Action", func() {
		selectedKeys = nil
		for _, check := range modifierChecks {
			check.SetChecked(false)
		}
		rebuildKeyRows()
	})
	clearKeys.Importance = widget.LowImportance

	setEditorValue := func(mapping Mapping) {
		selectedKeys = append(selectedKeys[:0], mapping.Keys[:mapping.KeyCount()]...)
		for i, item := range ModifierNames {
			modifierChecks[i].SetChecked(mapping.Modifier&item.Bit != 0)
		}
		rebuildKeyRows()
		status.SetText("Captured: " + FormatMapping(mapping))
	}
	capture := newKeyCaptureCanvas(setEditorValue, func() {
		status.SetText("Capture cancelled; picker selection is unchanged.")
	}, func(message string) {
		status.SetText(message)
	})
	rebuildKeyRows()
	content := container.NewVBox(
		widget.NewLabelWithStyle("Remap: "+inputs[index].Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(), keyRows,
		container.NewHBox(addKey, clearKeys),
		container.NewHBox(modifierChecks[0], modifierChecks[1], modifierChecks[2], modifierChecks[3]),
		container.NewHBox(modifierChecks[4], modifierChecks[5], modifierChecks[6], modifierChecks[7]),
		widget.NewSeparator(), capture, status,
	)

	d := dialog.NewCustomConfirm("Edit Mapping", "Apply", "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		var modifier uint8
		if len(selectedKeys) != 0 {
			for i, item := range ModifierNames {
				if modifierChecks[i].Checked {
					modifier |= item.Bit
				}
			}
		}
		mapping := Mapping{Modifier: modifier}
		for i, key := range selectedKeys {
			if containsKeycode(selectedKeys[:i], key) {
				dialog.ShowError(fmt.Errorf("a chord cannot contain the same key twice"), a.window)
				return
			}
			mapping.Keys[i] = key
		}
		duplicate := -1
		for i, candidate := range currentLayout {
			if i != index && sameChord(candidate, mapping) {
				duplicate = i
				break
			}
		}
		apply := func() { a.applyMapping(layer, index, mapping) }
		if duplicate >= 0 && mapping.KeyCount() != 0 {
			message := fmt.Sprintf("%s already uses %s in this layer. Apply the duplicate mapping?",
				inputs[duplicate].Name, FormatMapping(mapping))
			dialog.ShowConfirm("Duplicate Mapping", message, func(confirmed bool) {
				if confirmed {
					apply()
				}
			}, a.window)
			return
		}
		apply()
	}, a.window)
	d.Resize(fyne.NewSize(560, 500))
	d.Show()
}

func containsKeycode(keys []uint8, wanted uint8) bool {
	for _, key := range keys {
		if key == wanted {
			return true
		}
	}
	return false
}

func sameChord(left, right Mapping) bool {
	if left.Modifier != right.Modifier || left.KeyCount() != right.KeyCount() {
		return false
	}
	for i := 0; i < left.KeyCount(); i++ {
		if !containsKeycode(right.Keys[:right.KeyCount()], left.Keys[i]) {
			return false
		}
	}
	return true
}

func (a *App) applyMapping(layer, index int, mapping Mapping) {
	a.setBusy(true, "Applying mapping…")
	go func() {
		err := a.device.SetLayerMapping(layer, index, mapping)
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, a.window)
			} else {
				a.highlightedRows[layer] = index
				a.rebuildMappings(layer)
			}
			a.setBusy(false, connectedStatus(a.device))
		})
	}()
}

type keyCaptureCanvas struct {
	widget.BaseWidget
	onCapture func(Mapping)
	onCancel  func()
	onStatus  func(string)
	focused   bool
	pressed   map[fyne.KeyName]bool
	gesture   Mapping
	overflow  bool
}

func newKeyCaptureCanvas(onCapture func(Mapping), onCancel func(), onStatus func(string)) *keyCaptureCanvas {
	canvas := &keyCaptureCanvas{onCapture: onCapture, onCancel: onCancel,
		onStatus: onStatus, pressed: make(map[fyne.KeyName]bool)}
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
func (k *keyCaptureCanvas) FocusLost() {
	k.focused = false
	k.resetGesture()
	k.Refresh()
}

func modifierForKey(name fyne.KeyName) (uint8, bool) {
	switch name {
	case desktop.KeyControlLeft:
		return MOD_LCTRL, true
	case desktop.KeyControlRight:
		return MOD_RCTRL, true
	case desktop.KeyShiftLeft:
		return MOD_LSHIFT, true
	case desktop.KeyShiftRight:
		return MOD_RSHIFT, true
	case desktop.KeyAltLeft:
		return MOD_LALT, true
	case desktop.KeyAltRight:
		return MOD_RALT, true
	case desktop.KeySuperLeft:
		return MOD_LGUI, true
	case desktop.KeySuperRight:
		return MOD_RGUI, true
	default:
		return 0, false
	}
}

func (k *keyCaptureCanvas) resetGesture() {
	k.pressed = make(map[fyne.KeyName]bool)
	k.gesture = Mapping{}
	k.overflow = false
}

func (k *keyCaptureCanvas) KeyDown(event *fyne.KeyEvent) {
	if event.Name == fyne.KeyEscape {
		k.resetGesture()
		k.onCancel()
		return
	}
	if k.pressed[event.Name] {
		return
	}
	k.pressed[event.Name] = true
	if modifier, ok := modifierForKey(event.Name); ok {
		k.gesture.Modifier |= modifier
		return
	}
	keycode, ok := FyneToHID[event.Name]
	if !ok || containsKeycode(k.gesture.Keys[:k.gesture.KeyCount()], keycode) {
		return
	}
	count := k.gesture.KeyCount()
	if count >= maxMappingKeys {
		k.overflow = true
		k.onStatus("A chord can contain at most three regular keys.")
		return
	}
	k.gesture.Keys[count] = keycode
	k.onStatus("Capturing: " + FormatMapping(k.gesture) + " — release all keys to accept")
}

func (k *keyCaptureCanvas) KeyUp(event *fyne.KeyEvent) {
	delete(k.pressed, event.Name)
	if len(k.pressed) != 0 {
		return
	}
	gesture := k.gesture
	overflow := k.overflow
	k.resetGesture()
	if !overflow && gesture.KeyCount() > 0 {
		k.onCapture(gesture)
	}
}

func (k *keyCaptureCanvas) TypedRune(r rune) {
	if len(k.pressed) > 0 {
		return
	}
	keycode, shifted := ShiftedRuneToHID[r]
	if shifted {
		k.onCapture(SingleKeyMapping(MOD_LSHIFT, keycode))
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
	k.onCapture(SingleKeyMapping(modifier, keycode))
}

func (k *keyCaptureCanvas) TypedKey(event *fyne.KeyEvent) {
	if len(k.pressed) > 0 {
		return
	}
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
	k.onCapture(SingleKeyMapping(modifier, keycode))
}

var builtinShortcuts = map[string]uint8{
	"SelectAll": HID_KEY_A, "Copy": HID_KEY_C, "Paste": HID_KEY_V,
	"Cut": HID_KEY_X, "Undo": HID_KEY_Z, "Redo": HID_KEY_Y,
	"Find": HID_KEY_F, "New": HID_KEY_N, "Open": HID_KEY_O,
	"Save": HID_KEY_S, "Print": HID_KEY_P, "CloseWindow": HID_KEY_W, "Quit": HID_KEY_Q,
}

func (k *keyCaptureCanvas) TypedShortcut(shortcut fyne.Shortcut) {
	if len(k.pressed) > 0 {
		return
	}
	if keycode, ok := builtinShortcuts[shortcut.ShortcutName()]; ok {
		modifier := uint8(MOD_LCTRL)
		if fyne.KeyModifierShortcutDefault == fyne.KeyModifierSuper {
			modifier = MOD_LGUI
		}
		k.onCapture(SingleKeyMapping(modifier, keycode))
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
	k.onCapture(SingleKeyMapping(modifier, keycode))
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
