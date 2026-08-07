# Repository Guidelines

## Project Structure & Module Organization

Firmware lives in `src/main.cpp` and is configured by `platformio.ini` for the Pico W, Arduino, and TinyUSB. The Go/Fyne configurator is under `gui/`; keep serial transport in `device.go`, HID mappings in `keys.go`, and interface code in `ui.go`. `config_tool.py` provides a terminal configurator, while `test_hardware.py` exercises serial and physical controls. PlatformIO tests belong in `test/`; shared headers and libraries belong in `include/` and `lib/`. Generated `.pio/` contents and GUI binaries are build artifacts.

## Build, Test, and Development Commands

- `~/.platformio/penv/bin/pio run` — compile firmware and create `.pio/build/pico_w/firmware.uf2`.
- `~/.platformio/penv/bin/pio run -t upload` — flash a Pico W in BOOTSEL mode.
- `~/.platformio/penv/bin/pio device monitor` — inspect CDC serial output.
- `~/.platformio/penv/bin/pio test -e native` — run host-side firmware tests beneath `test/`.
- `cd gui && CGO_ENABLED=1 go build -o wireless-tourbox-config .` — build the Linux GUI; Fyne/X11 libraries are required.
- `cd gui && go test ./...` — compile and run all Go tests.
- `python3 config_tool.py` — launch the terminal configurator.
- `python3 test_hardware.py [port]` — run interactive device checks; the default port is `/dev/ttyACM0`.

## Coding Style & Naming Conventions

Format Go changes with `gofmt`; use standard Go names (`CamelCase` exports, `camelCase` internals). Follow existing four-space indentation and `snake_case` functions in Python. For Arduino C++, use four spaces, `camelCase` functions, and uppercase constants. Preserve the firmware's input-index order and the serial formats `GET_LAYOUT`, `SET_KEY:idx:mod:key`, and `KEY:` when changing code across components.

## Testing Guidelines

Add Go tests as `*_test.go` beside the code they cover and PlatformIO tests under `test/test_<feature>/`. Run firmware and Go checks before submitting. Hardware-facing changes must also be verified on a Pico W with `test_hardware.py`; report the board, port, and any steps that could not be exercised. No numeric coverage target is currently defined.

## Commit & Pull Request Guidelines

History currently contains only `Initial commit: WirelessTourbox firmware + GUI config tool`, so no established convention exists. Use concise, imperative, scoped subjects such as `gui: prevent serial reader races`. Pull requests should explain behavior and hardware impact, list verification commands, link relevant issues, and include screenshots for visible GUI changes. Call out EEPROM-layout or serial-protocol compatibility changes explicitly.

## Hardware & Configuration Safety

Do not commit machine-specific serial ports, USB bus IDs, binaries, or toolchain paths. Uploading may require reattaching USB passthrough after reboot, especially under WSL2.
