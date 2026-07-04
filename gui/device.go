package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

const (
	baudRate    = 115200
	numInputs   = 10
	readTimeout = 500 * time.Millisecond
)

// PortInfo holds information about a serial port.
type PortInfo struct {
	Name        string
	Description string
}

// KeyEvent represents an input event from the device.
type KeyEvent struct {
	Index    int
	Modifier uint8
	Keycode  uint8
	Action   string
}

// Device represents a connected WirelessTourbox device.
type Device struct {
	port       serial.Port
	connected  bool
	layout     [numInputs][2]uint8
	portName   string
	mu         sync.Mutex
	respCh     chan string
	eventCh    chan KeyEvent
	stopReadCh chan struct{}
}

// ListPorts returns available serial ports.
func ListPorts() ([]PortInfo, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return nil, err
	}
	var result []PortInfo
	for _, p := range ports {
		info := PortInfo{Name: p, Description: p}
		if strings.Contains(p, "ACM") || strings.Contains(p, "COM") {
			info.Description = p + " (Serial)"
		}
		result = append(result, info)
	}
	return result, nil
}

// Connect opens a serial connection to the device.
func (d *Device) Connect(portName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.connected {
		d.disconnectLocked()
	}
	mode := &serial.Mode{BaudRate: baudRate}
	port, err := serial.Open(portName, mode)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", portName, err)
	}
	port.SetReadTimeout(readTimeout)
	d.port = port
	d.connected = true
	d.portName = portName
	d.respCh = make(chan string, 1)
	d.eventCh = make(chan KeyEvent, 100)
	d.stopReadCh = make(chan struct{})

	// Drain startup message
	time.Sleep(500 * time.Millisecond)
	buf := make([]byte, 256)
	for {
		n, _ := port.Read(buf)
		if n == 0 {
			break
		}
	}

	go d.readLoop()
	return nil
}

// Disconnect closes the serial connection.
func (d *Device) Disconnect() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.disconnectLocked()
}

func (d *Device) disconnectLocked() error {
	if d.port != nil {
		close(d.stopReadCh)
		err := d.port.Close()
		d.port = nil
		d.connected = false
		// Close event channel so monitor goroutine exits
		close(d.eventCh)
		return err
	}
	return nil
}

// IsConnected returns whether the device is connected.
func (d *Device) IsConnected() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.connected
}

// PortName returns the connected port name.
func (d *Device) PortName() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.portName
}

// EventChan returns the channel for KEY events.
func (d *Device) EventChan() <-chan KeyEvent {
	return d.eventCh
}

// readLoop reads all lines from the serial port and routes them.
func (d *Device) readLoop() {
	for {
		select {
		case <-d.stopReadCh:
			return
		default:
		}
		line, err := d.readLine()
		if err != nil {
			if !d.connected {
				return
			}
			continue
		}
		if strings.HasPrefix(line, "KEY:") {
			evt := parseKeyEvent(line)
			if evt != nil {
				select {
				case d.eventCh <- *evt:
				default:
				}
			}
		} else {
			select {
			case d.respCh <- line:
			default:
			}
		}
	}
}

// readLine reads a single line from the serial port.
func (d *Device) readLine() (string, error) {
	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := d.port.Read(buf)
		if n > 0 {
			if buf[0] == '\n' || buf[0] == '\r' {
				if len(line) > 0 {
					return string(line), nil
				}
				continue
			}
			line = append(line, buf[0])
		}
		if err != nil {
			if len(line) > 0 {
				return string(line), err
			}
			return "", err
		}
	}
}

// parseKeyEvent parses a KEY:<index>:0x<mod>:0x<key> line.
func parseKeyEvent(line string) *KeyEvent {
	parts := strings.Split(line, ":")
	if len(parts) != 4 {
		return nil
	}
	idx, err := strconv.Atoi(parts[1])
	if err != nil || idx < 0 || idx >= numInputs {
		return nil
	}
	mod, err := strconv.ParseUint(strings.TrimPrefix(parts[2], "0x"), 16, 8)
	if err != nil {
		return nil
	}
	key, err := strconv.ParseUint(strings.TrimPrefix(parts[3], "0x"), 16, 8)
	if err != nil {
		return nil
	}
	return &KeyEvent{Index: idx, Modifier: uint8(mod), Keycode: uint8(key), Action: "pressed"}
}

// sendCommand sends a command and waits for the response.
func (d *Device) sendCommand(cmd string) (string, error) {
	d.mu.Lock()
	if !d.connected {
		d.mu.Unlock()
		return "", fmt.Errorf("not connected")
	}
	// Drain any stale response from a previous timeout
	select {
	case <-d.respCh:
	default:
	}
	_, err := d.port.Write([]byte(cmd + "\n"))
	d.mu.Unlock()
	if err != nil {
		return "", fmt.Errorf("write error: %w", err)
	}
	select {
	case resp := <-d.respCh:
		return resp, nil
	case <-time.After(3 * time.Second):
		return "", fmt.Errorf("timeout waiting for response")
	}
}

// GetLayout reads the current key layout from the device.
func (d *Device) GetLayout() error {
	resp, err := d.sendCommand("GET_LAYOUT")
	if err != nil {
		return err
	}
	if strings.HasPrefix(resp, "ERR") {
		return fmt.Errorf("device error: %s", resp)
	}
	pairs := strings.Split(resp, ",")
	if len(pairs) != numInputs {
		return fmt.Errorf("expected %d pairs, got %d", numInputs, len(pairs))
	}
	for i, pair := range pairs {
		parts := strings.Split(pair, ":")
		if len(parts) != 2 {
			return fmt.Errorf("invalid pair format: %s", pair)
		}
		mod, _ := strconv.Atoi(parts[0])
		key, _ := strconv.Atoi(parts[1])
		d.layout[i][0] = uint8(mod)
		d.layout[i][1] = uint8(key)
	}
	return nil
}

// SetKey sends a SET_KEY command to remap an input.
func (d *Device) SetKey(index int, mod, keycode uint8) error {
	if index < 0 || index >= numInputs {
		return fmt.Errorf("invalid index: %d", index)
	}
	cmd := fmt.Sprintf("SET_KEY:%d:%d:%d", index, mod, keycode)
	resp, err := d.sendCommand(cmd)
	if err != nil {
		return err
	}
	if resp != "OK" {
		return fmt.Errorf("device rejected: %s", resp)
	}
	d.layout[index][0] = mod
	d.layout[index][1] = keycode
	return nil
}

// ResetDefaults sends SET_KEY commands for all default mappings.
func (d *Device) ResetDefaults() error {
	defaults := [numInputs][2]uint8{
		{0x00, 0x68}, {0x00, 0x69}, {0x00, 0x6A}, {0x00, 0x6B},
		{0x00, 0x6C}, {0x00, 0x6D}, {0x00, 0x6E}, {0x00, 0x6F},
		{0x00, 0x70}, {0x00, 0x71},
	}
	for i := 0; i < numInputs; i++ {
		if err := d.SetKey(i, defaults[i][0], defaults[i][1]); err != nil {
			return fmt.Errorf("failed to reset input %d: %w", i, err)
		}
	}
	return nil
}

// GetLayoutData returns the current layout.
func (d *Device) GetLayoutData() [numInputs][2]uint8 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.layout
}
