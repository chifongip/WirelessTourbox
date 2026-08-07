package main

import (
	"io"
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
	layout, err := parseLayout(valid)
	if err != nil {
		t.Fatalf("parseLayout(valid): %v", err)
	}
	if layout[9] != [2]uint8{255, 113} {
		t.Fatalf("unexpected final mapping: %v", layout[9])
	}
	invalid := []string{
		"0:104", strings.Replace(valid, "255:113", "256:113", 1),
		strings.Replace(valid, "5:109", "x:109", 1),
		strings.Replace(valid, "6:110", "6", 1),
	}
	for _, response := range invalid {
		if _, err := parseLayout(response); err == nil {
			t.Errorf("parseLayout(%q) unexpectedly succeeded", response)
		}
	}
}

func TestParseKeyEvent(t *testing.T) {
	event := parseKeyEvent("KEY:6:0x2:0x6E")
	if event == nil || event.Index != 6 || event.Modifier != 2 || event.Keycode != 0x6E {
		t.Fatalf("unexpected event: %#v", event)
	}
	for _, line := range []string{"ready", "KEY:10:0x0:0x68", "KEY:x:0x0:0x68", "KEY:1:bad:0x68"} {
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
	if got := device.GetLayoutData()[3]; got != [2]uint8{3, 23} {
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
	if device.GetLayoutData() != defaultLayout {
		t.Fatal("reset did not update cached defaults")
	}
	first, second := <-commands, <-commands
	if first != "GET_INFO" || second != "RESET_DEFAULTS" {
		t.Fatalf("commands = %q, %q", first, second)
	}
}

func TestKeyChoicesAndModifierOrder(t *testing.T) {
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
