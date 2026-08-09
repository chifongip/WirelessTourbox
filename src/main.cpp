// WirelessTourbox — layered HID keyboard + CDC serial configuration

#include <Arduino.h>
#include <Adafruit_TinyUSB.h>
#include <EEPROM.h>
#include <TourboxCore.h>

constexpr uint8_t NUM_INPUTS = 10;
constexpr uint8_t NUM_SWITCHES = 6;
constexpr uint8_t STEPS_PER_DETENT = 4;
constexpr uint32_t DEBOUNCE_MS = 5;
constexpr uint32_t PULSE_MS = 8;
constexpr uint32_t PULSE_GAP_MS = 2;
constexpr uint8_t PULSE_QUEUE_SIZE = 32;
constexpr uint16_t DEFAULT_HOLD_MS = 200;
constexpr uint16_t MIN_HOLD_MS = 100;
constexpr uint16_t MAX_HOLD_MS = 1000;

constexpr uint16_t STORAGE_MAGIC = 0x5742;
constexpr uint8_t STORAGE_SCHEMA = 3;
constexpr uint8_t PREVIOUS_STORAGE_SCHEMA = 2;
constexpr uint8_t LEGACY_MAGIC = 0xA5;
constexpr uint16_t HEADER_SIZE = 21;
constexpr uint16_t MAPPINGS_SIZE =
    tourbox::MODE_COUNT * tourbox::MAX_INPUTS * sizeof(tourbox::Mapping);
constexpr uint16_t CRC_OFFSET = HEADER_SIZE + MAPPINGS_SIZE;
constexpr uint16_t EEPROM_SIZE = CRC_OFFSET + 2;
constexpr uint16_t V2_MAPPING_SIZE =
    tourbox::MODE_COUNT * tourbox::MAX_INPUTS * 2;
constexpr uint16_t V2_CRC_OFFSET = HEADER_SIZE + V2_MAPPING_SIZE;
constexpr uint16_t V2_EEPROM_SIZE = V2_CRC_OFFSET + 2;

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

struct InputDescriptor {
    const char* name;
    char kind;
    bool layerEligible;
};

const InputDescriptor inputDescriptors[NUM_INPUTS] = {
    {"Switch_1", 'S', true},       {"Switch_2", 'S', true},
    {"Switch_3", 'S', true},       {"Switch_4", 'S', true},
    {"Encoder_1_Click", 'B', false}, {"Encoder_2_Click", 'B', false},
    {"Encoder_1_CW", 'E', false},  {"Encoder_1_CCW", 'E', false},
    {"Encoder_2_CW", 'E', false},  {"Encoder_2_CCW", 'E', false},
};

const tourbox::Mapping defaultMappings[NUM_INPUTS] = {
    {0x00, {0x68, 0, 0}}, {0x00, {0x69, 0, 0}},
    {0x00, {0x6A, 0, 0}}, {0x00, {0x6B, 0, 0}},
    {0x00, {0x6C, 0, 0}}, {0x00, {0x6D, 0, 0}},
    {0x00, {0x6E, 0, 0}}, {0x00, {0x6F, 0, 0}},
    {0x00, {0x70, 0, 0}}, {0x00, {0x71, 0, 0}},
};

uint8_t const desc_hid_report[] = {TUD_HID_REPORT_DESC_KEYBOARD()};
Adafruit_USBD_HID usb_hid;

tourbox::Mapping mappings[tourbox::MODE_COUNT][tourbox::MAX_INPUTS];
uint8_t layerTriggers[tourbox::MAX_LAYERS];
uint16_t holdMs = DEFAULT_HOLD_MS;
tourbox::LayerState layerState;

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
    {SW1_PIN, 0, {HIGH, HIGH, 0}}, {SW2_PIN, 1, {HIGH, HIGH, 0}},
    {SW3_PIN, 2, {HIGH, HIGH, 0}}, {SW4_PIN, 3, {HIGH, HIGH, 0}},
    {ENC1_BTN_PIN, 4, {HIGH, HIGH, 0}},
    {ENC2_BTN_PIN, 5, {HIGH, HIGH, 0}},
};

