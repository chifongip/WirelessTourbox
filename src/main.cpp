// WirelessTourbox — HID Keyboard + CDC Serial + EEPROM Config

#include <Arduino.h>
#include <Adafruit_TinyUSB.h>
#include <EEPROM.h>
#include <TourboxCore.h>

constexpr uint8_t NUM_INPUTS = 10;
constexpr uint8_t NUM_SWITCHES = 6;
constexpr uint16_t EEPROM_SIZE = 21;
constexpr uint8_t EEPROM_MAGIC = 0xA5;
constexpr uint8_t STEPS_PER_DETENT = 4;
constexpr unsigned long DEBOUNCE_MS = 5;
constexpr unsigned long PULSE_MS = 8;
constexpr unsigned long PULSE_GAP_MS = 2;
constexpr uint8_t PULSE_QUEUE_SIZE = 32;

constexpr uint8_t SW1_PIN = 2;
constexpr uint8_t SW2_PIN = 3;
constexpr uint8_t SW3_PIN = 4;
constexpr uint8_t SW4_PIN = 5;
constexpr uint8_t ENC1_A_PIN = 6;
constexpr uint8_t ENC1_B_PIN = 7;
constexpr uint8_t ENC1_BTN_PIN = 8;
constexpr uint8_t ENC2_A_PIN = 10;
constexpr uint8_t ENC2_B_PIN = 11;
constexpr uint8_t ENC2_BTN_PIN = 12;

uint8_t const desc_hid_report[] = {TUD_HID_REPORT_DESC_KEYBOARD()};
Adafruit_USBD_HID usb_hid;

uint8_t config[NUM_INPUTS][2];
const uint8_t defaultConfig[NUM_INPUTS][2] = {
    {0x00, 0x68}, {0x00, 0x69}, {0x00, 0x6A}, {0x00, 0x6B},
    {0x00, 0x6C}, {0x00, 0x6D}, {0x00, 0x6E}, {0x00, 0x6F},
    {0x00, 0x70}, {0x00, 0x71},
};

volatile int32_t encoder1Pos = 0;
volatile uint8_t encoder1Last = 0;
volatile int32_t encoder2Pos = 0;
volatile uint8_t encoder2Last = 0;

static const int8_t lookup[16] = {
     0,  1, -1,  0,
    -1,  0,  0,  1,
     1,  0,  0, -1,
     0, -1,  1,  0,
};

struct SwitchState {
    uint8_t pin;
    uint8_t index;
    tourbox::DebounceState debounce;
};

SwitchState switches[NUM_SWITCHES] = {
    {SW1_PIN, 0, {HIGH, HIGH, 0}},
    {SW2_PIN, 1, {HIGH, HIGH, 0}},
    {SW3_PIN, 2, {HIGH, HIGH, 0}},
    {SW4_PIN, 3, {HIGH, HIGH, 0}},
    {ENC1_BTN_PIN, 4, {HIGH, HIGH, 0}},
    {ENC2_BTN_PIN, 5, {HIGH, HIGH, 0}},
};

bool switchActive[NUM_SWITCHES] = {false};
uint8_t pulseQueue[PULSE_QUEUE_SIZE];
uint8_t pulseHead = 0;
uint8_t pulseTail = 0;
uint8_t pulseCount = 0;
bool pulseActive = false;
uint8_t pulseIndex = 0;
unsigned long pulseReleaseAt = 0;
unsigned long pulseGapUntil = 0;
bool reportDirty = true;

constexpr uint8_t CMD_BUF_SIZE = 64;
char cmdBuf[CMD_BUF_SIZE];
uint8_t cmdLen = 0;
bool cmdOverflow = false;

void encoder1ISR() {
    uint8_t state = (digitalRead(ENC1_A_PIN) << 1) | digitalRead(ENC1_B_PIN);
    encoder1Pos += lookup[(encoder1Last << 2) | state];
    encoder1Last = state;
}

void encoder2ISR() {
    uint8_t state = (digitalRead(ENC2_A_PIN) << 1) | digitalRead(ENC2_B_PIN);
    encoder2Pos += lookup[(encoder2Last << 2) | state];
    encoder2Last = state;
}

