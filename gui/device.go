package main

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

const (
	baudRate       = 115200
	numInputs      = 10
	commandTimeout = 3 * time.Second
)

type PortInfo struct {
	Name        string
	Description string
}

type KeyEvent struct {
	Index    int
	Modifier uint8
	Keycode  uint8
	Action   string
}

type serialPort interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
	SetReadTimeout(time.Duration) error
}

var openSerialPort = func(name string, mode *serial.Mode) (serialPort, error) {
	return serial.Open(name, mode)
}

type deviceSession struct {
	port     serialPort
	respCh   chan string
	eventCh  chan KeyEvent
	errCh    chan error
	stopCh   chan struct{}
	stopOnce sync.Once
	doneCh   chan struct{}
}

func (s *deviceSession) stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		_ = s.port.Close()
	})
	<-s.doneCh
}

type Device struct {
	mu              sync.RWMutex
	commandMu       sync.Mutex
	session         *deviceSession
	layout          [numInputs][2]uint8
	portName        string
	protocolVersion int
}

func ListPorts() ([]PortInfo, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return nil, err
	}
	result := make([]PortInfo, 0, len(ports))
	for _, port := range ports {
		description := port
		upper := strings.ToUpper(port)
		if strings.Contains(upper, "ACM") || strings.Contains(upper, "COM") {
			description += " (likely USB serial)"
		}
		result = append(result, PortInfo{Name: port, Description: description})
	}
	return result, nil
}

func (d *Device) Connect(portName string) error {
	d.Disconnect()
	port, err := openSerialPort(portName, &serial.Mode{BaudRate: baudRate})
	if err != nil {
		return fmt.Errorf("open %s: %w", portName, err)
	}
	if err := port.SetReadTimeout(250 * time.Millisecond); err != nil {
		_ = port.Close()
		return fmt.Errorf("configure %s: %w", portName, err)
	}
	session := &deviceSession{
		port: port, respCh: make(chan string, 8), eventCh: make(chan KeyEvent, 100),
		errCh: make(chan error, 1), stopCh: make(chan struct{}), doneCh: make(chan struct{}),
	}
	d.mu.Lock()
	d.session = session
	d.portName = portName
	d.protocolVersion = 0
	d.mu.Unlock()
	go d.readLoop(session)
	return nil
}

func (d *Device) Disconnect() error {
	d.mu.Lock()
	session := d.session
	d.session = nil
	d.protocolVersion = 0
	d.mu.Unlock()
	if session == nil {
		return nil
	}
	session.stop()
	return nil
}

func (d *Device) IsConnected() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.session != nil
}

func (d *Device) PortName() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.portName
}

func (d *Device) ProtocolVersion() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.protocolVersion
}

func (d *Device) EventChan() <-chan KeyEvent {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.session == nil {
		return nil
	}
	return d.session.eventCh
}

func (d *Device) ErrorChan() <-chan error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.session == nil {
		return nil
	}
	return d.session.errCh
}

func (d *Device) readLoop(session *deviceSession) {
	defer close(session.doneCh)
	defer close(session.eventCh)
	defer close(session.errCh)
	buffer := make([]byte, 256)
	pending := make([]byte, 0, 256)
	for {
		n, err := session.port.Read(buffer)
		if n > 0 {
			pending = append(pending, buffer[:n]...)
			for {
				newline := bytes.IndexByte(pending, '\n')
				if newline < 0 {
					break
				}
				d.routeLine(session, strings.TrimSpace(string(pending[:newline])))
				pending = pending[newline+1:]
			}
			if len(pending) > 4096 {
				pending = pending[:0]
			}
		}
		if err != nil {
			select {
			case <-session.stopCh:
				return
			default:
			}
			d.mu.Lock()
			if d.session == session {
				d.session = nil
			}
			d.mu.Unlock()
			select {
			case session.errCh <- fmt.Errorf("serial connection lost: %w", err):
			default:
			}
			_ = session.port.Close()
			return
		}
		select {
		case <-session.stopCh:
			return
		default:
		}
	}
}

func (d *Device) routeLine(session *deviceSession, line string) {
	if line == "" {
		return
	}
	if event := parseKeyEvent(line); event != nil {
		select {
		case session.eventCh <- *event:
		default:
		}
		return
	}
	select {
	case session.respCh <- line:
	default:
	}
}

func parseKeyEvent(line string) *KeyEvent {
	if !strings.HasPrefix(line, "KEY:") {
		return nil
	}
	parts := strings.Split(line, ":")
	if len(parts) != 4 {
		return nil
	}
	index, err := strconv.Atoi(parts[1])
	if err != nil || index < 0 || index >= numInputs {
		return nil
	}
	modifier, err := strconv.ParseUint(strings.TrimPrefix(parts[2], "0x"), 16, 8)
	if err != nil {
		return nil
	}
	keycode, err := strconv.ParseUint(strings.TrimPrefix(parts[3], "0x"), 16, 8)
	if err != nil {
		return nil
	}
	return &KeyEvent{Index: index, Modifier: uint8(modifier), Keycode: uint8(keycode), Action: "pressed"}
}