bool switchActive[NUM_SWITCHES] = {false};
tourbox::Mapping activeMappings[NUM_SWITCHES];

struct PulseEvent {
    uint8_t index;
    tourbox::Mapping mapping;
};

PulseEvent pulseQueue[PULSE_QUEUE_SIZE];
uint8_t pulseHead = 0;
uint8_t pulseTail = 0;
uint8_t pulseCount = 0;
bool pulseActive = false;
PulseEvent activePulse = {0, {0, {0, 0, 0}}};
uint32_t pulseReleaseAt = 0;
uint32_t pulseGapUntil = 0;
bool reportDirty = true;

constexpr uint8_t CMD_BUF_SIZE = 96;
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

void setDefaults() {
    memset(mappings, 0, sizeof(mappings));
    for (uint8_t i = 0; i < NUM_INPUTS; ++i) mappings[0][i] = defaultMappings[i];
    memset(layerTriggers, tourbox::NO_TRIGGER, sizeof(layerTriggers));
    holdMs = DEFAULT_HOLD_MS;
}

void saveConfig() {
    uint8_t image[EEPROM_SIZE];
    memset(image, 0, sizeof(image));
    image[0] = STORAGE_MAGIC & 0xFF;
    image[1] = STORAGE_MAGIC >> 8;
    image[2] = STORAGE_SCHEMA;
    image[3] = NUM_INPUTS;
    image[4] = holdMs & 0xFF;
    image[5] = holdMs >> 8;
    memcpy(image + 6, layerTriggers, tourbox::MAX_LAYERS);
    memcpy(image + HEADER_SIZE, mappings, MAPPINGS_SIZE);
    uint16_t crc = tourbox::crc16(image, CRC_OFFSET);
    image[CRC_OFFSET] = crc & 0xFF;
    image[CRC_OFFSET + 1] = crc >> 8;
    for (uint16_t i = 0; i < EEPROM_SIZE; ++i) EEPROM.write(i, image[i]);
    EEPROM.commit();
}

bool validTrigger(uint8_t input) {
    return input < NUM_INPUTS && inputDescriptors[input].layerEligible;
}

bool duplicateTrigger(uint8_t slot, uint8_t trigger) {
    for (uint8_t i = 0; i < tourbox::MAX_LAYERS; ++i) {
        if (i != slot && layerTriggers[i] == trigger) return true;
    }
    return false;
}

bool sanitizeMapping(tourbox::Mapping& mapping) {
    tourbox::Mapping clean = {mapping.modifier, {0, 0, 0}};
    uint8_t count = 0;
    for (uint8_t i = 0; i < tourbox::MAX_MAPPING_KEYS; ++i) {
        uint8_t key = mapping.keys[i];
        if (key == 0 || tourbox::mappingContainsKey(clean, key)) continue;
        clean.keys[count++] = key;
    }
    if (count == 0) clean.modifier = 0;
    bool changed = memcmp(&mapping, &clean, sizeof(mapping)) != 0;
    mapping = clean;
    return changed;
}

bool validMapping(const tourbox::Mapping& mapping) {
    bool foundZero = false;
    uint8_t count = 0;
    for (uint8_t i = 0; i < tourbox::MAX_MAPPING_KEYS; ++i) {
        uint8_t key = mapping.keys[i];
        if (key == 0) {
            foundZero = true;
            continue;
        }
        if (foundZero) return false;
        for (uint8_t previous = 0; previous < i; ++previous) {
            if (mapping.keys[previous] == key) return false;
        }
        ++count;
    }
    return count > 0 || mapping.modifier == 0;
}

