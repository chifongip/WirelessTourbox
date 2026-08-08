package main

import (
	"testing"

	"fyne.io/fyne/v2"
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

	selected := -1
	ui.mappingLists[1].OnSelected = func(id widget.ListItemID) { selected = id }
	ui.focusMapping(1, 1)
	if ui.selectedLayer != 1 || selected != 1 {
		t.Fatalf("focus = layer %d, row %d", ui.selectedLayer, selected)
	}
	ui.rebuildMappings()
	if ui.selectedLayer != 1 || ui.mappingTabs.SelectedIndex() != ui.tabByLayer[1] || ui.highlightedRows[1] != 1 {
		t.Fatal("mapping rebuild lost selected layer or highlighted row")
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
	if ui.selectedLayer != 0 || ui.highlightedRows[0] != 1 {
		t.Fatal("physical input did not select Base and highlight its row")
	}
}
