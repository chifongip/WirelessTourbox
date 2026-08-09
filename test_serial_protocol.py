import unittest

from serial_protocol import response_matches, send_command


class FakeSerial:
    def __init__(self, lines):
        self.lines = [line.encode() for line in lines]
        self.writes = []

    def write(self, data):
        self.writes.append(data)

    def readline(self):
        if not self.lines:
            return b""
        return self.lines.pop(0)


class SerialProtocolTests(unittest.TestCase):
    def test_layout_skips_async_events_and_startup_lines(self):
        serial = FakeSerial([
            "KEY:0:0x0:0x68\n",
            "LAYER:1:ON\n",
            "WirelessTourbox ready\n",
            "0:104,0:105\n",
        ])
        events = []

        response = send_command(
            serial, "GET_LAYOUT", delay=0, on_event=events.append
        )

        self.assertEqual("0:104,0:105", response)
        self.assertEqual(b"GET_LAYOUT\n", serial.writes[0])
        self.assertEqual(["KEY:0:0x0:0x68", "LAYER:1:ON"], events)

    def test_mutation_waits_for_ok(self):
        serial = FakeSerial(["LAYER:2:OFF\n", "INFO:WirelessTourbox:3\n", "OK\n"])

        response = send_command(serial, "SET_KEY:0:0:4", delay=0)

        self.assertEqual("OK", response)

    def test_errors_are_valid_command_responses(self):
        serial = FakeSerial(["KEY:1:0x0:0x69\n", "ERR:RANGE\n"])

        response = send_command(serial, "SET_KEY:99:0:0", delay=0)

        self.assertEqual("ERR:RANGE", response)

    def test_compound_chord_response_is_validated(self):
        self.assertTrue(response_matches("GET_CHORDS:1", "1:4+5+6,0:0"))
        self.assertFalse(response_matches("GET_CHORDS:1", "LAYERCFG:200:1=0"))


if __name__ == "__main__":
    unittest.main()
