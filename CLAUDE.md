# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

WirelessTourbox — a USB HID macro controller inspired by the TourBox, built on the Raspberry Pi Pico W. It provides 10 configurable inputs (4 switches, 2 rotary encoders with clicks) that send keyboard keycodes to the host OS. Key mappings are stored in EEPROM and can be remapped at runtime via a serial command protocol.

## Build Commands

### Firmware (PlatformIO)

`pio` is installed at `~/.platformio/penv/bin/pio` (not on PATH by default).

```bash
~/.platformio/penv/bin/pio run                    # Build the project
~/.platformio/penv/bin/pio run -t upload          # Build and upload to the Pico W
~/.platformio/penv/bin/pio device monitor         # Open serial monitor
~/.platformio/penv/bin/pio test                   # Run unit tests (native or on-device)
~/.platformio/penv/bin/pio run -t clean           # Clean build artifacts
```

### GUI Config Tool (Go)

Go is installed at `~/go-install/go/bin/go`. osxcross for macOS cross-compilation is at `~/osxcross/target/bin/`.

```bash
# Add to PATH (add to ~/.bashrc for persistence)
export PATH=$PATH:$HOME/go-install/go/bin
export PATH=$PATH:$HOME/osxcross/target/bin

cd gui/

# Linux
CGO_ENABLED=1 go build -o wireless-tourbox-config .

# Windows (no terminal window)
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc go build -ldflags "-H windowsgui" -o wireless-tourbox-config.exe .

# macOS Intel
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 CC=o64-clang CXX=o64-clang++ go build -o wireless-tourbox-config-mac .

# macOS Apple Silicon
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 CC=oa64-clang CXX=oa64-clang++ go build -o wireless-tourbox-config-mac-arm64 .
```

**Build dependencies (Linux):**
```bash
sudo apt install -y pkg-config gcc libx11-dev libxcursor-dev libxrandr-dev libxinerama-dev libxi-dev libgl-dev libxxf86vm-dev
sudo apt install -y gcc-mingw-w64-x86-64  # for Windows cross-compilation
```

## WSL2 / USB Flashing

This project runs in WSL2 where USB devices are not directly accessible. To upload firmware:

**Option A — USB passthrough with usbipd (Windows PowerShell as admin):**
```powershell
usbipd list                              # find the Pico W's BUSID
usbipd bind --busid <BUSID>              # one-time bind
usbipd attach --wsl --busid <BUSID>      # attach each session
```

**Option B — Manual UF2 flash:**
The build produces `.pio/build/pico_w/firmware.uf2`. Hold BOOTSEL on the Pico W while plugging in — it mounts as a USB drive. Copy the `.uf2` file to that drive.

**Important WSL2 note:** After flashing, the Pico W reboots into application mode, which drops the usbipd attachment. You must re-attach via `usbipd attach` after every upload. The 1200bps touch-reset does not work in WSL2 — manual BOOTSEL is required for each upload.

## Architecture

- `platformio.ini` — Board config (Pico W, Arduino framework, TinyUSB via `-DUSE_TINYUSB`)
- `src/main.cpp` — Full firmware: HID keyboard + CDC serial composite, EEPROM config, input handling
- `config_tool.py` — Python terminal UI for viewing and remapping key bindings
- `test_hardware.py` — Automated hardware test script (serial commands + physical inputs)
- `gui/` — Native GUI config tool (Go + Fyne)
  - `main.go` — Entry point, Fyne app setup
  - `device.go` — Serial communication with single-reader architecture
  - `keys.go` — HID keycode definitions and Fyne-to-HID mapping
  - `ui.go` — GUI components, key capture dialog, monitor

The platform uses a community fork (`maxgerhardt/platform-raspberrypi`) for Pico W Arduino support.

## Key Technical Details

- **USB Composite:** HID Keyboard + CDC Serial via Adafruit TinyUSB. `TinyUSBDevice.begin(0)` must be called before `Serial.begin()`. `#include <Adafruit_TinyUSB.h>` is required for `Serial` to link.
- **Encoder:** EC11 rotary encoder, full quadrature state machine (CHANGE on both pins), 4 transitions per detent. Lookup table signs are specific to this encoder's resting state (3 = A=1,B=1).
- **EEPROM:** 21 bytes — 1 magic byte (0xA5) + 10 inputs × 2 bytes (modifier + keycode).
- **Serial Protocol:** `GET_LAYOUT` returns `mod:key,mod:key,...`. `SET_KEY:[idx]:[mod]:[key]` updates a mapping.
- **Debounce:** Switches use 5ms debounce in polling. Encoder uses full quadrature with 4-steps-per-detent counting.
- **GUI Serial Architecture:** Single reader goroutine (`readLoop`) routes `KEY:` events to event channel and command responses to response channel. Prevents race conditions between monitor and commands.
- **Key Capture (Fyne):** Three methods handle different key types:
  - `TypedRune` — printable characters (a-z, 0-9, symbols)
  - `TypedKey` — special keys (F1-F24, arrows, etc.) + Shift+key (via `desktop.Driver.CurrentKeyModifiers()`)
  - `TypedShortcut` — Ctrl/Alt+key combinations (Fyne intercepts these as shortcuts before `TypedKey`)
  - Super key is not captured (Windows key triggers OS events first)
- **Fyne Built-in Shortcuts:** Fyne maps common Ctrl+key to built-in shortcuts (Ctrl+C→Copy, Ctrl+V→Paste, etc.). The `builtinShortcuts` map in `ui.go` reverse-maps these back to HID keycodes.

## Key Constraints

- C++ (Arduino framework); use `#include <Arduino.h>` in all source files
- TinyUSB is enabled (`-DUSE_TINYUSB`) — use TinyUSB APIs, not the default Pico SDK USB stack
- The Pico W has onboard WiFi (CYW43) — available via the `WiFi` library
- HID keycodes F13-F22 are sent via `keyboardReport()` (not `keyboardPress()`, which only supports ASCII)
- Windows GUI build requires `-ldflags "-H windowsgui"` to hide terminal window
- macOS cross-compilation requires osxcross with macOS SDK
