#pragma once

#include <stddef.h>
#include <stdint.h>

namespace tourbox {

constexpr uint8_t MAX_INPUTS = 32;
constexpr uint8_t MAX_LAYERS = 15;
constexpr uint8_t MODE_COUNT = MAX_LAYERS + 1;
constexpr uint8_t NO_TRIGGER = 0xFF;

struct Mapping {
    uint8_t modifier;
    uint8_t keycode;
};

struct DebounceState {
    bool raw;
    bool stable;
    uint32_t changedAt;
};

inline bool updateDebounce(DebounceState& state, bool input, uint32_t now,
                           uint32_t interval) {
    if (input != state.raw) {
        state.raw = input;
        state.changedAt = now;
    }
    if (input != state.stable && now - state.changedAt >= interval) {
        state.stable = input;
        return true;
    }
    return false;
}

struct KeyboardReport {
    uint8_t modifiers;
    uint8_t keys[6];
    uint8_t count;
};

inline bool containsKey(const KeyboardReport& report, uint8_t keycode) {
    for (uint8_t i = 0; i < report.count; ++i) {
        if (report.keys[i] == keycode) return true;
    }
    return false;
}

inline void addMapping(KeyboardReport& report, const Mapping& mapping) {
    report.modifiers |= mapping.modifier;
    if (mapping.keycode != 0 && !containsKey(report, mapping.keycode) &&
        report.count < 6) {
        report.keys[report.count++] = mapping.keycode;
    }
}

inline KeyboardReport composeReport(const Mapping activeMappings[],
                                    const bool active[], uint8_t activeCount,
                                    const Mapping* pulseMapping) {
    KeyboardReport report = {0, {0, 0, 0, 0, 0, 0}, 0};
    for (uint8_t i = 0; i < activeCount; ++i) {
        if (active[i]) addMapping(report, activeMappings[i]);
    }
    if (pulseMapping != nullptr) addMapping(report, *pulseMapping);
    return report;
}

enum class LayerRelease : uint8_t { None, Tap, Deactivated };

class LayerState {
public:
    bool beginTrigger(uint8_t input, uint8_t layer, uint32_t now) {
        if (activeLayer_ != 0 || pendingInput_ >= 0 || layer == 0) return false;
        pendingInput_ = static_cast<int8_t>(input);
        pendingLayer_ = layer;
        pendingSince_ = now;
        return true;
    }

    bool promoteForActivity(uint8_t input) {
        if (pendingInput_ < 0 || input == static_cast<uint8_t>(pendingInput_)) {
            return false;
        }
        activatePending();
        return true;
    }

    bool update(uint32_t now, uint16_t holdMs) {
        if (pendingInput_ < 0 || now - pendingSince_ < holdMs) return false;
        activatePending();
        return true;
    }

    LayerRelease release(uint8_t input) {
        if (pendingInput_ == static_cast<int8_t>(input)) {
            pendingInput_ = -1;
            pendingLayer_ = 0;
            return LayerRelease::Tap;
        }
        if (activeTrigger_ == static_cast<int8_t>(input)) {
            activeTrigger_ = -1;
            activeLayer_ = 0;
            return LayerRelease::Deactivated;
        }
        return LayerRelease::None;
    }

    uint8_t currentLayer() const { return activeLayer_; }
    uint8_t pendingLayer() const { return pendingLayer_; }
    int8_t pendingInput() const { return pendingInput_; }
    int8_t activeTrigger() const { return activeTrigger_; }
    bool hasPending() const { return pendingInput_ >= 0; }

    void reset() {
        pendingInput_ = -1;
        pendingLayer_ = 0;
        pendingSince_ = 0;
        activeTrigger_ = -1;
        activeLayer_ = 0;
    }

private:
    void activatePending() {
        activeLayer_ = pendingLayer_;
        activeTrigger_ = pendingInput_;
        pendingInput_ = -1;
        pendingLayer_ = 0;
    }

    int8_t pendingInput_ = -1;
    uint8_t pendingLayer_ = 0;
    uint32_t pendingSince_ = 0;
    int8_t activeTrigger_ = -1;
    uint8_t activeLayer_ = 0;
};

inline uint16_t crc16(const uint8_t* data, size_t length) {
    uint16_t crc = 0xFFFF;
    for (size_t i = 0; i < length; ++i) {
        crc ^= static_cast<uint16_t>(data[i]) << 8;
        for (uint8_t bit = 0; bit < 8; ++bit) {
            crc = (crc & 0x8000) ? static_cast<uint16_t>((crc << 1) ^ 0x1021)
                                 : static_cast<uint16_t>(crc << 1);
        }
    }
    return crc;
}

}  // namespace tourbox