bool sanitizeConfig(uint8_t storedInputs) {
    bool sanitized = false;
    for (uint8_t slot = 0; slot < tourbox::MAX_LAYERS; ++slot) {
        if (layerTriggers[slot] != tourbox::NO_TRIGGER &&
            (!validTrigger(layerTriggers[slot]) ||
             duplicateTrigger(slot, layerTriggers[slot]))) {
            layerTriggers[slot] = tourbox::NO_TRIGGER;
            memset(mappings[slot + 1], 0, sizeof(mappings[slot + 1]));
            sanitized = true;
        }
    }
    for (uint8_t layer = 0; layer < tourbox::MODE_COUNT; ++layer) {
        for (uint8_t input = 0; input < tourbox::MAX_INPUTS; ++input) {
            sanitized |= sanitizeMapping(mappings[layer][input]);
        }
    }
    if (storedInputs < NUM_INPUTS) {
        for (uint8_t i = storedInputs; i < NUM_INPUTS; ++i) {
            mappings[0][i] = defaultMappings[i];
        }
        sanitized = true;
    }
    return sanitized;
}

void loadConfig() {
    EEPROM.begin(EEPROM_SIZE);
    if (EEPROM.read(0) == LEGACY_MAGIC) {
        setDefaults();
        for (uint8_t i = 0; i < NUM_INPUTS; ++i) {
            mappings[0][i] = tourbox::singleKeyMapping(
                EEPROM.read(1 + i * 2), EEPROM.read(2 + i * 2));
        }
        saveConfig();
        return;
    }

    uint8_t image[EEPROM_SIZE];
    for (uint16_t i = 0; i < EEPROM_SIZE; ++i) image[i] = EEPROM.read(i);
    uint16_t magic = static_cast<uint16_t>(image[0]) |
                     (static_cast<uint16_t>(image[1]) << 8);
    if (magic != STORAGE_MAGIC) {
        setDefaults();
        saveConfig();
        return;
    }

    if (image[2] == PREVIOUS_STORAGE_SCHEMA) {
        uint16_t storedCrc = static_cast<uint16_t>(image[V2_CRC_OFFSET]) |
                             (static_cast<uint16_t>(image[V2_CRC_OFFSET + 1]) << 8);
        if (storedCrc != tourbox::crc16(image, V2_CRC_OFFSET)) {
            setDefaults();
            saveConfig();
            return;
        }
        uint8_t storedInputs = image[3];
        holdMs = static_cast<uint16_t>(image[4]) |
                 (static_cast<uint16_t>(image[5]) << 8);
        memcpy(layerTriggers, image + 6, tourbox::MAX_LAYERS);
        memset(mappings, 0, sizeof(mappings));
        tourbox::expandSingleKeyMappings(
            image + HEADER_SIZE, &mappings[0][0],
            tourbox::MODE_COUNT * tourbox::MAX_INPUTS);
        if (holdMs < MIN_HOLD_MS || holdMs > MAX_HOLD_MS) holdMs = DEFAULT_HOLD_MS;
        sanitizeConfig(storedInputs);
        saveConfig();
        return;
    }

    uint16_t storedCrc = static_cast<uint16_t>(image[CRC_OFFSET]) |
                         (static_cast<uint16_t>(image[CRC_OFFSET + 1]) << 8);
    if (image[2] != STORAGE_SCHEMA ||
        storedCrc != tourbox::crc16(image, CRC_OFFSET)) {
        setDefaults();
        saveConfig();
        return;
    }

    uint8_t storedInputs = image[3];
    holdMs = static_cast<uint16_t>(image[4]) |
             (static_cast<uint16_t>(image[5]) << 8);
    bool sanitized = false;
    if (holdMs < MIN_HOLD_MS || holdMs > MAX_HOLD_MS) {
        holdMs = DEFAULT_HOLD_MS;
        sanitized = true;
    }
    memcpy(layerTriggers, image + 6, tourbox::MAX_LAYERS);
    memcpy(mappings, image + HEADER_SIZE, MAPPINGS_SIZE);
    sanitized |= sanitizeConfig(storedInputs);
    if (sanitized) saveConfig();
}

