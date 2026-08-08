package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
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

func TestResponsiveRowsKeepActionColumnSeparate(t *testing.T) {
	testApp := fynetest.NewApp()
	defer testApp.Quit()
	rows := []*fyne.Container{
		mappingRow(widget.NewLabel("0"), widget.NewLabel("A very long input name"),
			widget.NewLabel("Ctrl+Alt+Shift+A+B+C"), widget.NewButton("Edit", nil)),
		keyEditorRow(widget.NewLabel("Key 1"), widget.NewSelect([]string{"Navigation"}, nil),
			widget.NewSelect([]string{"A very long key name"}, nil), widget.NewButton("Remove", nil)),
	}
	for index, row := range rows {
		row.Resize(fyne.NewSize(480, mappingRowHeight))
		content, action := row.Objects[0], row.Objects[2]
		if content.Position().X+content.Size().Width > action.Position().X {
			t.Fatalf("row %d content overlaps action: content=%v/%v action=%v/%v",
				index, content.Position(), content.Size(), action.Position(), action.Size())
		}
	}
}

func TestCompoundKeyCapture(t *testing.T) {
	var captured Mapping
	capture := newKeyCaptureCanvas(func(mapping Mapping) {
		captured = mapping
	}, func() {}, func(string) {})
	capture.KeyDown(&fyne.KeyEvent{Name: desktop.KeyControlLeft})
	capture.KeyDown(&fyne.KeyEvent{Name: desktop.KeyAltLeft})
	capture.KeyDown(&fyne.KeyEvent{Name: fyne.KeyA})
	capture.KeyDown(&fyne.KeyEvent{Name: fyne.KeyB})
	capture.KeyUp(&fyne.KeyEvent{Name: fyne.KeyB})
	capture.KeyUp(&fyne.KeyEvent{Name: fyne.KeyA})
	capture.KeyUp(&fyne.KeyEvent{Name: desktop.KeyAltLeft})
	capture.KeyUp(&fyne.KeyEvent{Name: desktop.KeyControlLeft})
	want := Mapping{Modifier: MOD_LCTRL | MOD_LALT, Keys: [3]uint8{HID_KEY_A, HID_KEY_B}}
	if captured != want {
		t.Fatalf("captured mapping = %#v, want %#v", captured, want)
	}
}

func TestMappingSelectionSurvivesRebuild(t *testing.T) {
	testApp := fynetest.NewApp()
	defer testApp.Quit()
	window := testApp.NewWindow("WirelessTourbox selection test")
	device := &Device{
		protocolVersion: 3,
		capabilities:    DeviceCapabilities{Protocol: 3, InputCount: 2, MaxLayers: 15, MaxKeys: 3},
		inputs: []InputDescriptor{
			{Index: 0, Kind: "S", LayerEligible: true, Name: "Switch 1"},
			{Index: 1, Kind: "E", Name: "Encoder 1 CW"},
		},
		layerConfig: LayerConfig{HoldMS: 200, Layers: []LayerDefinition{{Slot: 1, Trigger: 0}}},
		layouts: map[int][]Mapping{
			0: {SingleKeyMapping(0, HID_KEY_A), SingleKeyMapping(0, HID_KEY_B)},
			1: {{}, SingleKeyMapping(MOD_LCTRL, HID_KEY_A)},
		},
	}
	ui := NewApp(window, device)
	ui.BuildUI()
	defer ui.Close()

	ui.focusMapping(1, 1)
	if ui.selectedLayer != 1 || ui.highlightedRows[1] != 1 {
		t.Fatalf("focus = layer %d, row %d", ui.selectedLayer, ui.highlightedRows[1])
	}
	ui.rebuildMappings()
	if ui.selectedLayer != 1 || ui.mappingTabs.SelectedIndex() != ui.tabByLayer[1] || ui.highlightedRows[1] != 1 {
		t.Fatal("mapping rebuild lost selected layer or highlighted row")
	}
	ui.focusMapping(1, 0)
	if ui.highlightedRows[1] != 0 {
		t.Fatal("ordinary row container did not accept programmatic highlight")
	}
	row := ui.mappingRows[1][1]
	if row.input.Text != "Encoder 1 CW" || row.mapping.Text != FormatMapping(SingleKeyMapping(MOD_LCTRL, HID_KEY_A)) {
		t.Fatalf("mapping row content is missing: input=%q mapping=%q", row.input.Text, row.mapping.Text)
	}
	if !row.input.Visible() || !row.mapping.Visible() || !row.button.Visible() || row.button.OnTapped == nil {
		t.Fatal("mapping row text or action button is unavailable")
	}
	row.button.Enable()
	fynetest.Tap(row.button)
	if ui.highlightedRows[1] != 1 {
		t.Fatal("mapping row action button did not receive the tap")
	}
	ui.navigateForDeviceEvent(DeviceEvent{Kind: "layer", Layer: 1, Action: "on"})
	if ui.selectedLayer != 1 || ui.highlightedRows[1] != 0 {
		t.Fatal("layer activation did not select its trigger row")
	}
	ui.navigateForDeviceEvent(DeviceEvent{Kind: "layer", Layer: 1, Action: "off"})
	if ui.selectedLayer != 1 {
		t.Fatal("layer release unexpectedly changed the selected tab")
	}
	ui.navigateForDeviceEvent(DeviceEvent{Kind: "key", Layer: 0, Index: 1})
	if ui.selectedLayer != 1 || ui.highlightedRows[1] != 1 {
		t.Fatal("physical input did not stay on the selected layer and highlight its row")
	}
}

