package main

import (
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"go.bug.st/serial"
)

type fakePort struct {
	readCh    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	remaining []byte
	onWrite   func(string)
}

func newFakePort() *fakePort {
	return &fakePort{readCh: make(chan []byte, 32), closed: make(chan struct{})}
}

func (f *fakePort) feed(line string) { f.readCh <- []byte(line + "\n") }

func (f *fakePort) Read(buffer []byte) (int, error) {
	f.mu.Lock()
	if len(f.remaining) > 0 {
		n := copy(buffer, f.remaining)
		f.remaining = f.remaining[n:]
		f.mu.Unlock()
		return n, nil
	}
	f.mu.Unlock()
	select {
	case data := <-f.readCh:
		f.mu.Lock()
		n := copy(buffer, data)
		f.remaining = append(f.remaining[:0], data[n:]...)
		f.mu.Unlock()
		return n, nil
	case <-f.closed:
		return 0, io.EOF
	}
}

func (f *fakePort) Write(data []byte) (int, error) {
	if f.onWrite != nil {
		f.onWrite(strings.TrimSpace(string(data)))
	}
	return len(data), nil
}

func (f *fakePort) Close() error {
	f.closeOnce.Do(func() { close(f.closed) })
	return nil
}

func (f *fakePort) SetReadTimeout(time.Duration) error { return nil }

func TestParseLayoutValidation(t *testing.T) {
	valid := "0:104,1:105,2:106,3:107,4:108,5:109,6:110,7:111,8:112,255:113"
	layout, err := parseLayout(valid, numInputs)
	if err != nil {
		t.Fatalf("parseLayout(valid): %v", err)
	}
	if layout[9] != (Mapping{255, 113}) {
		t.Fatalf("unexpected final mapping: %v", layout[9])
	}
	invalid := []string{
		"0:104", strings.Replace(valid, "255:113", "256:113", 1),
		strings.Replace(valid, "5:109", "x:109", 1),
		strings.Replace(valid, "6:110", "6", 1),
	}
	for _, response := range invalid {
		if _, err := parseLayout(response, numInputs); err == nil {
			t.Errorf("parseLayout(%q) unexpectedly succeeded", response)
		}
	}
}

func TestParseKeyEvent(t *testing.T) {
	event := parseKeyEvent("KEY:6:0x2:0x6E")
	if event == nil || event.Index != 6 || event.Modifier != 2 || event.Keycode != 0x6E {
		t.Fatalf("unexpected event: %#v", event)
	}
	for _, line := range []string{"ready", "KEY:32:0x0:0x68", "KEY:x:0x0:0x68", "KEY:1:bad:0x68"} {
		if parseKeyEvent(line) != nil {
			t.Errorf("parseKeyEvent(%q) unexpectedly succeeded", line)
		}
	}
}

func TestDeviceSessionAndLegacyFallback(t *testing.T) {
	fake := newFakePort()
	fake.onWrite = func(command string) {
		switch {
		case command == "GET_INFO":
			fake.feed("WirelessTourbox ready")
			fake.feed("ERR:UNKNOWN")
		case command == "GET_LAYOUT":
			fake.feed("0:104,0:105,0:106,0:107,0:108,0:109,0:110,0:111,0:112,0:113")
		case strings.HasPrefix(command, "SET_KEY:"):
			fake.feed("OK")
		}
	}
	previousOpen := openSerialPort
	openSerialPort = func(string, *serial.Mode) (serialPort, error) { return fake, nil }
	defer func() { openSerialPort = previousOpen }()

	device := &Device{}
	if err := device.Connect("fake"); err != nil {
		t.Fatal(err)
	}
	if err := device.Identify(); err != nil {
		t.Fatalf("legacy identify: %v", err)
	}
	if device.ProtocolVersion() != 0 {
		t.Fatalf("legacy protocol version = %d", device.ProtocolVersion())
	}
	if err := device.GetLayout(); err != nil {
		t.Fatalf("get layout: %v", err)
	}

	var wait sync.WaitGroup
	for i := 0; i < 4; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			if err := device.SetKey(index, uint8(index), uint8(20+index)); err != nil {
				t.Errorf("SetKey(%d): %v", index, err)
			}
		}(i)
	}
	wait.Wait()
	if got := device.GetLayoutData()[3]; got != (Mapping{3, 23}) {
		t.Fatalf("updated mapping = %v", got)
	}
	events := device.EventChan()
	if err := device.Disconnect(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("event channel remained open")
		}
	case <-time.After(time.Second):
		t.Fatal("event channel did not close")
	}
}

