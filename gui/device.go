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
	numInputs      = 10 // Legacy firmware input count.
	commandTimeout = 3 * time.Second
	defaultHoldMS  = 200
	minimumHoldMS  = 100
	maximumHoldMS  = 1000
	maxMappingKeys = 3
)

type PortInfo struct {
	Name        string
	Description string
}

type Mapping struct {
	Modifier uint8
	Keys     [maxMappingKeys]uint8
}

func SingleKeyMapping(modifier, keycode uint8) Mapping {
	mapping := Mapping{Modifier: modifier, Keys: [maxMappingKeys]uint8{keycode}}
	if keycode == 0 {
		mapping.Modifier = 0
	}
	return mapping
}

func (m Mapping) KeyCount() int {
	for i, key := range m.Keys {
		if key == 0 {
			return i
		}
	}
	return len(m.Keys)
}

type InputDescriptor struct {
	Index         int
	Kind          string
	LayerEligible bool
	Name          string
}

type DeviceCapabilities struct {
	Protocol   int
	InputCount int
	MaxLayers  int
	MaxKeys    int
}

type LayerDefinition struct {
	Slot    int
	Trigger int
}

type LayerConfig struct {
	HoldMS int
	Layers []LayerDefinition
}

type DeviceEvent struct {
	Kind     string
	Index    int
	Layer    int
	Modifier uint8
	Keys     [maxMappingKeys]uint8
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
	eventCh  chan DeviceEvent
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
	portName        string
	protocolVersion int
	capabilities    DeviceCapabilities
	inputs          []InputDescriptor
	layerConfig     LayerConfig
	layouts         map[int][]Mapping
	runtimeLayer    int
}

var legacyInputs = []InputDescriptor{
	{0, "S", true, "Switch 1 (GPIO2)"}, {1, "S", true, "Switch 2 (GPIO3)"},
	{2, "S", true, "Switch 3 (GPIO4)"}, {3, "S", true, "Switch 4 (GPIO5)"},
	{4, "B", false, "Enc 1 Click (GPIO8)"}, {5, "B", false, "Enc 2 Click (GPIO12)"},
	{6, "E", false, "Encoder 1 CW"}, {7, "E", false, "Encoder 1 CCW"},
	{8, "E", false, "Encoder 2 CW"}, {9, "E", false, "Encoder 2 CCW"},
}

var defaultLayout = []Mapping{
	SingleKeyMapping(0x00, 0x68), SingleKeyMapping(0x00, 0x69),
	SingleKeyMapping(0x00, 0x6A), SingleKeyMapping(0x00, 0x6B),
	SingleKeyMapping(0x00, 0x6C), SingleKeyMapping(0x00, 0x6D),
	SingleKeyMapping(0x00, 0x6E), SingleKeyMapping(0x00, 0x6F),
	SingleKeyMapping(0x00, 0x70), SingleKeyMapping(0x00, 0x71),
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
		port: port, respCh: make(chan string, 8), eventCh: make(chan DeviceEvent, 100),
		errCh: make(chan error, 1), stopCh: make(chan struct{}), doneCh: make(chan struct{}),
	}
	d.mu.Lock()
	d.session = session
	d.portName = portName
	d.protocolVersion = 0
	d.capabilities = DeviceCapabilities{}
	d.inputs = nil
	d.layerConfig = LayerConfig{}
	d.layouts = make(map[int][]Mapping)
	d.runtimeLayer = 0
	d.mu.Unlock()
	go d.readLoop(session)
	return nil
}