uint8_t layerForTrigger(uint8_t input) {
    for (uint8_t slot = 0; slot < tourbox::MAX_LAYERS; ++slot) {
        if (layerTriggers[slot] == input) return slot + 1;
    }
    return 0;
}

void logKeyEvent(uint8_t index, const tourbox::Mapping& mapping) {
    Serial.print("KEY:");
    Serial.print(index);
    Serial.print(":0x");
    Serial.print(mapping.modifier, HEX);
    Serial.print(":0x");
    Serial.println(mapping.keys[0], HEX);
}

void logLayer(uint8_t layer, bool active) {
    Serial.print("LAYER:");
    Serial.print(layer);
    Serial.println(active ? ":ON" : ":OFF");
}

void promotePending(uint8_t input) {
    if (layerState.promoteForActivity(input)) logLayer(layerState.currentLayer(), true);
}

bool pulseFits(const tourbox::Mapping& mapping) {
    tourbox::KeyboardReport report = tourbox::composeReport(
        activeMappings, switchActive, NUM_SWITCHES, nullptr);
    return tourbox::canAddMapping(report, mapping);
}

void sendCurrentReport() {
    if (!reportDirty || !usb_hid.ready()) return;
    const tourbox::Mapping* pulse = pulseActive ? &activePulse.mapping : nullptr;
    tourbox::KeyboardReport report = tourbox::composeReport(
        activeMappings, switchActive, NUM_SWITCHES, pulse);
    usb_hid.keyboardReport(0, report.modifiers, report.keys);
    reportDirty = false;
}

bool enqueuePulse(uint8_t index, const tourbox::Mapping& mapping) {
    if (pulseCount == PULSE_QUEUE_SIZE) return false;
    pulseQueue[pulseTail] = {index, mapping};
    pulseTail = (pulseTail + 1) % PULSE_QUEUE_SIZE;
    ++pulseCount;
    logKeyEvent(index, mapping);
    return true;
}

void servicePulse() {
    uint32_t now = millis();
    if (pulseActive && static_cast<int32_t>(now - pulseReleaseAt) >= 0) {
        pulseActive = false;
        pulseGapUntil = now + PULSE_GAP_MS;
        reportDirty = true;
    }
    if (!pulseActive && pulseCount > 0 &&
        static_cast<int32_t>(now - pulseGapUntil) >= 0 &&
        pulseFits(pulseQueue[pulseHead].mapping)) {
        activePulse = pulseQueue[pulseHead];
        pulseHead = (pulseHead + 1) % PULSE_QUEUE_SIZE;
        --pulseCount;
        pulseActive = true;
        pulseReleaseAt = now + PULSE_MS;
        reportDirty = true;
    }
}

void pressSwitch(uint8_t switchSlot) {
    uint8_t input = switches[switchSlot].index;
    promotePending(input);
    uint8_t triggerLayer = layerForTrigger(input);
    if (layerState.currentLayer() == 0 && triggerLayer != 0 &&
        layerState.beginTrigger(input, triggerLayer, millis())) {
        return;
    }
    activeMappings[switchSlot] = mappings[layerState.currentLayer()][input];
    switchActive[switchSlot] = true;
    reportDirty = true;
    logKeyEvent(input, activeMappings[switchSlot]);
}

void releaseSwitch(uint8_t switchSlot) {
    uint8_t input = switches[switchSlot].index;
    uint8_t activeLayer = layerState.currentLayer();
    tourbox::LayerRelease release = layerState.release(input);
    if (release == tourbox::LayerRelease::Tap) {
        enqueuePulse(input, mappings[0][input]);
    } else if (release == tourbox::LayerRelease::Deactivated) {
        logLayer(activeLayer, false);
    } else if (switchActive[switchSlot]) {
        switchActive[switchSlot] = false;
        reportDirty = true;
    }
}

void pollSwitches() {
    uint32_t now = millis();
    if (layerState.update(now, holdMs)) logLayer(layerState.currentLayer(), true);
    for (uint8_t i = 0; i < NUM_SWITCHES; ++i) {
        bool state = digitalRead(switches[i].pin);
        if (tourbox::updateDebounce(switches[i].debounce, state, now, DEBOUNCE_MS)) {
            if (switches[i].debounce.stable == LOW) pressSwitch(i);
            else releaseSwitch(i);
        }
    }
}

