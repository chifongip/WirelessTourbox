#pragma once

#include <stddef.h>
#include <stdint.h>

namespace tourbox {

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

inline void addMapping(KeyboardReport& report, const uint8_t mapping[2]) {
    report.modifiers |= mapping[0];
    uint8_t keycode = mapping[1];
    if (keycode != 0 && !containsKey(report, keycode) && report.count < 6) {
        report.keys[report.count++] = keycode;
    }
}

inline KeyboardReport composeReport(const uint8_t config[][2],
                                    const bool active[], uint8_t activeCount,
                                    int8_t pulseIndex) {
    KeyboardReport report = {0, {0, 0, 0, 0, 0, 0}, 0};
    for (uint8_t i = 0; i < activeCount; ++i) {
        if (active[i]) addMapping(report, config[i]);
    }
    if (pulseIndex >= 0) addMapping(report, config[pulseIndex]);
    return report;
}

}  // namespace tourbox
