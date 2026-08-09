"""Shared serial command/response handling for WirelessTourbox tools."""

import re
import time


ASYNC_EVENT_PREFIXES = ("KEY:", "LAYER:")
_LAYOUT_RESPONSE = re.compile(r"^\d+:\d+(?:,\d+:\d+)*$")
_CHORD_RESPONSE = re.compile(r"^\d+:\d+(?:\+\d+){0,2}(?:,\d+:\d+(?:\+\d+){0,2})*$")
_RESPONSE_PREFIXES = {
    "GET_INFO": "INFO:",
    "GET_CAPS": "CAPS:",
    "GET_INPUTS": "INPUTS:",
    "GET_LAYER_CONFIG": "LAYERCFG:",
}
_MUTATING_COMMANDS = (
    "SET_KEY:",
    "SET_CHORD:",
    "SET_LAYER:",
    "REMOVE_LAYER:",
    "SET_HOLD_MS:",
)


def is_async_event(line):
    """Return whether a line is an unsolicited physical-input event."""
    return line.startswith(ASYNC_EVENT_PREFIXES)


def response_matches(command, line):
    """Return whether a non-event line is a valid response to command."""
    if line.startswith("ERR:"):
        return True
    if command == "GET_LAYOUT" or command.startswith("GET_LAYOUT:"):
        return _LAYOUT_RESPONSE.fullmatch(line) is not None
    if command.startswith("GET_CHORDS:"):
        return _CHORD_RESPONSE.fullmatch(line) is not None
    expected_prefix = _RESPONSE_PREFIXES.get(command)
    if expected_prefix is not None:
        return line.startswith(expected_prefix)
    if command == "RESET_DEFAULTS" or command.startswith(_MUTATING_COMMANDS):
        return line == "OK"
    return False


def send_command(ser, command, attempts=20, delay=0.05, on_event=None):
    """Send one command and wait for its matching response.

    Asynchronous KEY and LAYER events are skipped and optionally reported via
    on_event. Other unrelated lines are ignored so startup messages cannot be
    mistaken for command responses.
    """
    ser.write((command + "\n").encode())
    for _ in range(attempts):
        if delay:
            time.sleep(delay)
        raw = ser.readline()
        if isinstance(raw, bytes):
            line = raw.decode("utf-8", errors="replace").strip()
        else:
            line = str(raw).strip()
        if not line:
            continue
        if is_async_event(line):
            if on_event is not None:
                on_event(line)
            continue
        if response_matches(command, line):
            return line
    return ""