func TestNewProtocolAtomicReset(t *testing.T) {
	fake := newFakePort()
	commands := make(chan string, 8)
	fake.onWrite = func(command string) {
		commands <- command
		switch command {
		case "GET_INFO":
			fake.feed("INFO:WirelessTourbox:1")
		case "RESET_DEFAULTS":
			fake.feed("OK")
		}
	}
	previousOpen := openSerialPort
	openSerialPort = func(string, *serial.Mode) (serialPort, error) { return fake, nil }
	defer func() { openSerialPort = previousOpen }()

	device := &Device{}
	if err := device.Connect("fake"); err != nil {
		t.Fatal(err)
	}
	defer device.Disconnect()
	if err := device.Identify(); err != nil {
		t.Fatal(err)
	}
	if err := device.ResetDefaults(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(device.GetLayoutData(), defaultLayout) {
		t.Fatal("reset did not update cached defaults")
	}
	first, second := <-commands, <-commands
	if first != "GET_INFO" || second != "RESET_DEFAULTS" {
		t.Fatalf("commands = %q, %q", first, second)
	}
}

func TestProtocolTwoLoadsDynamicLayersAndEvents(t *testing.T) {
	fake := newFakePort()
	base := "0:104,0:105,0:106,0:107,0:108,0:109,0:110,0:111,0:112,0:113"
	layer := "0:0,1:4,0:0,0:0,0:0,0:0,0:79,0:80,0:0,0:0"
	fake.onWrite = func(command string) {
		switch command {
		case "GET_INFO":
			fake.feed("INFO:WirelessTourbox:2")
		case "GET_CAPS":
			fake.feed("CAPS:2:10:15")
		case "GET_INPUTS":
			fake.feed("INPUTS:0:S:1:Switch_1|1:S:1:Switch_2|2:S:1:Switch_3|3:S:1:Switch_4|4:B:0:Encoder_1_Click|5:B:0:Encoder_2_Click|6:E:0:Encoder_1_CW|7:E:0:Encoder_1_CCW|8:E:0:Encoder_2_CW|9:E:0:Encoder_2_CCW")
		case "GET_LAYER_CONFIG":
			fake.feed("LAYERCFG:250:1=0")
		case "GET_LAYOUT:0":
			fake.feed(base)
		case "GET_LAYOUT:1":
			fake.feed(layer)
		case "SET_KEY:1:1:1:4":
			fake.feed("OK")
		}
	}
	previousOpen := openSerialPort
	openSerialPort = func(string, *serial.Mode) (serialPort, error) { return fake, nil }
	defer func() { openSerialPort = previousOpen }()

	device := &Device{}
	if err := device.Connect("fake"); err != nil {
		t.Fatal(err)
	}
	defer device.Disconnect()
	if err := device.Identify(); err != nil {
		t.Fatal(err)
	}
	if err := device.LoadConfiguration(); err != nil {
		t.Fatal(err)
	}
	caps, inputs, config, layouts := device.Snapshot()
	if caps.InputCount != 10 || caps.MaxLayers != 15 || len(inputs) != 10 {
		t.Fatalf("unexpected dynamic capabilities: %#v, %d inputs", caps, len(inputs))
	}
	if config.HoldMS != 250 || len(config.Layers) != 1 || config.Layers[0] != (LayerDefinition{1, 0}) {
		t.Fatalf("unexpected layer configuration: %#v", config)
	}
	if got := layouts[1][1]; got != (Mapping{1, 4}) {
		t.Fatalf("layer mapping = %#v", got)
	}
	if err := device.SetLayerKey(1, 1, 1, 4); err != nil {
		t.Fatal(err)
	}
	fake.feed("LAYER:1:ON")
	select {
	case event := <-device.EventChan():
		if event.Kind != "layer" || event.Layer != 1 || event.Action != "on" {
			t.Fatalf("unexpected layer event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("layer event was not routed")
	}
}

func TestParseLayerConfigurationRejectsDuplicates(t *testing.T) {
	if _, err := parseLayerConfig("LAYERCFG:200:1=0,2=0", 15, 10); err == nil {
		t.Fatal("duplicate trigger unexpectedly accepted")
	}
	if _, err := parseLayerConfig("LAYERCFG:99:", 15, 10); err == nil {
		t.Fatal("out-of-range hold threshold unexpectedly accepted")
	}
}

func TestKeyChoicesAndModifierOrder(t *testing.T) {
	if FormatKey(MOD_LCTRL, 0) != "No Action" || KeyCategory(0) != "Actions" {
		t.Fatal("No Action mapping is not represented consistently")
	}
	if got := ModifierString(MOD_RGUI | MOD_LCTRL | MOD_LALT); got != "LCtrl+LAlt+RGui" {
		t.Fatalf("ModifierString order = %q", got)
	}
	functions := KeyChoices("Function")
	if !contains(functions, "F1") || !contains(functions, "F24") {
		t.Fatalf("function choices incomplete: %v", functions)
	}
	if code, ok := KeyCodeByName("Keypad Enter"); !ok || code != HID_KEY_KPENTER {
		t.Fatalf("keypad lookup = 0x%02X, %v", code, ok)
	}
	if ShiftedRuneToHID['?'] != HID_KEY_SLASH || ShiftedRuneToHID['!'] != HID_KEY_1 {
		t.Fatal("shifted punctuation capture map is incomplete")
	}
}
