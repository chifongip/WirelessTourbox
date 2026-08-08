package main

import (
	"fmt"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
)

// HID Keycodes (USB HID Usage Tables, Keyboard/Keypad page 0x07)
const (
	HID_KEY_A          = 0x04
	HID_KEY_B          = 0x05
	HID_KEY_C          = 0x06
	HID_KEY_D          = 0x07
	HID_KEY_E          = 0x08
	HID_KEY_F          = 0x09
	HID_KEY_G          = 0x0A
	HID_KEY_H          = 0x0B
	HID_KEY_I          = 0x0C
	HID_KEY_J          = 0x0D
	HID_KEY_K          = 0x0E
	HID_KEY_L          = 0x0F
	HID_KEY_M          = 0x10
	HID_KEY_N          = 0x11
	HID_KEY_O          = 0x12
	HID_KEY_P          = 0x13
	HID_KEY_Q          = 0x14
	HID_KEY_R          = 0x15
	HID_KEY_S          = 0x16
	HID_KEY_T          = 0x17
	HID_KEY_U          = 0x18
	HID_KEY_V          = 0x19
	HID_KEY_W          = 0x1A
	HID_KEY_X          = 0x1B
	HID_KEY_Y          = 0x1C
	HID_KEY_Z          = 0x1D
	HID_KEY_1          = 0x1E
	HID_KEY_2          = 0x1F
	HID_KEY_3          = 0x20
	HID_KEY_4          = 0x21
	HID_KEY_5          = 0x22
	HID_KEY_6          = 0x23
	HID_KEY_7          = 0x24
	HID_KEY_8          = 0x25
	HID_KEY_9          = 0x26
	HID_KEY_0          = 0x27
	HID_KEY_ENTER      = 0x28
	HID_KEY_ESCAPE     = 0x29
	HID_KEY_BACKSPACE  = 0x2A
	HID_KEY_TAB        = 0x2B
	HID_KEY_SPACE      = 0x2C
	HID_KEY_MINUS      = 0x2D
	HID_KEY_EQUAL      = 0x2E
	HID_KEY_LEFTBRACE  = 0x2F
	HID_KEY_RIGHTBRACE = 0x30
	HID_KEY_BACKSLASH  = 0x31
	HID_KEY_SEMICOLON  = 0x33
	HID_KEY_APOSTROPHE = 0x34
	HID_KEY_GRAVE      = 0x35
	HID_KEY_COMMA      = 0x36
	HID_KEY_DOT        = 0x37
	HID_KEY_SLASH      = 0x38
	HID_KEY_CAPSLOCK   = 0x39
	HID_KEY_F1         = 0x3A
	HID_KEY_F2         = 0x3B
	HID_KEY_F3         = 0x3C
	HID_KEY_F4         = 0x3D
	HID_KEY_F5         = 0x3E
	HID_KEY_F6         = 0x3F
	HID_KEY_F7         = 0x40
	HID_KEY_F8         = 0x41
	HID_KEY_F9         = 0x42
	HID_KEY_F10        = 0x43
	HID_KEY_F11        = 0x44
	HID_KEY_F12        = 0x45
	HID_KEY_PRINT      = 0x46
	HID_KEY_SCROLLLOCK = 0x47
	HID_KEY_PAUSE      = 0x48
	HID_KEY_INSERT     = 0x49
	HID_KEY_HOME       = 0x4A
	HID_KEY_PAGEUP     = 0x4B
	HID_KEY_DELETE     = 0x4C
	HID_KEY_END        = 0x4D
	HID_KEY_PAGEDOWN   = 0x4E
	HID_KEY_RIGHT      = 0x4F
	HID_KEY_LEFT       = 0x50
	HID_KEY_DOWN       = 0x51
	HID_KEY_UP         = 0x52
	HID_KEY_NUMLOCK    = 0x53
	HID_KEY_KPSLASH    = 0x54
	HID_KEY_KPASTERISK = 0x55
	HID_KEY_KPMINUS    = 0x56
	HID_KEY_KPPLUS     = 0x57
	HID_KEY_KPENTER    = 0x58
	HID_KEY_KP1        = 0x59
	HID_KEY_KP2        = 0x5A
	HID_KEY_KP3        = 0x5B
	HID_KEY_KP4        = 0x5C
	HID_KEY_KP5        = 0x5D
	HID_KEY_KP6        = 0x5E
	HID_KEY_KP7        = 0x5F
	HID_KEY_KP8        = 0x60
	HID_KEY_KP9        = 0x61
	HID_KEY_KP0        = 0x62
	HID_KEY_KPDOT      = 0x63
	HID_KEY_F13        = 0x68
	HID_KEY_F14        = 0x69
	HID_KEY_F15        = 0x6A
	HID_KEY_F16        = 0x6B
	HID_KEY_F17        = 0x6C
	HID_KEY_F18        = 0x6D
	HID_KEY_F19        = 0x6E
	HID_KEY_F20        = 0x6F
	HID_KEY_F21        = 0x70
	HID_KEY_F22        = 0x71
	HID_KEY_F23        = 0x72
	HID_KEY_F24        = 0x73
)