func responseMatches(command, response string) bool {
	if strings.HasPrefix(response, "ERR:") {
		return true
	}
	switch {
	case command == "GET_INFO":
		return strings.HasPrefix(response, "INFO:")
	case command == "GET_LAYOUT":
		return strings.Count(response, ",") == numInputs-1
	case command == "RESET_DEFAULTS", strings.HasPrefix(command, "SET_KEY:"):
		return response == "OK"
	default:
		return true
	}
}

func (d *Device) sendCommand(command string) (string, error) {
	d.commandMu.Lock()
	defer d.commandMu.Unlock()
	d.mu.RLock()
	session := d.session
	d.mu.RUnlock()
	if session == nil {
		return "", fmt.Errorf("not connected")
	}
	for {
		select {
		case <-session.respCh:
		default:
			goto drained
		}
	}

drained:
	if _, err := session.port.Write([]byte(command + "\n")); err != nil {
		return "", fmt.Errorf("write command: %w", err)
	}
	timer := time.NewTimer(commandTimeout)
	defer timer.Stop()
	for {
		select {
		case response := <-session.respCh:
			if responseMatches(command, response) {
				return response, nil
			}
		case <-session.doneCh:
			return "", fmt.Errorf("device disconnected")
		case <-timer.C:
			return "", fmt.Errorf("timeout waiting for %s response", strings.Split(command, ":")[0])
		}
	}
}

func (d *Device) Identify() error {
	response, err := d.sendCommand("GET_INFO")
	if err != nil {
		return err
	}
	version := 0
	if response == "ERR:UNKNOWN" {
		// Firmware before protocol versioning is accepted after layout validation.
	} else {
		parts := strings.Split(response, ":")
		if len(parts) != 3 || parts[0] != "INFO" || parts[1] != "WirelessTourbox" {
			return fmt.Errorf("selected port is not a WirelessTourbox")
		}
		version, err = strconv.Atoi(parts[2])
		if err != nil || version < 1 {
			return fmt.Errorf("unsupported protocol response: %s", response)
		}
	}
	d.mu.Lock()
	d.protocolVersion = version
	d.mu.Unlock()
	return nil
}

func parseLayout(response string) ([numInputs][2]uint8, error) {
	var layout [numInputs][2]uint8
	pairs := strings.Split(response, ",")
	if len(pairs) != numInputs {
		return layout, fmt.Errorf("expected %d mappings, got %d", numInputs, len(pairs))
	}
	for i, pair := range pairs {
		parts := strings.Split(pair, ":")
		if len(parts) != 2 {
			return layout, fmt.Errorf("invalid mapping %q", pair)
		}
		modifier, err := strconv.ParseUint(parts[0], 10, 8)
		if err != nil {
			return layout, fmt.Errorf("invalid modifier in mapping %d", i)
		}
		keycode, err := strconv.ParseUint(parts[1], 10, 8)
		if err != nil {
			return layout, fmt.Errorf("invalid keycode in mapping %d", i)
		}
		layout[i] = [2]uint8{uint8(modifier), uint8(keycode)}
	}
	return layout, nil
}

func (d *Device) GetLayout() error {
	response, err := d.sendCommand("GET_LAYOUT")
	if err != nil {
		return err
	}
	if strings.HasPrefix(response, "ERR:") {
		return fmt.Errorf("device error: %s", response)
	}
	layout, err := parseLayout(response)
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.layout = layout
	d.mu.Unlock()
	return nil
}

func (d *Device) SetKey(index int, modifier, keycode uint8) error {
	if index < 0 || index >= numInputs {
		return fmt.Errorf("invalid index: %d", index)
	}
	response, err := d.sendCommand(fmt.Sprintf("SET_KEY:%d:%d:%d", index, modifier, keycode))
	if err != nil {
		return err
	}
	if response != "OK" {
		return fmt.Errorf("device rejected mapping: %s", response)
	}
	d.mu.Lock()
	d.layout[index] = [2]uint8{modifier, keycode}
	d.mu.Unlock()
	return nil
}

var defaultLayout = [numInputs][2]uint8{
	{0x00, 0x68}, {0x00, 0x69}, {0x00, 0x6A}, {0x00, 0x6B},
	{0x00, 0x6C}, {0x00, 0x6D}, {0x00, 0x6E}, {0x00, 0x6F},
	{0x00, 0x70}, {0x00, 0x71},
}

func (d *Device) ResetDefaults() error {
	if d.ProtocolVersion() >= 1 {
		response, err := d.sendCommand("RESET_DEFAULTS")
		if err != nil {
			return err
		}
		if response != "OK" {
			return fmt.Errorf("device rejected reset: %s", response)
		}
		d.mu.Lock()
		d.layout = defaultLayout
		d.mu.Unlock()
		return nil
	}
	for i, mapping := range defaultLayout {
		if err := d.SetKey(i, mapping[0], mapping[1]); err != nil {
			return fmt.Errorf("reset input %d: %w", i, err)
		}
	}
	return nil
}

func (d *Device) GetLayoutData() [numInputs][2]uint8 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.layout
}
