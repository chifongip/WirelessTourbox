// WirelessTourbox — HID Keyboard + CDC Serial + EEPROM Config
// Composite USB device with configurable key mappings

#include <Arduino.h>
#include <Adafruit_TinyUSB.h>
#include <EEPROM.h>

// --- Pin definitions ---
#define NUM_INPUTS 10
#define EEPROM_SIZE 21  // 1 magic + 10 × 2 (modifier + keycode)
#define EEPROM_MAGIC 0xA5

#define SW1_PIN 2
#define SW2_PIN 3
#define SW3_PIN 4
#define SW4_PIN 5
#define ENC1_A_PIN 6
#define ENC1_B_PIN 7
#define ENC1_BTN_PIN 8
#define ENC2_A_PIN 10
#define ENC2_B_PIN 11
#define ENC2_BTN_PIN 12

#define STEPS_PER_DETENT 4  // EC11 encoder: 4 quadrature transitions per detent

// --- HID ---
uint8_t const desc_hid_report[] = { TUD_HID_REPORT_DESC_KEYBOARD() };
Adafruit_USBD_HID usb_hid;

// --- Config: [modifier, keycode] per input ---
uint8_t config[NUM_INPUTS][2];

// Default mapping: F13-F22, no modifiers
const uint8_t defaultConfig[NUM_INPUTS][2] = {
    {0x00, 0x68},  // 0: Switch 1     → F13
    {0x00, 0x69},  // 1: Switch 2     → F14
    {0x00, 0x6A},  // 2: Switch 3     → F15
    {0x00, 0x6B},  // 3: Switch 4     → F16
    {0x00, 0x6C},  // 4: Enc 1 Click  → F17
    {0x00, 0x6D},  // 5: Enc 2 Click  → F18
    {0x00, 0x6E},  // 6: Enc 1 CW     → F19
    {0x00, 0x6F},  // 7: Enc 1 CCW    → F20
    {0x00, 0x70},  // 8: Enc 2 CW     → F21
    {0x00, 0x71},  // 9: Enc 2 CCW    → F22
};

// --- Encoder state (quadrature, full state machine) ---
volatile int encoder1Pos = 0;
volatile uint8_t encoder1Last = 0;
volatile int encoder2Pos = 0;
volatile uint8_t encoder2Last = 0;

// --- Serial command buffer ---
#define CMD_BUF_SIZE 64
char cmdBuf[CMD_BUF_SIZE];
int cmdLen = 0;

// ==================== ISR ====================

// Full quadrature state machine with CORRECTED signs.
// CW sequence: 00→01→11→10→00  (delta = +1)
// CCW sequence: 00→10→11→01→00 (delta = -1)
static const int8_t lookup[16] = {
     0,  1, -1,  0,  // 00→00, 00→01(CW), 00→10(CCW), 00→11(invalid)
    -1,  0,  0,  1,  // 01→00(CCW), 01→01, 01→10(invalid), 01→11(CW)
     1,  0,  0, -1,  // 10→00(CW), 10→01(invalid), 10→10, 10→11(CCW)
     0, -1,  1,  0   // 11→00(invalid), 11→01(CCW), 11→10(CW), 11→11
};

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

// ==================== EEPROM ====================

void loadConfig() {
    EEPROM.begin(EEPROM_SIZE);
    if (EEPROM.read(0) != EEPROM_MAGIC) {
        EEPROM.write(0, EEPROM_MAGIC);
        for (int i = 0; i < NUM_INPUTS; i++) {
            EEPROM.write(1 + i * 2, defaultConfig[i][0]);
            EEPROM.write(1 + i * 2 + 1, defaultConfig[i][1]);
        }
        EEPROM.commit();
    }
    for (int i = 0; i < NUM_INPUTS; i++) {
        config[i][0] = EEPROM.read(1 + i * 2);
        config[i][1] = EEPROM.read(1 + i * 2 + 1);
    }
}

void saveConfig() {
    EEPROM.write(0, EEPROM_MAGIC);
    for (int i = 0; i < NUM_INPUTS; i++) {
        EEPROM.write(1 + i * 2, config[i][0]);
        EEPROM.write(1 + i * 2 + 1, config[i][1]);
    }
    EEPROM.commit();
}

// ==================== HID Output ====================

void sendKey(uint8_t index) {
    if (index >= NUM_INPUTS) return;
    if (!usb_hid.ready()) return;
    uint8_t keycode[6] = {0};
    keycode[0] = config[index][1];
    usb_hid.keyboardReport(0, config[index][0], keycode);
    Serial.print("KEY:");
    Serial.print(index);
    Serial.print(":0x");
    Serial.print(config[index][0], HEX);
    Serial.print(":0x");
    Serial.println(config[index][1], HEX);
}

void releaseKey() {
    if (!usb_hid.ready()) return;
    usb_hid.keyboardRelease(0);
}

// ==================== Input Polling ====================

struct SwitchState {
    uint8_t pin;
    uint8_t index;
    bool lastState;
    bool hasKey;
    unsigned long lastChange;
};

SwitchState switches[] = {
    {SW1_PIN, 0, HIGH, false, 0},
    {SW2_PIN, 1, HIGH, false, 0},
    {SW3_PIN, 2, HIGH, false, 0},
    {SW4_PIN, 3, HIGH, false, 0},
    {ENC1_BTN_PIN, 4, HIGH, false, 0},
    {ENC2_BTN_PIN, 5, HIGH, false, 0},
};
#define NUM_SWITCHES 6