// Modifier bitmask constants
const (
	MOD_LCTRL  = 0x01
	MOD_LSHIFT = 0x02
	MOD_LALT   = 0x04
	MOD_LGUI   = 0x08
	MOD_RCTRL  = 0x10
	MOD_RSHIFT = 0x20
	MOD_RALT   = 0x40
	MOD_RGUI   = 0x80
)

// HIDNames maps HID keycodes to human-readable names.
var HIDNames = map[uint8]string{
	0x00: "No Action",
	0x04: "A", 0x05: "B", 0x06: "C", 0x07: "D", 0x08: "E",
	0x09: "F", 0x0A: "G", 0x0B: "H", 0x0C: "I", 0x0D: "J",
	0x0E: "K", 0x0F: "L", 0x10: "M", 0x11: "N", 0x12: "O",
	0x13: "P", 0x14: "Q", 0x15: "R", 0x16: "S", 0x17: "T",
	0x18: "U", 0x19: "V", 0x1A: "W", 0x1B: "X", 0x1C: "Y",
	0x1D: "Z",
	0x1E: "1", 0x1F: "2", 0x20: "3", 0x21: "4", 0x22: "5",
	0x23: "6", 0x24: "7", 0x25: "8", 0x26: "9", 0x27: "0",
	0x28: "Enter", 0x29: "Escape", 0x2A: "Backspace", 0x2B: "Tab",
	0x2C: "Space", 0x2D: "-", 0x2E: "=", 0x2F: "[", 0x30: "]",
	0x31: "\\", 0x33: ";", 0x34: "'", 0x35: "`", 0x36: ",",
	0x37: ".", 0x38: "/",
	0x39: "CapsLock",
	0x3A: "F1", 0x3B: "F2", 0x3C: "F3", 0x3D: "F4", 0x3E: "F5",
	0x3F: "F6", 0x40: "F7", 0x41: "F8", 0x42: "F9", 0x43: "F10",
	0x44: "F11", 0x45: "F12",
	0x46: "PrintScreen", 0x47: "ScrollLock", 0x48: "Pause",
	0x49: "Insert", 0x4A: "Home", 0x4B: "PageUp",
	0x4C: "Delete", 0x4D: "End", 0x4E: "PageDown",
	0x4F: "Right", 0x50: "Left", 0x51: "Down", 0x52: "Up",
	0x53: "NumLock",
	0x54: "Keypad /", 0x55: "Keypad *", 0x56: "Keypad -",
	0x57: "Keypad +", 0x58: "Keypad Enter", 0x59: "Keypad 1",
	0x5A: "Keypad 2", 0x5B: "Keypad 3", 0x5C: "Keypad 4",
	0x5D: "Keypad 5", 0x5E: "Keypad 6", 0x5F: "Keypad 7",
	0x60: "Keypad 8", 0x61: "Keypad 9", 0x62: "Keypad 0",
	0x63: "Keypad .",
	0x68: "F13", 0x69: "F14", 0x6A: "F15", 0x6B: "F16",
	0x6C: "F17", 0x6D: "F18", 0x6E: "F19", 0x6F: "F20",
	0x70: "F21", 0x71: "F22", 0x72: "F23", 0x73: "F24",
}

// ModifierNames maps modifier bitmask values to names.
var ModifierNames = []struct {
	Bit  uint8
	Name string
}{
	{MOD_LCTRL, "LCtrl"}, {MOD_LSHIFT, "LShift"},
	{MOD_LALT, "LAlt"}, {MOD_LGUI, "LGui"},
	{MOD_RCTRL, "RCtrl"}, {MOD_RSHIFT, "RShift"},
	{MOD_RALT, "RAlt"}, {MOD_RGUI, "RGui"},
}