bool enqueueEncoder(uint8_t input) {
    promotePending(input);
    return enqueuePulse(input, mappings[layerState.currentLayer()][input]);
}

void checkEncoders() {
    static int32_t enc1Reported = 0;
    static int32_t enc2Reported = 0;
    noInterrupts();
    int32_t enc1 = encoder1Pos;
    int32_t enc2 = encoder2Pos;
    interrupts();
    while (enc1 - enc1Reported >= STEPS_PER_DETENT && enqueueEncoder(6)) enc1Reported += STEPS_PER_DETENT;
    while (enc1 - enc1Reported <= -STEPS_PER_DETENT && enqueueEncoder(7)) enc1Reported -= STEPS_PER_DETENT;
    while (enc2 - enc2Reported >= STEPS_PER_DETENT && enqueueEncoder(8)) enc2Reported += STEPS_PER_DETENT;
    while (enc2 - enc2Reported <= -STEPS_PER_DETENT && enqueueEncoder(9)) enc2Reported -= STEPS_PER_DETENT;
}

void printLayout(uint8_t layer) {
    for (uint8_t i = 0; i < NUM_INPUTS; ++i) {
        if (i > 0) Serial.print(',');
        Serial.print(mappings[layer][i].modifier);
        Serial.print(':');
        Serial.print(mappings[layer][i].keys[0]);
    }
    Serial.println();
}

void printChords(uint8_t layer) {
    for (uint8_t input = 0; input < NUM_INPUTS; ++input) {
        if (input > 0) Serial.print(',');
        const tourbox::Mapping& mapping = mappings[layer][input];
        Serial.print(mapping.modifier);
        Serial.print(':');
        uint8_t count = tourbox::mappingKeyCount(mapping);
        if (count == 0) {
            Serial.print('0');
            continue;
        }
        for (uint8_t key = 0; key < count; ++key) {
            if (key > 0) Serial.print('+');
            Serial.print(mapping.keys[key]);
        }
    }
    Serial.println();
}

void printInputs() {
    Serial.print("INPUTS:");
    for (uint8_t i = 0; i < NUM_INPUTS; ++i) {
        if (i > 0) Serial.print('|');
        Serial.print(i);
        Serial.print(':');
        Serial.print(inputDescriptors[i].kind);
        Serial.print(':');
        Serial.print(inputDescriptors[i].layerEligible ? 1 : 0);
        Serial.print(':');
        Serial.print(inputDescriptors[i].name);
    }
    Serial.println();
}

void printLayerConfig() {
    Serial.print("LAYERCFG:");
    Serial.print(holdMs);
    Serial.print(':');
    bool first = true;
    for (uint8_t slot = 0; slot < tourbox::MAX_LAYERS; ++slot) {
        if (layerTriggers[slot] == tourbox::NO_TRIGGER) continue;
        if (!first) Serial.print(',');
        Serial.print(slot + 1);
        Serial.print('=');
        Serial.print(layerTriggers[slot]);
        first = false;
    }
    Serial.println();
}

bool parseExact(const char* text, int& a, int& b, int& c) {
    char trailing;
    return sscanf(text, "%d:%d:%d%c", &a, &b, &c, &trailing) == 3;
}

bool parseExact(const char* text, int& a, int& b, int& c, int& d) {
    char trailing;
    return sscanf(text, "%d:%d:%d:%d%c", &a, &b, &c, &d, &trailing) == 4;
}

