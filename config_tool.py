#!/usr/bin/env python3
"""
WirelessTourbox Config Tool
Terminal-based UI for viewing and remapping key bindings.

Usage: python3 config_tool.py [port]
"""

import serial
import sys
import time
import termios
import tty

from serial_protocol import send_command

PORT = sys.argv[1] if len(sys.argv) > 1 else "/dev/ttyACM0"
BAUD = 115200

# --- Input names ---
INPUT_NAMES = [
    "Switch 1 (GPIO2)",
    "Switch 2 (GPIO3)",
    "Switch 3 (GPIO4)",
    "Switch 4 (GPIO5)",
    "Enc 1 Click (GPIO8)",
    "Enc 2 Click (GPIO12)",
    "Encoder 1 CW",
    "Encoder 1 CCW",
    "Encoder 2 CW",
    "Encoder 2 CCW",
]

# --- HID keycode lookup ---
HID_KEYCODES = {
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
    0x54: "KP /", 0x55: "KP *", 0x56: "KP -", 0x57: "KP +",
    0x58: "KP Enter", 0x59: "KP 1", 0x5A: "KP 2", 0x5B: "KP 3",
    0x5C: "KP 4", 0x5D: "KP 5", 0x5E: "KP 6", 0x5F: "KP 7",
    0x60: "KP 8", 0x61: "KP 9", 0x62: "KP 0", 0x63: "KP .",
    0x65: "App",
    0x68: "F13", 0x69: "F14", 0x6A: "F15", 0x6B: "F16",
    0x6C: "F17", 0x6D: "F18", 0x6E: "F19", 0x6F: "F20",
    0x70: "F21", 0x71: "F22", 0x72: "F23", 0x73: "F24",
}

# Reverse map: name -> keycode
NAME_TO_HID = {}
for code, name in HID_KEYCODES.items():
    NAME_TO_HID[name.lower()] = code

MODIFIER_NAMES = {
    0x01: "LCtrl", 0x02: "LShift", 0x04: "LAlt", 0x08: "LGui",
    0x10: "RCtrl", 0x20: "RShift", 0x40: "RAlt", 0x80: "RGui",
}

DEFAULT_MODS = [0x00] * 10
DEFAULT_KEYS = [0x68, 0x69, 0x6A, 0x6B, 0x6C, 0x6D, 0x6E, 0x6F, 0x70, 0x71]


def modifier_str(mod):
    """Convert modifier bitmask to string."""
    if mod == 0:
        return "None"
    parts = []
    for bit, name in MODIFIER_NAMES.items():
        if mod & bit:
            parts.append(name)
    return "+".join(parts)


def key_name(code):
    """Convert HID keycode to human-readable name."""
    return HID_KEYCODES.get(code, f"0x{code:02X}")


def format_mapping(mod, key):
    """Format a modifier+keycode as a readable string."""
    if mod == 0:
        return key_name(key)
    return f"{modifier_str(mod)}+{key_name(key)}"


# --- Serial communication ---

def send_cmd(ser, cmd):
    """Send a command and return its matching response line."""
    return send_command(ser, cmd)


def get_layout(ser):
    """Get current key layout from device."""
    response = send_cmd(ser, "GET_LAYOUT")
    if not response or response.startswith("ERR"):
        return None
    pairs = response.split(",")
    layout = []
    for pair in pairs:
        parts = pair.split(":")
        if len(parts) == 2:
            layout.append((int(parts[0]), int(parts[1])))
    return layout


def set_key(ser, index, mod, keycode):
    """Set a key mapping on the device."""
    response = send_cmd(ser, f"SET_KEY:{index}:{mod}:{keycode}")
    return response == "OK"


# --- Key capture ---