// FyneToHID maps Fyne key names to HID keycodes.
var FyneToHID = map[fyne.KeyName]uint8{
	fyne.KeyA: HID_KEY_A, fyne.KeyB: HID_KEY_B, fyne.KeyC: HID_KEY_C,
	fyne.KeyD: HID_KEY_D, fyne.KeyE: HID_KEY_E, fyne.KeyF: HID_KEY_F,
	fyne.KeyG: HID_KEY_G, fyne.KeyH: HID_KEY_H, fyne.KeyI: HID_KEY_I,
	fyne.KeyJ: HID_KEY_J, fyne.KeyK: HID_KEY_K, fyne.KeyL: HID_KEY_L,
	fyne.KeyM: HID_KEY_M, fyne.KeyN: HID_KEY_N, fyne.KeyO: HID_KEY_O,
	fyne.KeyP: HID_KEY_P, fyne.KeyQ: HID_KEY_Q, fyne.KeyR: HID_KEY_R,
	fyne.KeyS: HID_KEY_S, fyne.KeyT: HID_KEY_T, fyne.KeyU: HID_KEY_U,
	fyne.KeyV: HID_KEY_V, fyne.KeyW: HID_KEY_W, fyne.KeyX: HID_KEY_X,
	fyne.KeyY: HID_KEY_Y, fyne.KeyZ: HID_KEY_Z,
	fyne.Key0: HID_KEY_0, fyne.Key1: HID_KEY_1, fyne.Key2: HID_KEY_2,
	fyne.Key3: HID_KEY_3, fyne.Key4: HID_KEY_4, fyne.Key5: HID_KEY_5,
	fyne.Key6: HID_KEY_6, fyne.Key7: HID_KEY_7, fyne.Key8: HID_KEY_8,
	fyne.Key9:  HID_KEY_9,
	fyne.KeyF1: HID_KEY_F1, fyne.KeyF2: HID_KEY_F2,
	fyne.KeyF3: HID_KEY_F3, fyne.KeyF4: HID_KEY_F4,
	fyne.KeyF5: HID_KEY_F5, fyne.KeyF6: HID_KEY_F6,
	fyne.KeyF7: HID_KEY_F7, fyne.KeyF8: HID_KEY_F8,
	fyne.KeyF9: HID_KEY_F9, fyne.KeyF10: HID_KEY_F10,
	fyne.KeyF11: HID_KEY_F11, fyne.KeyF12: HID_KEY_F12,
	fyne.KeyUp:        HID_KEY_UP,
	fyne.KeyDown:      HID_KEY_DOWN,
	fyne.KeyLeft:      HID_KEY_LEFT,
	fyne.KeyRight:     HID_KEY_RIGHT,
	fyne.KeyReturn:    HID_KEY_ENTER,
	fyne.KeyEscape:    HID_KEY_ESCAPE,
	fyne.KeyBackspace: HID_KEY_BACKSPACE,
	fyne.KeyTab:       HID_KEY_TAB,
	fyne.KeySpace:     HID_KEY_SPACE,
	fyne.KeyDelete:    HID_KEY_DELETE,
	fyne.KeyHome:      HID_KEY_HOME,
	fyne.KeyEnd:       HID_KEY_END,
	fyne.KeyPageUp:    HID_KEY_PAGEUP,
	fyne.KeyPageDown:  HID_KEY_PAGEDOWN,
	fyne.KeyInsert:    HID_KEY_INSERT,
	// Symbol keys
	fyne.KeyComma:        HID_KEY_COMMA,
	fyne.KeyPeriod:       HID_KEY_DOT,
	fyne.KeySlash:        HID_KEY_SLASH,
	fyne.KeySemicolon:    HID_KEY_SEMICOLON,
	fyne.KeyApostrophe:   HID_KEY_APOSTROPHE,
	fyne.KeyLeftBracket:  HID_KEY_LEFTBRACE,
	fyne.KeyRightBracket: HID_KEY_RIGHTBRACE,
	fyne.KeyBackslash:    HID_KEY_BACKSLASH,
	fyne.KeyMinus:        HID_KEY_MINUS,
	fyne.KeyEqual:        HID_KEY_EQUAL,
	fyne.KeyBackTick:     HID_KEY_GRAVE,
}