void setMappingCommand(uint8_t layer, int index,
                       const tourbox::Mapping& mapping) {
    if (layer >= tourbox::MODE_COUNT || index < 0 || index >= NUM_INPUTS ||
        !validMapping(mapping)) {
        Serial.println("ERR:RANGE");
        return;
    }
    if (layer > 0 && layerTriggers[layer - 1] == tourbox::NO_TRIGGER) {
        Serial.println("ERR:DISABLED");
        return;
    }
    if (memcmp(&mappings[layer][index], &mapping, sizeof(mapping)) != 0) {
        mappings[layer][index] = mapping;
        saveConfig();
    }
    reportDirty = true;
    Serial.println("OK");
}

void setSingleMappingCommand(uint8_t layer, int index, int modifier,
                             int keycode) {
    if (modifier < 0 || modifier > 255 || keycode < 0 || keycode > 255) {
        Serial.println("ERR:RANGE");
        return;
    }
    tourbox::Mapping mapping = tourbox::singleKeyMapping(
        static_cast<uint8_t>(modifier), static_cast<uint8_t>(keycode));
    setMappingCommand(layer, index, mapping);
}

bool parseChordKeys(const char* text, tourbox::Mapping& mapping) {
    mapping.keys[0] = mapping.keys[1] = mapping.keys[2] = 0;
    uint8_t count = 0;
    const char* cursor = text;
    while (*cursor != '\0') {
        if (count >= tourbox::MAX_MAPPING_KEYS) return false;
        char* end = nullptr;
        long value = strtol(cursor, &end, 10);
        if (end == cursor || value < 0 || value > 255) return false;
        if (value == 0) return count == 0 && *end == '\0';
        for (uint8_t i = 0; i < count; ++i) {
            if (mapping.keys[i] == value) return false;
        }
        mapping.keys[count++] = static_cast<uint8_t>(value);
        if (*end == '\0') break;
        if (*end != '+') return false;
        cursor = end + 1;
        if (*cursor == '\0') return false;
    }
    if (count == 0) return false;
    return true;
}

void resetConfig() {
    setDefaults();
    layerState.reset();
    saveConfig();
    reportDirty = true;
}