def capture_key():
    """Capture a keypress from terminal and return (modifier, keycode)."""
    fd = sys.stdin.fileno()
    old_settings = termios.tcgetattr(fd)
    try:
        tty.setraw(fd)
        modifier = 0
        # Read first byte
        ch = sys.stdin.read(1)
        byte = ord(ch)

        # Check for ESC sequence (potential Alt or special key)
        if byte == 0x1B:  # ESC
            # Check if more bytes follow (escape sequence)
            import select
            if select.select([sys.stdin], [], [], 0.05)[0]:
                seq = sys.stdin.read(1)
                if seq == "[":
                    # CSI sequence
                    seq2 = sys.stdin.read(1)
                    if seq2 == "A": return (0, 0x52)  # Up
                    if seq2 == "B": return (0, 0x51)  # Down
                    if seq2 == "C": return (0, 0x4F)  # Right
                    if seq2 == "D": return (0, 0x50)  # Left
                    if seq2 == "H": return (0, 0x4A)  # Home
                    if seq2 == "F": return (0, 0x4D)  # End
                    # Multi-char CSI
                    buf = seq2
                    while True:
                        c = sys.stdin.read(1)
                        buf += c
                        if c == "~":
                            break
                    csi_map = {
                        "2~": 0x49,   # Insert
                        "3~": 0x4C,   # Delete
                        "5~": 0x4B,   # PageUp
                        "6~": 0x4E,   # PageDown
                        "11~": 0x3A,  # F1
                        "12~": 0x3B,  # F2
                        "13~": 0x3C,  # F3
                        "14~": 0x3D,  # F4
                        "15~": 0x3E,  # F5
                        "17~": 0x3F,  # F6
                        "18~": 0x40,  # F7
                        "19~": 0x41,  # F8
                        "20~": 0x42,  # F9
                        "21~": 0x43,  # F10
                        "23~": 0x44,  # F11
                        "24~": 0x45,  # F12
                    }
                    code = csi_map.get(buf)
                    if code:
                        return (0, code)
                    return None
                elif seq == "O":
                    # SS3 sequence (F1-F4 in some terminals)
                    c = sys.stdin.read(1)
                    ss3_map = {"P": 0x3A, "Q": 0x3B, "R": 0x3C, "S": 0x3D}
                    code = ss3_map.get(c)
                    if code:
                        return (0, code)
                    return None
                else:
                    # Alt + key
                    modifier = 0x04  # Left Alt
                    byte = ord(seq)
                    ch = seq

        # Ctrl detection: 0x01-0x1A = Ctrl+A through Ctrl+Z
        if 0x01 <= byte <= 0x1A:
            modifier = 0x01  # Left Ctrl
            keycode = 0x04 + (byte - 0x01)  # A=0x04
            return (modifier, keycode)

        # Shift detection for letters
        if ch.isalpha() and ch.isupper():
            modifier = 0x02  # Left Shift
            keycode = 0x04 + (ord(ch.lower()) - ord('a'))
            return (modifier, keycode)

        # Regular letters
        if ch.isalpha():
            keycode = 0x04 + (ord(ch.lower()) - ord('a'))
            return (0, keycode)

        # Numbers
        if ch.isdigit():
            if ch == '0':
                return (0, 0x27)
            return (0, 0x1E + (int(ch) - 1))

        # Special characters
        char_map = {
            ' ': 0x2C, '\r': 0x28, '\n': 0x28, '\t': 0x2B,
            '\x7f': 0x2A,  # Backspace
            '-': 0x2D, '=': 0x2E, '[': 0x2F, ']': 0x30,
            '\\': 0x31, ';': 0x33, "'": 0x34, '`': 0x35,
            ',': 0x36, '.': 0x37, '/': 0x38,
        }
        code = char_map.get(ch)
        if code:
            return (0, code)

        return None
    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, old_settings)


# --- UI Functions ---

def view_layout(ser):
    """Display current key layout."""
    layout = get_layout(ser)
    if not layout:
        print("Error: Could not read layout from device.")
        return

    print()
    print(f"{'#':>2}  {'Input':<25} {'Modifier':<12} {'Keycode':<8} {'Key Name'}")
    print("-" * 70)
    for i, (mod, key) in enumerate(layout):
        print(f"{i:>2}  {INPUT_NAMES[i]:<25} {modifier_str(mod):<12} "
              f"0x{key:02X}     {key_name(key)}")
    print()


