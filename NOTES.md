# WirelessTourbox — Design Spec

## Project Overview
A compact, highly tactile desktop macro controller inspired by the TourBox, optimized for one-handed creative workflows.

## Hardware Stack & Pin Mapping
- **Controller:** Raspberry Pi Pico W
- **Framework:** Arduino (Earle Philhower Core via PlatformIO)
- **Wiring Topology:** Direct-to-GPIO (no matrix, common GND, internal pull-ups required)

### Pin Configurations
- **Switch 1:** GPIO 2
- **Switch 2:** GPIO 3
- **Switch 3:** GPIO 4
- **Switch 4:** GPIO 5
- **Encoder 1 (Rotation):** Pin A -> GPIO 6, Pin B -> GPIO 7
- **Encoder 1 (Click Switch):** GPIO 8
- **Encoder 2 (Rotation):** Pin A -> GPIO 10, Pin B -> GPIO 11
- **Encoder 2 (Click Switch):** GPIO 12

## Memory & Configuration Architecture
- **Storage:** `EEPROM.h` (Flash emulation on Pico) stores a 21-byte configuration array.
- **EEPROM Layout:** Byte 0 = magic (0xA5), bytes 1-20 = 10 inputs × 2 bytes (modifier + keycode).
- **Boot Behavior:** On `setup()`, initialize EEPROM. Read the 21-byte map. If magic byte != 0xA5, write defaults and commit.
- **USB Architecture:** Composite Device providing both **USB HID Keyboard** and **USB Serial (CDC)** simultaneously via Adafruit TinyUSB.
- **Communication Protocol:** The firmware listens on the Serial port for configuration commands.

### Default Configuration Array Mappings
Each input is 2 bytes: [modifier, keycode]. Default mappings use no modifier (0x00).

| Index | Input | Modifier | Keycode | Key Name |
|-------|-------|----------|---------|----------|
| 0 | Switch 1 | 0x00 | 0x68 | F13 |
| 1 | Switch 2 | 0x00 | 0x69 | F14 |
| 2 | Switch 3 | 0x00 | 0x6A | F15 |
| 3 | Switch 4 | 0x00 | 0x6B | F16 |
| 4 | Encoder 1 Click | 0x00 | 0x6C | F17 |
| 5 | Encoder 2 Click | 0x00 | 0x6D | F18 |
| 6 | Encoder 1 CW | 0x00 | 0x6E | F19 |
| 7 | Encoder 1 CCW | 0x00 | 0x6F | F20 |
| 8 | Encoder 2 CW | 0x00 | 0x70 | F21 |
| 9 | Encoder 2 CCW | 0x00 | 0x71 | F22 |

### Modifier Bitmask
| Bit | Hex | Name |
|-----|-----|------|
| 0 | 0x01 | Left Ctrl |
| 1 | 0x02 | Left Shift |
| 2 | 0x04 | Left Alt |
| 3 | 0x08 | Left GUI (Win/Cmd) |
| 4 | 0x10 | Right Ctrl |
| 5 | 0x20 | Right Shift |
| 6 | 0x40 | Right Alt |
| 7 | 0x80 | Right GUI |

### Serial Command Spec
- `GET_LAYOUT` → Returns `mod:key,mod:key,...` (10 pairs, decimal, comma-separated).
- `SET_KEY:[index]:[modifier]:[keycode]` → Updates modifier and keycode at index, commits to EEPROM, responds `OK`.

### Debug Output
When a key is sent, the firmware echoes: `KEY:<index>:0x<modifier_hex>:0x<keycode_hex>`

## Encoder Details
- **Type:** EC11 rotary encoder with push button
- **Quadrature:** Full state machine (CHANGE on both A and B pins)
- **Transitions per detent:** 4 (each detent click produces 4 quadrature state changes)
- **Resting state:** 3 (A=1, B=1) — lookup table signs are calibrated for this
- **Debounce:** 5ms on switch/button polling; encoder uses step-counting (4 steps = 1 detent)

## Configuration Tools

### GUI Config Tool (Go + Fyne)
- Cross-platform native desktop application (Windows, macOS, Linux)
- Live key capture for remapping
- Duplicate key detection
- Real-time input monitor
- Single-reader serial architecture (prevents race conditions between monitor and commands)

### Key Capture Implementation (Fyne)
The key capture widget uses three methods to handle different key types:
- `TypedRune(r rune)` — printable characters (a-z, 0-9, symbols). Uppercase letters automatically get LShift modifier.
- `TypedKey(event *fyne.KeyEvent)` — special keys (F1-F24, arrows, Enter, Escape, etc.) and Shift+key combinations. Uses `desktop.Driver.CurrentKeyModifiers()` to detect Shift state.
- `TypedShortcut(shortcut fyne.Shortcut)` — Ctrl/Alt+key combinations. Fyne intercepts these as shortcuts before they reach TypedKey. Handles both built-in shortcuts (Ctrl+C→Copy, etc.) and custom shortcuts via `*desktop.CustomShortcut`.

Fyne's built-in shortcuts (Ctrl+C, Ctrl+V, Ctrl+Z, etc.) are reverse-mapped via the `builtinShortcuts` table in `ui.go`.

Super/Windows key is not supported — it triggers OS events before reaching the application.

### Terminal Config Tool (Python)
- `config_tool.py` — Terminal UI with key capture
- `test_hardware.py` — Automated hardware verification

## Tooling & Development Commands
- Build firmware: `~/.platformio/penv/bin/pio run`
- Upload firmware: `~/.platformio/penv/bin/pio run -t upload` (requires BOOTSEL mode)
- Clean build: `~/.platformio/penv/bin/pio run -t clean`
- Build GUI (Linux): `cd gui && CGO_ENABLED=1 go build -o wireless-tourbox-config .`
- Build GUI (Windows): `cd gui && CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc go build -ldflags "-H windowsgui" -o wireless-tourbox-config.exe .`