func TestDisplayLayersUsesNaturalNameOrder(t *testing.T) {
	inputs := []InputDescriptor{
		{Index: 0, Name: "Switch 10"},
		{Index: 1, Name: "Switch 2"},
		{Index: 2, Name: "Switch 1"},
	}
	layers := displayLayers(inputs, []LayerDefinition{
		{Slot: 1, Trigger: 0},
		{Slot: 3, Trigger: 1},
		{Slot: 2, Trigger: 2},
	})
	want := []string{"Switch 1 Layer", "Switch 2 Layer", "Switch 10 Layer"}
	for index, name := range want {
		if layers[index].name != name {
			t.Fatalf("layer %d = %q, want %q", index, layers[index].name, name)
		}
	}
}

func TestPhysicalInputHighlightsLayerManager(t *testing.T) {
	testApp := fynetest.NewApp()
	defer testApp.Quit()
	ui := NewApp(testApp.NewWindow("Layer manager selection test"), &Device{
		layerConfig: LayerConfig{Layers: []LayerDefinition{{Slot: 2, Trigger: 0}, {Slot: 4, Trigger: 1}}},
	})
	rows := container.NewVBox()
	for index := 0; index < 2; index++ {
		highlight := newRowHighlight()
		view := &layerManagerRowView{
			Container: highlightedRow(highlight, widget.NewLabel("Layer")),
			highlight: highlight,
		}
		ui.layerManagerViews = append(ui.layerManagerViews, view)
		rows.Add(view.Container)
	}
	ui.layerManagerScroll = container.NewVScroll(rows)
	ui.layerManagerRows = map[int]int{2: 1, 4: 0}
	ui.layerManagerTriggerRows = map[int]int{0: 1, 1: 0}
	ui.layerManagerHighlighted = -1

	ui.navigateForDeviceEvent(DeviceEvent{Kind: "key", Index: 0})
	if ui.layerManagerHighlighted != 1 {
		t.Fatalf("physical trigger highlighted manager row %d, want 1", ui.layerManagerHighlighted)
	}
	ui.navigateForDeviceEvent(DeviceEvent{Kind: "layer", Layer: 4, Action: "on"})
	if ui.layerManagerHighlighted != 0 {
		t.Fatalf("layer activation highlighted manager row %d, want 0", ui.layerManagerHighlighted)
	}
}

func TestRowHighlightUsesSeparateIndicatorCell(t *testing.T) {
	highlight := newRowHighlight()
	label := widget.NewLabel("Visible content")
	row := highlightedRow(highlight, label)
	row.Resize(fyne.NewSize(480, mappingRowHeight))
	setRowHighlighted(highlight, true)
	_, _, _, alpha := highlight.FillColor.RGBA()
	if alpha == 0 {
		t.Fatal("highlight indicator is not visible")
	}
	if label.Position().X < rowHighlightWidth || label.Size().Width == 0 {
		t.Fatalf("content overlaps indicator: position=%v size=%v", label.Position(), label.Size())
	}
}