def remap_key(ser):
    """Remap a single key."""
    # Show current layout
    layout = get_layout(ser)
    if not layout:
        print("Error: Could not read layout from device.")
        return

    print()
    print("Select input to remap:")
    for i, (mod, key) in enumerate(layout):
        current = format_mapping(mod, key)
        print(f"  {i}: {INPUT_NAMES[i]:<25} [currently: {current}]")
    print()

    try:
        choice = int(input("Choice (0-9): "))
        if choice < 0 or choice > 9:
            print("Invalid choice.")
            return
    except ValueError:
        print("Invalid input.")
        return

    print()
    print("Press the desired key (or key combination)...")
    print("(Ctrl+C to cancel)")
    print()

    result = capture_key()
    if result is None:
        print("Could not recognize the key. Try again.")
        return

    mod, keycode = result
    key_str = format_mapping(mod, keycode)

    # Check for duplicate key
    for i, (m, k) in enumerate(layout):
        if i != choice and m == mod and k == keycode:
            print(f"Error: {key_str} is already mapped to {INPUT_NAMES[i]} (index {i}).")
            print("Duplicate keys are not allowed.")
            return

    print()
    print(f"Captured: {key_str}")
    print(f"  Modifier: 0x{mod:02X} ({modifier_str(mod)})")
    print(f"  Keycode:  0x{keycode:02X} ({key_name(keycode)})")
    print()

    confirm = input(f"Map {INPUT_NAMES[choice]} to {key_str}? (y/n): ")
    if confirm.lower() == 'y':
        if set_key(ser, choice, mod, keycode):
            print("OK — key mapped successfully.")
        else:
            print("Error: device rejected the mapping.")
    else:
        print("Cancelled.")


def reset_defaults(ser):
    """Reset all keys to defaults."""
    confirm = input("Reset all keys to defaults (F13-F22)? (y/n): ")
    if confirm.lower() != 'y':
        print("Cancelled.")
        return

    print("Sending defaults...", end=" ", flush=True)
    ok = True
    for i in range(10):
        if not set_key(ser, i, DEFAULT_MODS[i], DEFAULT_KEYS[i]):
            ok = False
            break
    if ok:
        print("OK")
    else:
        print("Error")


def monitor_inputs(ser):
    """Monitor live input events."""
    print()
    print("Monitoring inputs... Press Ctrl+C to stop.")
    print("-" * 50)

    # Flush buffer
    ser.reset_input_buffer()

    try:
        while True:
            line = ser.readline().decode("utf-8", errors="replace").strip()
            if line and line.startswith("KEY:"):
                # Parse KEY:index:mod:keycode
                parts = line.split(":")
                if len(parts) == 4:
                    idx = int(parts[1])
                    mod = int(parts[2], 16)
                    key = int(parts[3], 16)
                    name = INPUT_NAMES[idx] if idx < 10 else f"Input {idx}"
                    print(f"  [{name}] {format_mapping(mod, key)}")
            elif line:
                print(f"  {line}")
    except KeyboardInterrupt:
        print("\nStopped.")


def main():
    print(f"Connecting to {PORT}...")
    try:
        ser = serial.Serial(PORT, BAUD, timeout=0.5)
    except serial.SerialException as e:
        print(f"Error: {e}")
        return 1

    # Read startup message
    time.sleep(0.5)
    while ser.in_waiting:
        line = ser.readline().decode("utf-8", errors="replace").strip()
        if line:
            print(f"  Device: {line}")

    while True:
        print()
        print("=== WirelessTourbox Config Tool ===")
        print()
        print("1. View current layout")
        print("2. Remap a key")
        print("3. Reset to defaults")
        print("4. Monitor inputs (live)")
        print("5. Quit")
        print()

        choice = input("Choice: ").strip()

        if choice == "1":
            view_layout(ser)
        elif choice == "2":
            remap_key(ser)
        elif choice == "3":
            reset_defaults(ser)
        elif choice == "4":
            monitor_inputs(ser)
        elif choice == "5":
            print("Bye!")
            break
        else:
            print("Invalid choice.")

    ser.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