func (d *Device) Disconnect() error {
	d.mu.Lock()
	session := d.session
	d.session = nil
	d.protocolVersion = 0
	d.capabilities = DeviceCapabilities{}
	d.inputs = nil
	d.layerConfig = LayerConfig{}
	d.layouts = nil
	d.runtimeLayer = 0
	d.mu.Unlock()
	if session != nil {
		session.stop()
	}
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

func (d *Device) EventChan() <-chan DeviceEvent {
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
				d.protocolVersion = 0
				d.capabilities = DeviceCapabilities{}
				d.inputs = nil
				d.layerConfig = LayerConfig{}
				d.layouts = nil
				d.runtimeLayer = 0
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
	if event := parseDeviceEvent(line); event != nil {
		d.mu.Lock()
		if event.Kind == "layer" {
			if event.Action == "on" {
				d.runtimeLayer = event.Layer
			} else if d.runtimeLayer == event.Layer {
				d.runtimeLayer = 0
			}
		} else if event.Kind == "key" {
			event.Layer = d.runtimeLayer
		}
		d.mu.Unlock()
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

func parseDeviceEvent(line string) *DeviceEvent {
	parts := strings.Split(line, ":")
	if len(parts) == 4 && parts[0] == "KEY" {
		index, err := strconv.Atoi(parts[1])
		if err != nil || index < 0 || index >= 32 {
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
		return &DeviceEvent{Kind: "key", Index: index, Modifier: uint8(modifier),
			Keys: [maxMappingKeys]uint8{uint8(keycode)}, Action: "pressed"}
	}
	if len(parts) == 3 && parts[0] == "LAYER" {
		layer, err := strconv.Atoi(parts[1])
		if err != nil || layer < 1 || layer > 15 || (parts[2] != "ON" && parts[2] != "OFF") {
			return nil
		}
		return &DeviceEvent{Kind: "layer", Layer: layer, Action: strings.ToLower(parts[2])}
	}
	return nil
}

// parseKeyEvent remains as a compatibility helper for existing callers and tests.
func parseKeyEvent(line string) *DeviceEvent { return parseDeviceEvent(line) }

func responseMatches(command, response string) bool {
	if strings.HasPrefix(response, "ERR:") {
		return true
	}
	switch {
	case command == "GET_INFO":
		return strings.HasPrefix(response, "INFO:")
	case command == "GET_CAPS":
		return strings.HasPrefix(response, "CAPS:")
	case command == "GET_INPUTS":
		return strings.HasPrefix(response, "INPUTS:")
	case command == "GET_LAYER_CONFIG":
		return strings.HasPrefix(response, "LAYERCFG:")
	case strings.HasPrefix(command, "GET_CHORDS:"):
		return strings.Contains(response, ":")
	case strings.HasPrefix(command, "GET_LAYOUT"):
		return strings.Contains(response, ":") && !strings.Contains(response, "WirelessTourbox ready")
	case command == "RESET_DEFAULTS", strings.HasPrefix(command, "SET_KEY:"),
		strings.HasPrefix(command, "SET_CHORD:"),
		strings.HasPrefix(command, "SET_LAYER:"), strings.HasPrefix(command, "REMOVE_LAYER:"),
		strings.HasPrefix(command, "SET_HOLD_MS:"):
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
	if response != "ERR:UNKNOWN" {
		parts := strings.Split(response, ":")
		if len(parts) != 3 || parts[0] != "INFO" || parts[1] != "WirelessTourbox" {
			return fmt.Errorf("selected port is not a WirelessTourbox")
		}
		version, err = strconv.Atoi(parts[2])
		if err != nil || version < 1 || version > 3 {
			return fmt.Errorf("unsupported protocol response: %s", response)
		}
	}
	d.mu.Lock()
	d.protocolVersion = version
	d.mu.Unlock()
	return nil
}

func parseLayout(response string, expected int) ([]Mapping, error) {
	pairs := strings.Split(response, ",")
	if len(pairs) != expected {
		return nil, fmt.Errorf("expected %d mappings, got %d", expected, len(pairs))
	}
	layout := make([]Mapping, expected)
	for i, pair := range pairs {
		parts := strings.Split(pair, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid mapping %q", pair)
		}
		modifier, err := strconv.ParseUint(parts[0], 10, 8)
		if err != nil {
			return nil, fmt.Errorf("invalid modifier in mapping %d", i)
		}
		keycode, err := strconv.ParseUint(parts[1], 10, 8)
		if err != nil {
			return nil, fmt.Errorf("invalid keycode in mapping %d", i)
		}
		layout[i] = SingleKeyMapping(uint8(modifier), uint8(keycode))
	}
	return layout, nil
}

func validateMapping(mapping Mapping) error {
	seenZero := false
	seen := make(map[uint8]bool)
	for _, key := range mapping.Keys {
		if key == 0 {
			seenZero = true
			continue
		}
		if seenZero {
			return fmt.Errorf("keys must be packed before empty slots")
		}
		if seen[key] {
			return fmt.Errorf("duplicate keycode %d", key)
		}
		seen[key] = true
	}
	if len(seen) == 0 && mapping.Modifier != 0 {
		return fmt.Errorf("No Action cannot include modifiers")
	}
	return nil
}

func parseChordLayout(response string, expected int) ([]Mapping, error) {
	entries := strings.Split(response, ",")
	if len(entries) != expected {
		return nil, fmt.Errorf("expected %d mappings, got %d", expected, len(entries))
	}
	layout := make([]Mapping, expected)
	for index, entry := range entries {
		parts := strings.Split(entry, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid chord mapping %q", entry)
		}
		modifier, err := strconv.ParseUint(parts[0], 10, 8)
		if err != nil {
			return nil, fmt.Errorf("invalid modifier in mapping %d", index)
		}
		keyParts := strings.Split(parts[1], "+")
		if len(keyParts) > maxMappingKeys || len(keyParts) == 0 {
			return nil, fmt.Errorf("invalid key count in mapping %d", index)
		}
		mapping := Mapping{Modifier: uint8(modifier)}
		for keyIndex, keyText := range keyParts {
			key, parseErr := strconv.ParseUint(keyText, 10, 8)
			if parseErr != nil {
				return nil, fmt.Errorf("invalid key in mapping %d", index)
			}
			mapping.Keys[keyIndex] = uint8(key)
		}
		if mapping.Keys[0] == 0 {
			if len(keyParts) != 1 || mapping.Modifier != 0 {
				return nil, fmt.Errorf("invalid No Action mapping %d", index)
			}
		}
		if err := validateMapping(mapping); err != nil {
			return nil, fmt.Errorf("invalid mapping %d: %w", index, err)
		}
		layout[index] = mapping
	}
	return layout, nil
}

func parseCapabilities(response string) (DeviceCapabilities, error) {
	parts := strings.Split(response, ":")
	if (len(parts) != 4 && len(parts) != 5) || parts[0] != "CAPS" {
		return DeviceCapabilities{}, fmt.Errorf("invalid capabilities: %s", response)
	}
	protocol, err1 := strconv.Atoi(parts[1])
	inputs, err2 := strconv.Atoi(parts[2])
	layers, err3 := strconv.Atoi(parts[3])
	if err1 != nil || err2 != nil || err3 != nil || protocol < 2 || protocol > 3 || inputs < 1 || inputs > 32 || layers < 1 || layers > 15 {
		return DeviceCapabilities{}, fmt.Errorf("invalid capabilities: %s", response)
	}
	maxKeys := 1
	if protocol >= 3 {
		if len(parts) != 5 {
			return DeviceCapabilities{}, fmt.Errorf("missing chord capability: %s", response)
		}
		maxKeys, err1 = strconv.Atoi(parts[4])
		if err1 != nil || maxKeys != maxMappingKeys {
			return DeviceCapabilities{}, fmt.Errorf("unsupported chord capability: %s", response)
		}
	} else if len(parts) != 4 {
		return DeviceCapabilities{}, fmt.Errorf("invalid capabilities: %s", response)
	}
	return DeviceCapabilities{Protocol: protocol, InputCount: inputs, MaxLayers: layers, MaxKeys: maxKeys}, nil
}

func parseInputs(response string, expected int) ([]InputDescriptor, error) {
	if !strings.HasPrefix(response, "INPUTS:") {
		return nil, fmt.Errorf("invalid input descriptors: %s", response)
	}
	entries := strings.Split(strings.TrimPrefix(response, "INPUTS:"), "|")
	if len(entries) != expected {
		return nil, fmt.Errorf("expected %d input descriptors, got %d", expected, len(entries))
	}
	inputs := make([]InputDescriptor, expected)
	seen := make([]bool, expected)
	for _, entry := range entries {
		parts := strings.SplitN(entry, ":", 4)
		if len(parts) != 4 {
			return nil, fmt.Errorf("invalid input descriptor %q", entry)
		}
		index, err := strconv.Atoi(parts[0])
		if err != nil || index < 0 || index >= expected || seen[index] || (parts[2] != "0" && parts[2] != "1") {
			return nil, fmt.Errorf("invalid input descriptor %q", entry)
		}
		seen[index] = true
		inputs[index] = InputDescriptor{index, parts[1], parts[2] == "1", strings.ReplaceAll(parts[3], "_", " ")}
	}
	return inputs, nil
}

func parseLayerConfig(response string, maxLayers, inputCount int) (LayerConfig, error) {
	parts := strings.SplitN(response, ":", 3)
	if len(parts) != 3 || parts[0] != "LAYERCFG" {
		return LayerConfig{}, fmt.Errorf("invalid layer configuration: %s", response)
	}
	hold, err := strconv.Atoi(parts[1])
	if err != nil || hold < minimumHoldMS || hold > maximumHoldMS {
		return LayerConfig{}, fmt.Errorf("invalid hold threshold: %s", parts[1])
	}
	config := LayerConfig{HoldMS: hold}
	usedSlots := make(map[int]bool)
	usedTriggers := make(map[int]bool)
	if parts[2] == "" {
		return config, nil
	}
	for _, item := range strings.Split(parts[2], ",") {
		pair := strings.Split(item, "=")
		if len(pair) != 2 {
			return LayerConfig{}, fmt.Errorf("invalid layer entry %q", item)
		}
		slot, err1 := strconv.Atoi(pair[0])
		trigger, err2 := strconv.Atoi(pair[1])
		if err1 != nil || err2 != nil || slot < 1 || slot > maxLayers || trigger < 0 || trigger >= inputCount || usedSlots[slot] || usedTriggers[trigger] {
			return LayerConfig{}, fmt.Errorf("invalid layer entry %q", item)
		}
		usedSlots[slot], usedTriggers[trigger] = true, true
		config.Layers = append(config.Layers, LayerDefinition{slot, trigger})
	}
	return config, nil
}

func (d *Device) LoadConfiguration() error {
	protocol := d.ProtocolVersion()
	if protocol < 2 {
		response, err := d.sendCommand("GET_LAYOUT")
		if err != nil {
			return err
		}
		layout, err := parseLayout(response, numInputs)
		if err != nil {
			return err
		}
		d.mu.Lock()
		d.capabilities = DeviceCapabilities{Protocol: protocol, InputCount: numInputs, MaxKeys: 1}
		d.inputs = append([]InputDescriptor(nil), legacyInputs...)
		d.layerConfig = LayerConfig{HoldMS: defaultHoldMS}
		d.layouts = map[int][]Mapping{0: layout}
		d.mu.Unlock()
		return nil
	}

	capsResponse, err := d.sendCommand("GET_CAPS")
	if err != nil {
		return err
	}
	caps, err := parseCapabilities(capsResponse)
	if err != nil {
		return err
	}
	if caps.Protocol != protocol {
		return fmt.Errorf("protocol mismatch: identity %d, capabilities %d", protocol, caps.Protocol)
	}
	inputsResponse, err := d.sendCommand("GET_INPUTS")
	if err != nil {
		return err
	}
	inputs, err := parseInputs(inputsResponse, caps.InputCount)
	if err != nil {
		return err
	}
	layersResponse, err := d.sendCommand("GET_LAYER_CONFIG")
	if err != nil {
		return err
	}
	layerConfig, err := parseLayerConfig(layersResponse, caps.MaxLayers, caps.InputCount)
	if err != nil {
		return err
	}
	layouts := make(map[int][]Mapping)
	layerNumbers := []int{0}
	for _, layer := range layerConfig.Layers {
		layerNumbers = append(layerNumbers, layer.Slot)
	}
	for _, layer := range layerNumbers {
		command := fmt.Sprintf("GET_LAYOUT:%d", layer)
		if protocol >= 3 {
			command = fmt.Sprintf("GET_CHORDS:%d", layer)
		}
		response, commandErr := d.sendCommand(command)
		if commandErr != nil {
			return commandErr
		}
		var layout []Mapping
		var parseErr error
		if protocol >= 3 {
			layout, parseErr = parseChordLayout(response, caps.InputCount)
		} else {
			layout, parseErr = parseLayout(response, caps.InputCount)
		}
		if parseErr != nil {
			return parseErr
		}
		layouts[layer] = layout
	}
	d.mu.Lock()
	d.capabilities = caps
	d.inputs = inputs
	d.layerConfig = layerConfig
	d.layouts = layouts
	d.mu.Unlock()
	return nil
}

func (d *Device) GetLayout() error { return d.LoadConfiguration() }

func (d *Device) SetKey(index int, modifier, keycode uint8) error {
	return d.SetLayerKey(0, index, modifier, keycode)
}

func (d *Device) SetLayerKey(layer, index int, modifier, keycode uint8) error {
	return d.SetLayerMapping(layer, index, SingleKeyMapping(modifier, keycode))
}

func chordKeyString(mapping Mapping) string {
	if mapping.KeyCount() == 0 {
		return "0"
	}
	parts := make([]string, mapping.KeyCount())
	for i := range parts {
		parts[i] = strconv.Itoa(int(mapping.Keys[i]))
	}
	return strings.Join(parts, "+")
}

func (d *Device) SetLayerMapping(layer, index int, mapping Mapping) error {
	if err := validateMapping(mapping); err != nil {
		return fmt.Errorf("invalid mapping: %w", err)
	}
	d.mu.RLock()
	inputCount := d.capabilities.InputCount
	protocol := d.protocolVersion
	_, layerExists := d.layouts[layer]
	d.mu.RUnlock()
	if index < 0 || index >= inputCount || layer < 0 || !layerExists {
		return fmt.Errorf("invalid layer or input: %d/%d", layer, index)
	}
	command := fmt.Sprintf("SET_KEY:%d:%d:%d", index, mapping.Modifier, mapping.Keys[0])
	if protocol >= 2 {
		command = fmt.Sprintf("SET_KEY:%d:%d:%d:%d", layer, index, mapping.Modifier, mapping.Keys[0])
	}
	if protocol >= 3 {
		command = fmt.Sprintf("SET_CHORD:%d:%d:%d:%s", layer, index,
			mapping.Modifier, chordKeyString(mapping))
	}
	response, err := d.sendCommand(command)
	if err != nil {
		return err
	}
	if response != "OK" {
		return fmt.Errorf("device rejected mapping: %s", response)
	}
	d.mu.Lock()
	d.layouts[layer][index] = mapping
	d.mu.Unlock()
	return nil
}

func (d *Device) SetLayer(slot, trigger int) error {
	if d.ProtocolVersion() < 2 {
		return fmt.Errorf("layers require firmware protocol 2")
	}
	response, err := d.sendCommand(fmt.Sprintf("SET_LAYER:%d:%d", slot, trigger))
	if err != nil {
		return err
	}
	if response != "OK" {
		return fmt.Errorf("device rejected layer: %s", response)
	}
	return d.LoadConfiguration()
}

func (d *Device) RemoveLayer(slot int) error {
	response, err := d.sendCommand(fmt.Sprintf("REMOVE_LAYER:%d", slot))
	if err != nil {
		return err
	}
	if response != "OK" {
		return fmt.Errorf("device rejected layer removal: %s", response)
	}
	return d.LoadConfiguration()
}

func (d *Device) SetHoldMS(value int) error {
	if value < minimumHoldMS || value > maximumHoldMS {
		return fmt.Errorf("hold threshold must be %d–%d ms", minimumHoldMS, maximumHoldMS)
	}
	response, err := d.sendCommand(fmt.Sprintf("SET_HOLD_MS:%d", value))
	if err != nil {
		return err
	}
	if response != "OK" {
		return fmt.Errorf("device rejected hold threshold: %s", response)
	}
	d.mu.Lock()
	d.layerConfig.HoldMS = value
	d.mu.Unlock()
	return nil
}

func (d *Device) ResetDefaults() error {
	protocol := d.ProtocolVersion()
	if protocol >= 1 {
		response, err := d.sendCommand("RESET_DEFAULTS")
		if err != nil {
			return err
		}
		if response != "OK" {
			return fmt.Errorf("device rejected reset: %s", response)
		}
		if protocol >= 2 {
			return d.LoadConfiguration()
		}
		d.mu.Lock()
		d.capabilities = DeviceCapabilities{Protocol: protocol, InputCount: numInputs, MaxKeys: 1}
		d.inputs = append([]InputDescriptor(nil), legacyInputs...)
		d.layerConfig = LayerConfig{HoldMS: defaultHoldMS}
		d.layouts = map[int][]Mapping{0: append([]Mapping(nil), defaultLayout...)}
		d.mu.Unlock()
		return nil
	}
	for i, mapping := range defaultLayout {
		if err := d.SetKey(i, mapping.Modifier, mapping.Keys[0]); err != nil {
			return fmt.Errorf("reset input %d: %w", i, err)
		}
	}
	return nil
}

func (d *Device) Snapshot() (DeviceCapabilities, []InputDescriptor, LayerConfig, map[int][]Mapping) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	inputs := append([]InputDescriptor(nil), d.inputs...)
	layers := LayerConfig{HoldMS: d.layerConfig.HoldMS, Layers: append([]LayerDefinition(nil), d.layerConfig.Layers...)}
	layouts := make(map[int][]Mapping, len(d.layouts))
	for layer, mappings := range d.layouts {
		layouts[layer] = append([]Mapping(nil), mappings...)
	}
	return d.capabilities, inputs, layers, layouts
}

func (d *Device) GetLayoutData() []Mapping {
	_, _, _, layouts := d.Snapshot()
	return layouts[0]
}