// RuneToHID maps printable characters to HID keycodes.
var RuneToHID = map[rune]uint8{
	'a': HID_KEY_A, 'b': HID_KEY_B, 'c': HID_KEY_C,
	'd': HID_KEY_D, 'e': HID_KEY_E, 'f': HID_KEY_F,
	'g': HID_KEY_G, 'h': HID_KEY_H, 'i': HID_KEY_I,
	'j': HID_KEY_J, 'k': HID_KEY_K, 'l': HID_KEY_L,
	'm': HID_KEY_M, 'n': HID_KEY_N, 'o': HID_KEY_O,
	'p': HID_KEY_P, 'q': HID_KEY_Q, 'r': HID_KEY_R,
	's': HID_KEY_S, 't': HID_KEY_T, 'u': HID_KEY_U,
	'v': HID_KEY_V, 'w': HID_KEY_W, 'x': HID_KEY_X,
	'y': HID_KEY_Y, 'z': HID_KEY_Z,
	'1': HID_KEY_1, '2': HID_KEY_2, '3': HID_KEY_3,
	'4': HID_KEY_4, '5': HID_KEY_5, '6': HID_KEY_6,
	'7': HID_KEY_7, '8': HID_KEY_8, '9': HID_KEY_9,
	'0': HID_KEY_0,
	'-': HID_KEY_MINUS, '=': HID_KEY_EQUAL,
	'[': HID_KEY_LEFTBRACE, ']': HID_KEY_RIGHTBRACE,
	'\\': HID_KEY_BACKSLASH, ';': HID_KEY_SEMICOLON,
	'\'': HID_KEY_APOSTROPHE, '`': HID_KEY_GRAVE,
	',': HID_KEY_COMMA, '.': HID_KEY_DOT, '/': HID_KEY_SLASH,
	' ': HID_KEY_SPACE,
}

var ShiftedRuneToHID = map[rune]uint8{
	'!': HID_KEY_1, '@': HID_KEY_2, '#': HID_KEY_3, '$': HID_KEY_4,
	'%': HID_KEY_5, '^': HID_KEY_6, '&': HID_KEY_7, '*': HID_KEY_8,
	'(': HID_KEY_9, ')': HID_KEY_0, '_': HID_KEY_MINUS, '+': HID_KEY_EQUAL,
	'{': HID_KEY_LEFTBRACE, '}': HID_KEY_RIGHTBRACE, '|': HID_KEY_BACKSLASH,
	':': HID_KEY_SEMICOLON, '"': HID_KEY_APOSTROPHE, '~': HID_KEY_GRAVE,
	'<': HID_KEY_COMMA, '>': HID_KEY_DOT, '?': HID_KEY_SLASH,
}

// FormatKey returns a human-readable string for a modifier+keycode combination.
func FormatKey(mod, keycode uint8) string {
	if keycode == 0 {
		return "No Action"
	}
	name, ok := HIDNames[keycode]
	if !ok {
		name = fmt.Sprintf("0x%02X", keycode)
	}
	if mod == 0 {
		return name
	}
	return ModifierString(mod) + "+" + name
}

// ModifierString returns a human-readable string for a modifier bitmask.
func ModifierString(mod uint8) string {
	if mod == 0 {
		return "None"
	}
	parts := []string{}
	for _, modifier := range ModifierNames {
		if mod&modifier.Bit != 0 {
			parts = append(parts, modifier.Name)
		}
	}
	return strings.Join(parts, "+")
}

var KeyCategories = []string{"Actions", "Letters", "Numbers", "Symbols", "Editing", "Navigation", "Function", "Keypad"}

// KeyChoices returns stable, keycode-ordered names for a picker category.
func KeyChoices(category string) []string {
	type choice struct {
		code uint8
		name string
	}
	choices := []choice{}
	for code, name := range HIDNames {
		if KeyCategory(code) == category {
			choices = append(choices, choice{code: code, name: name})
		}
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].code < choices[j].code })
	names := make([]string, len(choices))
	for i, choice := range choices {
		names[i] = choice.name
	}
	return names
}

func KeyCategory(code uint8) string {
	switch {
	case code == 0:
		return "Actions"
	case code >= HID_KEY_A && code <= HID_KEY_Z:
		return "Letters"
	case code >= HID_KEY_1 && code <= HID_KEY_0:
		return "Numbers"
	case code >= HID_KEY_MINUS && code <= HID_KEY_SLASH:
		return "Symbols"
	case code >= HID_KEY_F1 && code <= HID_KEY_F12, code >= HID_KEY_F13 && code <= HID_KEY_F24:
		return "Function"
	case code >= HID_KEY_KPSLASH && code <= HID_KEY_KPDOT:
		return "Keypad"
	case code >= HID_KEY_INSERT && code <= HID_KEY_NUMLOCK:
		return "Navigation"
	default:
		return "Editing"
	}
}

func KeyCodeByName(name string) (uint8, bool) {
	for code, candidate := range HIDNames {
		if candidate == name {
			return code, true
		}
	}
	return 0, false
}

// HIDKeyName returns the name for a HID keycode, or hex string if unknown.
func HIDKeyName(keycode uint8) string {
	if name, ok := HIDNames[keycode]; ok {
		return name
	}
	return fmt.Sprintf("0x%02X", keycode)
}
