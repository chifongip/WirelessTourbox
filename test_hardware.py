#!/usr/bin/env python3
"""
Hardware test script for WirelessTourbox.
Validates all inputs and key mappings via serial debug output.

Usage: python3 test_hardware.py [port]
"""

import serial
import sys
import time

PORT = sys.argv[1] if len(sys.argv) > 1 else "/dev/ttyACM0"
BAUD = 115200
TIMEOUT = 30

INPUT_NAMES = {
    0: "Switch 1 (GPIO2)",
    1: "Switch 2 (GPIO3)",
    2: "Switch 3 (GPIO4)",
    3: "Switch 4 (GPIO5)",
    4: "Enc 1 Click (GPIO8)",
    5: "Enc 2 Click (GPIO12)",
    6: "Encoder 1 CW",
    7: "Encoder 1 CCW",
    8: "Encoder 2 CW",
    9: "Encoder 2 CCW",
}

HID_KEYS = {
    0x68: "F13", 0x69: "F14", 0x6A: "F15", 0x6B: "F16",
    0x6C: "F17", 0x6D: "F18", 0x6E: "F19", 0x6F: "F20",
    0x70: "F21", 0x71: "F22",
}


def test_serial_commands(ser):
    """Test GET_LAYOUT and SET_KEY commands."""
    print("\n--- Serial Command Tests ---")
    results = []

    # Test GET_LAYOUT
    ser.write(b"GET_LAYOUT\n")
    time.sleep(0.2)
    resp = ser.readline().decode("utf-8", errors="replace").strip()
    ok = resp.startswith("0:104") or resp.startswith("0:0:")
    results.append(("GET_LAYOUT returns config", ok, resp))

    # Test SET_KEY
    ser.write(b"SET_KEY:0:1:61\n")
    time.sleep(0.2)
    resp = ser.readline().decode("utf-8", errors="replace").strip()
    ok = resp == "OK"
    results.append(("SET_KEY accepts valid command", ok, resp))

    # Verify change
    ser.write(b"GET_LAYOUT\n")
    time.sleep(0.2)
    resp = ser.readline().decode("utf-8", errors="replace").strip()
    ok = resp.startswith("1:61")
    results.append(("SET_KEY persists change", ok, resp))

    # Reset
    ser.write(b"SET_KEY:0:0:104\n")
    time.sleep(0.2)
    ser.readline()

    # Test invalid
    ser.write(b"SET_KEY:99:0:0\n")
    time.sleep(0.2)
    resp = ser.readline().decode("utf-8", errors="replace").strip()
    ok = resp.startswith("ERR")
    results.append(("SET_KEY rejects invalid index", ok, resp))

    return results


def test_inputs(ser):
    """Test physical inputs via debug output."""
    print("\n--- Input Tests (perform within 30s) ---")
    print("TEST 1: Press switch on GPIO2")
    print("TEST 2: Rotate encoder 1 CW")
    print("TEST 3: Rotate encoder 1 CCW")
    print("-" * 50)

    ser.reset_input_buffer()
    detected = {}
    start = time.time()

    while time.time() - start < TIMEOUT:
        line = ser.readline().decode("utf-8", errors="replace").strip()
        if not line or not line.startswith("KEY:"):
            continue

        parts = line.split(":")
        if len(parts) != 4:
            continue

        idx = int(parts[1])
        mod = int(parts[2], 16)
        key = int(parts[3], 16)
        name = INPUT_NAMES.get(idx, f"Input {idx}")
        key_name = HID_KEYS.get(key, f"0x{key:02X}")

        elapsed = time.time() - start
        print(f"[{elapsed:5.1f}s] {name} → {key_name}")

        if idx not in detected:
            detected[idx] = True

    return detected


def main():
    print(f"Connecting to {PORT}...")
    try:
        ser = serial.Serial(PORT, BAUD, timeout=0.5)
    except serial.SerialException as e:
        print(f"Error: {e}")
        return 1

    time.sleep(0.5)
    while ser.in_waiting:
        ser.readline()

    # Serial command tests
    cmd_results = test_serial_commands(ser)

    # Input tests
    detected_inputs = test_inputs(ser)

    ser.close()

    # Results
    print("\n" + "=" * 50)
    print("RESULTS")
    print("=" * 50)

    all_pass = True

    print("\nSerial Commands:")
    for name, passed, detail in cmd_results:
        status = "PASS" if passed else "FAIL"
        if not passed:
            all_pass = False
        print(f"  [{status}] {name}")
        if not passed:
            print(f"         got: {detail}")

    print("\nInputs:")
    for idx, name in INPUT_NAMES.items():
        passed = idx in detected_inputs
        status = "PASS" if passed else "FAIL"
        if not passed:
            all_pass = False
        print(f"  [{status}] {name}")

    print()
    if all_pass:
        print("ALL TESTS PASSED")
    else:
        print("SOME TESTS FAILED")

    return 0 if all_pass else 1


if __name__ == "__main__":
    sys.exit(main())