void processCommand(const char* cmd) {
    if (strcmp(cmd, "GET_INFO") == 0) {
        Serial.println("INFO:WirelessTourbox:3");
    } else if (strcmp(cmd, "GET_CAPS") == 0) {
        Serial.print("CAPS:3:");
        Serial.print(NUM_INPUTS);
        Serial.print(':');
        Serial.print(tourbox::MAX_LAYERS);
        Serial.print(':');
        Serial.println(tourbox::MAX_MAPPING_KEYS);
    } else if (strcmp(cmd, "GET_INPUTS") == 0) {
        printInputs();
    } else if (strcmp(cmd, "GET_LAYER_CONFIG") == 0) {
        printLayerConfig();
    } else if (strcmp(cmd, "GET_LAYOUT") == 0) {
        printLayout(0);
    } else if (strncmp(cmd, "GET_LAYOUT:", 11) == 0) {
        int layer;
        char trailing;
        if (sscanf(cmd + 11, "%d%c", &layer, &trailing) != 1) Serial.println("ERR:PARSE");
        else if (layer < 0 || layer >= tourbox::MODE_COUNT) Serial.println("ERR:RANGE");
        else if (layer > 0 && layerTriggers[layer - 1] == tourbox::NO_TRIGGER) Serial.println("ERR:DISABLED");
        else printLayout(layer);
    } else if (strncmp(cmd, "GET_CHORDS:", 11) == 0) {
        int layer;
        char trailing;
        if (sscanf(cmd + 11, "%d%c", &layer, &trailing) != 1) Serial.println("ERR:PARSE");
        else if (layer < 0 || layer >= tourbox::MODE_COUNT) Serial.println("ERR:RANGE");
        else if (layer > 0 && layerTriggers[layer - 1] == tourbox::NO_TRIGGER) Serial.println("ERR:DISABLED");
        else printChords(layer);
    } else if (strcmp(cmd, "RESET_DEFAULTS") == 0) {
        resetConfig();
        Serial.println("OK");
    } else if (strncmp(cmd, "SET_KEY:", 8) == 0) {
        int layer, index, modifier, keycode;
        if (parseExact(cmd + 8, layer, index, modifier, keycode)) {
            setSingleMappingCommand(layer, index, modifier, keycode);
        } else if (parseExact(cmd + 8, index, modifier, keycode)) {
            setSingleMappingCommand(0, index, modifier, keycode);
        } else {
            Serial.println("ERR:PARSE");
        }
    } else if (strncmp(cmd, "SET_CHORD:", 10) == 0) {
        int layer, index, modifier;
        char keys[24];
        if (sscanf(cmd + 10, "%d:%d:%d:%23s", &layer, &index, &modifier, keys) != 4) {
            Serial.println("ERR:PARSE");
        } else if (modifier < 0 || modifier > 255) {
            Serial.println("ERR:RANGE");
        } else {
            tourbox::Mapping mapping = {static_cast<uint8_t>(modifier), {0, 0, 0}};
            if (!parseChordKeys(keys, mapping)) Serial.println("ERR:PARSE");
            else {
                if (mapping.keys[0] == 0) mapping.modifier = 0;
                setMappingCommand(layer, index, mapping);
            }
        }
    } else if (strncmp(cmd, "SET_LAYER:", 10) == 0) {
        int layer, trigger;
        char trailing;
        if (sscanf(cmd + 10, "%d:%d%c", &layer, &trigger, &trailing) != 2) Serial.println("ERR:PARSE");
        else if (layer < 1 || layer > tourbox::MAX_LAYERS || trigger < 0 || trigger >= NUM_INPUTS) Serial.println("ERR:RANGE");
        else if (!validTrigger(trigger)) Serial.println("ERR:INELIGIBLE");
        else if (duplicateTrigger(layer - 1, trigger)) Serial.println("ERR:DUPLICATE");
        else {
            uint8_t slot = static_cast<uint8_t>(layer - 1);
            uint8_t nextTrigger = static_cast<uint8_t>(trigger);
            bool wasUnused = layerTriggers[slot] == tourbox::NO_TRIGGER;
            if (tourbox::updateConfigValue(layerTriggers[slot], nextTrigger)) {
                if (layerState.clearLayer(static_cast<uint8_t>(layer))) {
                    logLayer(static_cast<uint8_t>(layer), false);
                }
                if (wasUnused) {
                    memset(mappings[layer], 0, sizeof(mappings[layer]));
                }
                saveConfig();
            }
            Serial.println("OK");
        }
    } else if (strncmp(cmd, "REMOVE_LAYER:", 13) == 0) {
        int layer;
        char trailing;
        if (sscanf(cmd + 13, "%d%c", &layer, &trailing) != 1) Serial.println("ERR:PARSE");
        else if (layer < 1 || layer > tourbox::MAX_LAYERS) Serial.println("ERR:RANGE");
        else {
            uint8_t slot = static_cast<uint8_t>(layer - 1);
            if (tourbox::updateConfigValue(layerTriggers[slot],
                                           tourbox::NO_TRIGGER)) {
                if (layerState.clearLayer(static_cast<uint8_t>(layer))) {
                    logLayer(static_cast<uint8_t>(layer), false);
                }
                memset(mappings[layer], 0, sizeof(mappings[layer]));
                saveConfig();
            }
            Serial.println("OK");
        }
    } else if (strncmp(cmd, "SET_HOLD_MS:", 12) == 0) {
        int value;
        char trailing;
        if (sscanf(cmd + 12, "%d%c", &value, &trailing) != 1) Serial.println("ERR:PARSE");
        else if (value < MIN_HOLD_MS || value > MAX_HOLD_MS) Serial.println("ERR:RANGE");
        else {
            uint16_t nextHoldMs = static_cast<uint16_t>(value);
            if (tourbox::updateConfigValue(holdMs, nextHoldMs)) {
                saveConfig();
            }
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
            if (cmdOverflow) Serial.println("ERR:TOO_LONG");
            else if (cmdLen > 0) {
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
        switchActive[i] = false;
        activeMappings[i] = {0, {0, 0, 0}};
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