void loadConfig() {
    EEPROM.begin(EEPROM_SIZE);
    if (EEPROM.read(0) != EEPROM_MAGIC) {
        EEPROM.write(0, EEPROM_MAGIC);
        for (uint8_t i = 0; i < NUM_INPUTS; ++i) {
            EEPROM.write(1 + i * 2, defaultConfig[i][0]);
            EEPROM.write(2 + i * 2, defaultConfig[i][1]);
        }
        EEPROM.commit();
    }
    for (uint8_t i = 0; i < NUM_INPUTS; ++i) {
        config[i][0] = EEPROM.read(1 + i * 2);
        config[i][1] = EEPROM.read(2 + i * 2);
    }
}

void saveConfig() {
    EEPROM.write(0, EEPROM_MAGIC);
    for (uint8_t i = 0; i < NUM_INPUTS; ++i) {
        EEPROM.write(1 + i * 2, config[i][0]);
        EEPROM.write(2 + i * 2, config[i][1]);
    }
    EEPROM.commit();
}

void resetConfig() {
    bool changed = false;
    for (uint8_t i = 0; i < NUM_INPUTS; ++i) {
        changed |= config[i][0] != defaultConfig[i][0] ||
                   config[i][1] != defaultConfig[i][1];
        config[i][0] = defaultConfig[i][0];
        config[i][1] = defaultConfig[i][1];
    }
    if (changed) saveConfig();
    reportDirty = true;
}

void logKeyEvent(uint8_t index) {
    Serial.print("KEY:");
    Serial.print(index);
    Serial.print(":0x");
    Serial.print(config[index][0], HEX);
    Serial.print(":0x");
    Serial.println(config[index][1], HEX);
}

bool pulseFits(uint8_t index) {
    tourbox::KeyboardReport report = tourbox::composeReport(
        config, switchActive, NUM_SWITCHES, -1);
    uint8_t keycode = config[index][1];
    return keycode == 0 || tourbox::containsKey(report, keycode) || report.count < 6;
}

void sendCurrentReport() {
    if (!reportDirty || !usb_hid.ready()) return;
    tourbox::KeyboardReport report = tourbox::composeReport(
        config, switchActive, NUM_SWITCHES, pulseActive ? pulseIndex : -1);
    usb_hid.keyboardReport(0, report.modifiers, report.keys);
    reportDirty = false;
}

bool enqueuePulse(uint8_t index) {
    if (pulseCount == PULSE_QUEUE_SIZE) return false;
    pulseQueue[pulseTail] = index;
    pulseTail = (pulseTail + 1) % PULSE_QUEUE_SIZE;
    ++pulseCount;
    return true;
}

void servicePulse() {
    unsigned long now = millis();
    if (pulseActive && static_cast<int32_t>(now - pulseReleaseAt) >= 0) {
        pulseActive = false;
        pulseGapUntil = now + PULSE_GAP_MS;
        reportDirty = true;
    }
    if (!pulseActive && pulseCount > 0 &&
        static_cast<int32_t>(now - pulseGapUntil) >= 0 &&
        pulseFits(pulseQueue[pulseHead])) {
        pulseIndex = pulseQueue[pulseHead];
        pulseHead = (pulseHead + 1) % PULSE_QUEUE_SIZE;
        --pulseCount;
        pulseActive = true;
        pulseReleaseAt = now + PULSE_MS;
        reportDirty = true;
        logKeyEvent(pulseIndex);
    }
}

void pollSwitches() {
    unsigned long now = millis();
    for (uint8_t i = 0; i < NUM_SWITCHES; ++i) {
        bool state = digitalRead(switches[i].pin);
        if (tourbox::updateDebounce(switches[i].debounce, state, now, DEBOUNCE_MS)) {
            switchActive[i] = switches[i].debounce.stable == LOW;
            reportDirty = true;
            if (switchActive[i]) logKeyEvent(switches[i].index);
        }
    }
}

void checkEncoders() {
    static int32_t enc1Reported = 0;
    static int32_t enc2Reported = 0;
    noInterrupts();
    int32_t enc1 = encoder1Pos;
    int32_t enc2 = encoder2Pos;
    interrupts();

    while (enc1 - enc1Reported >= STEPS_PER_DETENT && enqueuePulse(6)) {
        enc1Reported += STEPS_PER_DETENT;
    }
    while (enc1 - enc1Reported <= -STEPS_PER_DETENT && enqueuePulse(7)) {
        enc1Reported -= STEPS_PER_DETENT;
    }
    while (enc2 - enc2Reported >= STEPS_PER_DETENT && enqueuePulse(8)) {
        enc2Reported += STEPS_PER_DETENT;
    }
    while (enc2 - enc2Reported <= -STEPS_PER_DETENT && enqueuePulse(9)) {
        enc2Reported -= STEPS_PER_DETENT;
    }
}