void pollSwitches() {
    for (int i = 0; i < NUM_SWITCHES; i++) {
        bool state = digitalRead(switches[i].pin);
        if (state != switches[i].lastState &&
            (millis() - switches[i].lastChange) > 5) {
            switches[i].lastChange = millis();
            switches[i].lastState = state;
            if (state == LOW && !switches[i].hasKey) {
                sendKey(switches[i].index);
                switches[i].hasKey = true;
            } else if (state == HIGH && switches[i].hasKey) {
                releaseKey();
                switches[i].hasKey = false;
            }
        }
    }
}

void checkEncoders() {
    // Encoder 1: report one step per STEPS_PER_DETENT accumulated transitions
    static int enc1LastReported = 0;
    int enc1 = encoder1Pos;
    int enc1Delta = enc1 - enc1LastReported;
    if (enc1Delta >= STEPS_PER_DETENT) {
        sendKey(6);   // CW
        delay(5);
        releaseKey();
        enc1LastReported += STEPS_PER_DETENT;
    } else if (enc1Delta <= -STEPS_PER_DETENT) {
        sendKey(7);   // CCW
        delay(5);
        releaseKey();
        enc1LastReported -= STEPS_PER_DETENT;
    }

    // Encoder 2
    static int enc2LastReported = 0;
    int enc2 = encoder2Pos;
    int enc2Delta = enc2 - enc2LastReported;
    if (enc2Delta >= STEPS_PER_DETENT) {
        sendKey(8);   // CW
        delay(5);
        releaseKey();
        enc2LastReported += STEPS_PER_DETENT;
    } else if (enc2Delta <= -STEPS_PER_DETENT) {
        sendKey(9);   // CCW
        delay(5);
        releaseKey();
        enc2LastReported -= STEPS_PER_DETENT;
    }
}

// ==================== Serial Commands ====================

void processCommand(const char* cmd) {
    if (strcmp(cmd, "GET_LAYOUT") == 0) {
        for (int i = 0; i < NUM_INPUTS; i++) {
            if (i > 0) Serial.print(",");
            Serial.print(config[i][0]);
            Serial.print(":");
            Serial.print(config[i][1]);
        }
        Serial.println();
    }
    else if (strncmp(cmd, "SET_KEY:", 8) == 0) {
        int idx, mod, key;
        if (sscanf(cmd + 8, "%d:%d:%d", &idx, &mod, &key) == 3) {
            if (idx >= 0 && idx < NUM_INPUTS &&
                mod >= 0 && mod <= 255 &&
                key >= 0 && key <= 255) {
                config[idx][0] = (uint8_t)mod;
                config[idx][1] = (uint8_t)key;
                saveConfig();
                Serial.println("OK");
            } else {
                Serial.println("ERR:RANGE");
            }
        } else {
            Serial.println("ERR:PARSE");
        }
    }
    else {
        Serial.println("ERR:UNKNOWN");
    }
}

void readSerial() {
    while (Serial.available()) {
        char c = Serial.read();
        if (c == '\n' || c == '\r') {
            if (cmdLen > 0) {
                cmdBuf[cmdLen] = '\0';
                processCommand(cmdBuf);
                cmdLen = 0;
            }
        } else if (cmdLen < CMD_BUF_SIZE - 1) {
            cmdBuf[cmdLen++] = c;
        }
    }
}

// ==================== Setup ====================

void setup() {
    if (!TinyUSBDevice.isInitialized()) {
        TinyUSBDevice.begin(0);
    }
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

    // Init switch pins
    for (int i = 0; i < NUM_SWITCHES; i++) {
        pinMode(switches[i].pin, INPUT_PULLUP);
    }

    // Init encoder pins
    pinMode(ENC1_A_PIN, INPUT_PULLUP);
    pinMode(ENC1_B_PIN, INPUT_PULLUP);
    pinMode(ENC2_A_PIN, INPUT_PULLUP);
    pinMode(ENC2_B_PIN, INPUT_PULLUP);

    // Read initial encoder state
    encoder1Last = (digitalRead(ENC1_A_PIN) << 1) | digitalRead(ENC1_B_PIN);
    encoder2Last = (digitalRead(ENC2_A_PIN) << 1) | digitalRead(ENC2_B_PIN);

    // Full quadrature: interrupt on CHANGE for both pins
    attachInterrupt(digitalPinToInterrupt(ENC1_A_PIN), encoder1ISR, CHANGE);
    attachInterrupt(digitalPinToInterrupt(ENC1_B_PIN), encoder1ISR, CHANGE);
    attachInterrupt(digitalPinToInterrupt(ENC2_A_PIN), encoder2ISR, CHANGE);
    attachInterrupt(digitalPinToInterrupt(ENC2_B_PIN), encoder2ISR, CHANGE);

    delay(2000);
    Serial.println("WirelessTourbox ready");
}

// ==================== Loop ====================

void loop() {
    #ifdef TINYUSB_NEED_POLLING_TASK
    TinyUSBDevice.task();
    #endif

    if (!TinyUSBDevice.mounted()) return;

    readSerial();
    pollSwitches();
    checkEncoders();
}
