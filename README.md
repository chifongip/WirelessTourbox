# WirelessTourbox

A compact USB macro controller inspired by the [TourBox](https://tourboxtech.com/), built on the Raspberry Pi Pico W. Designed for one-handed creative workflows in photo editing, video editing, and other creative applications.

## Features

- **10 configurable inputs:** 4 switches, 2 rotary encoders with push buttons
- **USB HID keyboard:** Plug-and-play, no drivers needed on Linux, macOS, or Windows
- **Persistent key mappings:** Stored in EEPROM, survives power cycles
- **Runtime remapping:** Change key bindings via GUI tool or serial commands
- **Key combinations:** Supports modifier keys (Ctrl, Shift, Alt, GUI) + any keycode
- **Default mappings:** F13–F22 (easily remappable to any HID keycode)
- **Native GUI:** Cross-platform config tool (Windows, macOS, Linux)

## Hardware

| Input | GPIO | Default Key |
|-------|------|-------------|
| Switch 1 | GPIO 2 | F13 |
| Switch 2 | GPIO 3 | F14 |
| Switch 3 | GPIO 4 | F15 |
| Switch 4 | GPIO 5 | F16 |
| Encoder 1 Click | GPIO 8 | F17 |
| Encoder 2 Click | GPIO 12 | F18 |
| Encoder 1 CW | GPIO 6/7 | F19 |
| Encoder 1 CCW | GPIO 6/7 | F20 |
| Encoder 2 CW | GPIO 10/11 | F21 |
| Encoder 2 CCW | GPIO 10/11 | F22 |

**Wiring:** All inputs are direct-to-GPIO with internal pull-ups. Switches and encoder buttons connect between the GPIO pin and GND. Encoders use standard EC11 wiring (A, B, C=GND).

## Getting Started

### Prerequisites

- [PlatformIO](https://platformio.org/) (CLI or VS Code extension)
- Python 3 with `pyserial` (`pip install pyserial`)
- Raspberry Pi Pico W

### Build and Flash Firmware

#### Step 1: Build the firmware

```bash
~/.platformio/penv/bin/pio run
```

This compiles the firmware and produces `.pio/build/pico_w/firmware.uf2`.

#### Step 2: Enter BOOTSEL mode

The Pico W must be in **BOOTSEL mode** to accept new firmware. There are two ways to enter BOOTSEL mode:

**Method A — BOOTSEL button (always works):**
1. **Unplug** the Pico W from USB
2. **Press and hold** the BOOTSEL button on the Pico W (small button near the USB port)
3. **While holding** the button, **plug in** the USB cable
4. **Release** the button after plugging in

The Pico W will appear as a USB drive named `RPI-RP2` on your computer. On Linux, it will also appear as a USB device (`Raspberry Pi RP2 Boot` in `lsusb`).

**Method B — Software reset (only works if firmware is already running):**
PlatformIO can automatically reset the Pico W into BOOTSEL mode via a 1200bps touch-reset on the serial port. This works on native Linux/macOS but **does not work in WSL2**.

#### Step 3: Upload the firmware

```bash
~/.platformio/penv/bin/pio run -t upload
```

PlatformIO will detect the Pico W in BOOTSEL mode and flash the firmware. After flashing, the Pico W automatically reboots into application mode.

#### WSL2 Users

WSL2 cannot directly access USB devices. You need [usbipd](https://github.com/dorssel/usbipd-win) to pass the USB device through to WSL.

**First-time setup (Windows PowerShell as admin):**
```powershell
usbipd install                           # Install the USBIP driver (one-time)
usbipd list                              # Find the Pico W's BUSID
usbipd bind --busid <BUSID>              # Bind the device (one-time per device)
```

**Every flash session:**
1. Put the Pico W in BOOTSEL mode (see Method A above)
2. In Windows PowerShell (admin): `usbipd attach --wsl --busid <BUSID>`
3. In WSL: `~/.platformio/penv/bin/pio run -t upload`
4. After flashing, the Pico W reboots and drops the usbipd attachment
5. Re-attach for config tool use: `usbipd attach --wsl --busid <BUSID>`

**Important:** After every firmware upload, the Pico W reboots into application mode with a different USB device ID, which causes usbipd to drop the attachment. You must re-attach via `usbipd attach` each time.

#### Manual UF2 Flash (Alternative)

If usbipd is not available, you can flash manually:
1. Build the firmware: `~/.platformio/penv/bin/pio run`
2. Put the Pico W in BOOTSEL mode (hold BOOTSEL while plugging in)
3. The Pico W mounts as a USB drive (`RPI-RP2`)
4. Copy `.pio/build/pico_w/firmware.uf2` to the drive
5. The Pico W automatically reboots and starts running the new firmware

## Configuration Tools

### GUI Config Tool (Recommended)

Native desktop application for viewing and remapping key bindings. Available for Windows, macOS, and Linux.

**Download:** Build from source (see below) or download pre-built binaries.

**Usage:**
```bash
./wireless-tourbox-config        # Linux
wireless-tourbox-config.exe      # Windows
./wireless-tourbox-config-mac    # macOS (Intel)
./wireless-tourbox-config-mac-arm64  # macOS (Apple Silicon)
```

**Features:**
- Connect to device via serial port dropdown
- View all 10 inputs with current key mappings
- Click "Change" to remap any input via live key capture
- Duplicate key detection
- Reset all keys to defaults (F13-F22)
- Live monitor showing input events in real-time

**Supported key combinations:**
- Single keys: A-Z, 0-9, F1-F24, symbols (`,./;'[]\-=`)
- Shift+key: Shift+A, Shift+F1, etc.
- Ctrl+key: Ctrl+C, Ctrl+Z, Ctrl+A, etc.
- Alt+key: Alt+A, Alt+F1, etc.
- Note: Windows/Super key is not supported (triggers OS events first)

**Build from source:**
```bash
cd gui/

# Linux
CGO_ENABLED=1 go build -o wireless-tourbox-config .

# Windows
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc go build -ldflags "-H windowsgui" -o wireless-tourbox-config.exe .

# macOS Intel
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 CC=o64-clang CXX=o64-clang++ go build -o wireless-tourbox-config-mac .

# macOS Apple Silicon
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 CC=oa64-clang CXX=oa64-clang++ go build -o wireless-tourbox-config-mac-arm64 .
```

**Environment setup:**
```bash
# Add Go and osxcross to PATH (add to ~/.bashrc for persistence)
export PATH=$PATH:$HOME/go-install/go/bin
export PATH=$PATH:$HOME/osxcross/target/bin
```

**Build dependencies (Linux):**
```bash
sudo apt install -y pkg-config gcc libx11-dev libxcursor-dev libxrandr-dev libxinerama-dev libxi-dev libgl-dev libxxf86vm-dev
sudo apt install -y gcc-mingw-w64-x86-64  # for Windows cross-compilation
```

### Terminal Config Tool

Python-based terminal UI for viewing and remapping key bindings:

```bash
python3 config_tool.py
```

```
=== WirelessTourbox Config Tool ===

1. View current layout
2. Remap a key
3. Reset to defaults
4. Monitor inputs (live)
5. Quit
```

### Hardware Test

Run the automated test to verify all inputs:

```bash
python3 test_hardware.py
```

This tests serial commands (GET_LAYOUT, SET_KEY) and physical inputs (switches, encoder rotation, encoder buttons).

## Serial Protocol

The device exposes a CDC serial interface for runtime configuration:

| Command | Response | Description |
|---------|----------|-------------|
| `GET_LAYOUT` | `mod:key,mod:key,...` | Returns current 10 key mappings (decimal) |
| `SET_KEY:[idx]:[mod]:[key]` | `OK` or `ERR` | Updates key at index, persists to EEPROM |

**Example:**
```
> GET_LAYOUT
0:104,0:105,0:106,0:107,0:108,0:109,0:110,0:111,0:112,0:113

> SET_KEY:0:1:26     # Remap Switch 1 to Ctrl+Z
OK
```

## Using the Tourbox

The device appears as a standard USB keyboard. To use it:

1. **Plug in** the Pico W
2. **Configure your application** to respond to F13–F22 keypresses
3. **Or remap** the keys to application shortcuts using the config tool

**Application examples:**

| Application | Switch 1 | Encoder CW | Encoder CCW |
|-------------|----------|------------|-------------|
| Photoshop | Undo (Ctrl+Z) | Zoom In | Zoom Out |
| Premiere | Play/Pause | Frame Forward | Frame Back |
| DaVinci Resolve | Cut | Navigate Timeline | Navigate Timeline |
| General | Copy (Ctrl+C) | Volume Up | Volume Down |

Most creative apps let you bind custom keyboard shortcuts — just press the Tourbox input in the shortcut config dialog.

## Project Structure

```
WirelessTourbox/
├── src/
│   └── main.cpp          # Firmware (HID + CDC + EEPROM + inputs)
├── gui/
│   ├── main.go           # GUI entry point (Go + Fyne)
│   ├── device.go         # Serial communication
│   ├── keys.go           # HID keycode definitions
│   ├── ui.go             # GUI components
│   ├── go.mod
│   └── go.sum
├── config_tool.py        # Python terminal config tool
├── test_hardware.py      # Hardware test script
├── platformio.ini        # Build configuration
├── CLAUDE.md             # Developer guide
├── NOTES.md              # Design spec
└── README.md             # This file
```

## License

MIT