void printLayout() {
    for (uint8_t i = 0; i < NUM_INPUTS; ++i) {
        if (i > 0) Serial.print(',');
        Serial.print(config[i][0]);
        Serial.print(':');
        Serial.print(config[i][1]);
    }
    Serial.println();
}

void processCommand(const char* cmd) {
    if (strcmp(cmd, "GET_INFO") == 0) {
        Serial.println("INFO:WirelessTourbox:1");
    } else if (strcmp(cmd, "GET_LAYOUT") == 0) {
        printLayout();
    } else if (strcmp(cmd, "RESET_DEFAULTS") == 0) {
        resetConfig();
        Serial.println("OK");
    } else if (strncmp(cmd, "SET_KEY:", 8) == 0) {
        int idx, mod, key;
        char trailing;
        if (sscanf(cmd + 8, "%d:%d:%d%c", &idx, &mod, &key, &trailing) != 3) {
            Serial.println("ERR:PARSE");
        } else if (idx < 0 || idx >= NUM_INPUTS || mod < 0 || mod > 255 ||
                   key < 0 || key > 255) {
            Serial.println("ERR:RANGE");
        } else {
            bool changed = config[idx][0] != static_cast<uint8_t>(mod) ||
                           config[idx][1] != static_cast<uint8_t>(key);
            config[idx][0] = static_cast<uint8_t>(mod);
            config[idx][1] = static_cast<uint8_t>(key);
            if (changed) saveConfig();
            reportDirty = true;
            Serial.println("OK");
        }
    } else {
        Serial.println("ERR:UNKNOWN");
    }
}

void readSerial() {
    while (Serial.available()) {
        char c = Serial.read();
        if (c == '\n' || c == '\r') {
            if (cmdOverflow) {
                Serial.println("ERR:TOO_LONG");
            } else if (cmdLen > 0) {
                cmdBuf[cmdLen] = '\0';
                processCommand(cmdBuf);
            }
            cmdLen = 0;
            cmdOverflow = false;
        } else if (!cmdOverflow && cmdLen < CMD_BUF_SIZE - 1) {
            cmdBuf[cmdLen++] = c;
        } else {
            cmdOverflow = true;
        }
    }
}

void setup() {
    if (!TinyUSBDevice.isInitialized()) TinyUSBDevice.begin(0);
    Serial.begin(115200);

    usb_hid.setPollInterval(2);
    usb_hid.setBootProtocol(HID_ITF_PROTOCOL_KEYBOARD);
    usb_hid.setReportDescriptor(desc_hid_report, sizeof(desc_hid_report));
    usb_hid.setStringDescriptor("WirelessTourbox");
    usb_hid.begin();

    if (TinyUSBDevice.mounted()) {
        TinyUSBDevice.detach();
        delay(10);
        TinyUSBDevice.attach();
    }

    loadConfig();
    for (uint8_t i = 0; i < NUM_SWITCHES; ++i) {
        pinMode(switches[i].pin, INPUT_PULLUP);
        bool initial = digitalRead(switches[i].pin);
        switches[i].debounce = {initial, initial, millis()};
        switchActive[i] = initial == LOW;
    }

    pinMode(ENC1_A_PIN, INPUT_PULLUP);
    pinMode(ENC1_B_PIN, INPUT_PULLUP);
    pinMode(ENC2_A_PIN, INPUT_PULLUP);
    pinMode(ENC2_B_PIN, INPUT_PULLUP);
    encoder1Last = (digitalRead(ENC1_A_PIN) << 1) | digitalRead(ENC1_B_PIN);
    encoder2Last = (digitalRead(ENC2_A_PIN) << 1) | digitalRead(ENC2_B_PIN);
    attachInterrupt(digitalPinToInterrupt(ENC1_A_PIN), encoder1ISR, CHANGE);
    attachInterrupt(digitalPinToInterrupt(ENC1_B_PIN), encoder1ISR, CHANGE);
    attachInterrupt(digitalPinToInterrupt(ENC2_A_PIN), encoder2ISR, CHANGE);
    attachInterrupt(digitalPinToInterrupt(ENC2_B_PIN), encoder2ISR, CHANGE);

    Serial.println("WirelessTourbox ready");
}

void loop() {
#ifdef TINYUSB_NEED_POLLING_TASK
    TinyUSBDevice.task();
#endif
    readSerial();
    pollSwitches();
    checkEncoders();
    servicePulse();
    sendCurrentReport();
}
