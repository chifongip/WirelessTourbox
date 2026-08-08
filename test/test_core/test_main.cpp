#include <TourboxCore.h>
#include <unity.h>

void test_debounce_requires_stable_interval() {
    tourbox::DebounceState state = {true, true, 0};
    TEST_ASSERT_FALSE(tourbox::updateDebounce(state, false, 10, 5));
    TEST_ASSERT_FALSE(tourbox::updateDebounce(state, true, 12, 5));
    TEST_ASSERT_FALSE(tourbox::updateDebounce(state, false, 14, 5));
    TEST_ASSERT_FALSE(tourbox::updateDebounce(state, false, 18, 5));
    TEST_ASSERT_TRUE(tourbox::updateDebounce(state, false, 19, 5));
    TEST_ASSERT_FALSE(state.stable);
}

void test_report_combines_modifiers_and_keys() {
    const tourbox::Mapping mappings[3] = {
        {0x01, {0x04, 0, 0}}, {0x02, {0x05, 0, 0}},
        {0x04, {0x04, 0, 0}},
    };
    const bool active[3] = {true, true, true};
    const tourbox::Mapping pulse = {0, {0x06, 0, 0}};
    tourbox::KeyboardReport report = tourbox::composeReport(mappings, active, 3, &pulse);
    TEST_ASSERT_EQUAL_HEX8(0x07, report.modifiers);
    TEST_ASSERT_EQUAL_UINT8(3, report.count);
    TEST_ASSERT_EQUAL_HEX8(0x04, report.keys[0]);
    TEST_ASSERT_EQUAL_HEX8(0x05, report.keys[1]);
    TEST_ASSERT_EQUAL_HEX8(0x06, report.keys[2]);
}

void test_report_enforces_six_unique_keys() {
    const tourbox::Mapping mappings[6] = {
        {0, {4, 0, 0}}, {0, {5, 0, 0}}, {0, {6, 0, 0}},
        {0, {7, 0, 0}}, {0, {8, 0, 0}}, {0, {9, 0, 0}},
    };
    const bool active[6] = {true, true, true, true, true, true};
    const tourbox::Mapping pulse = {0, {10, 0, 0}};
    tourbox::KeyboardReport report = tourbox::composeReport(mappings, active, 6, &pulse);
    TEST_ASSERT_EQUAL_UINT8(6, report.count);
    TEST_ASSERT_FALSE(tourbox::containsKey(report, 10));
}

void test_compound_mapping_is_simultaneous() {
    const tourbox::Mapping mappings[1] = {{0x05, {0x04, 0x05, 0x06}}};
    const bool active[1] = {true};
    tourbox::KeyboardReport report =
        tourbox::composeReport(mappings, active, 1, nullptr);
    TEST_ASSERT_EQUAL_HEX8(0x05, report.modifiers);
    TEST_ASSERT_EQUAL_UINT8(3, report.count);
    TEST_ASSERT_EQUAL_HEX8(0x04, report.keys[0]);
    TEST_ASSERT_EQUAL_HEX8(0x05, report.keys[1]);
    TEST_ASSERT_EQUAL_HEX8(0x06, report.keys[2]);
}

void test_compound_mapping_is_never_partial() {
    const tourbox::Mapping mappings[2] = {
        {0, {4, 5, 6}}, {0x01, {7, 8, 9}},
    };
    const bool active[2] = {true, true};
    const tourbox::Mapping pulse = {0x04, {10, 11, 0}};
    tourbox::KeyboardReport report =
        tourbox::composeReport(mappings, active, 2, &pulse);
    TEST_ASSERT_EQUAL_UINT8(6, report.count);
    TEST_ASSERT_EQUAL_HEX8(0x01, report.modifiers);
    TEST_ASSERT_FALSE(tourbox::containsKey(report, 10));
    TEST_ASSERT_FALSE(tourbox::containsKey(report, 11));
    TEST_ASSERT_EQUAL_HEX8(0x00, report.modifiers & 0x04);
}

void test_compound_mapping_reuses_existing_keys() {
    tourbox::KeyboardReport report = {0, {4, 5, 6, 7, 8, 0}, 5};
    const tourbox::Mapping mapping = {0x02, {4, 9, 0}};
    TEST_ASSERT_TRUE(tourbox::canAddMapping(report, mapping));
    TEST_ASSERT_TRUE(tourbox::addMapping(report, mapping));
    TEST_ASSERT_EQUAL_UINT8(6, report.count);
    TEST_ASSERT_EQUAL_HEX8(0x02, report.modifiers);
    TEST_ASSERT_EQUAL_HEX8(9, report.keys[5]);
}

void test_layer_quick_release_is_tap() {
    tourbox::LayerState state;
    TEST_ASSERT_TRUE(state.beginTrigger(0, 1, 100));
    TEST_ASSERT_FALSE(state.update(299, 200));
    TEST_ASSERT_EQUAL_UINT8(static_cast<uint8_t>(tourbox::LayerRelease::Tap),
                            static_cast<uint8_t>(state.release(0)));
    TEST_ASSERT_EQUAL_UINT8(0, state.currentLayer());
}

void test_layer_activates_after_hold_and_deactivates_on_release() {
    tourbox::LayerState state;
    TEST_ASSERT_TRUE(state.beginTrigger(1, 3, 100));
    TEST_ASSERT_TRUE(state.update(300, 200));
    TEST_ASSERT_EQUAL_UINT8(3, state.currentLayer());
    TEST_ASSERT_EQUAL_UINT8(static_cast<uint8_t>(tourbox::LayerRelease::Deactivated),
                            static_cast<uint8_t>(state.release(1)));
    TEST_ASSERT_EQUAL_UINT8(0, state.currentLayer());
}

void test_other_input_promotes_pending_and_first_trigger_wins() {
    tourbox::LayerState state;
    TEST_ASSERT_TRUE(state.beginTrigger(0, 1, 0));
    TEST_ASSERT_FALSE(state.beginTrigger(1, 2, 1));
    TEST_ASSERT_TRUE(state.promoteForActivity(2));
    TEST_ASSERT_EQUAL_UINT8(1, state.currentLayer());
    TEST_ASSERT_FALSE(state.promoteForActivity(3));
}

void test_crc_detects_changes() {
    uint8_t data[] = {1, 2, 3, 4, 5};
    uint16_t original = tourbox::crc16(data, sizeof(data));
    data[2] ^= 0x40;
    TEST_ASSERT_NOT_EQUAL(original, tourbox::crc16(data, sizeof(data)));
}

void test_v2_mappings_expand_without_data_loss() {
    const uint8_t stored[] = {0x05, 0x04, 0x00, 0x00, 0x02, 0x4C};
    tourbox::Mapping mappings[3];
    tourbox::expandSingleKeyMappings(stored, mappings, 3);
    TEST_ASSERT_EQUAL_HEX8(0x05, mappings[0].modifier);
    TEST_ASSERT_EQUAL_HEX8(0x04, mappings[0].keys[0]);
    TEST_ASSERT_EQUAL_UINT8(1, tourbox::mappingKeyCount(mappings[0]));
    TEST_ASSERT_EQUAL_HEX8(0x00, mappings[1].modifier);
    TEST_ASSERT_EQUAL_UINT8(0, tourbox::mappingKeyCount(mappings[1]));
    TEST_ASSERT_EQUAL_HEX8(0x02, mappings[2].modifier);
    TEST_ASSERT_EQUAL_HEX8(0x4C, mappings[2].keys[0]);
}

int main(int, char**) {
    UNITY_BEGIN();
    RUN_TEST(test_debounce_requires_stable_interval);
    RUN_TEST(test_report_combines_modifiers_and_keys);
    RUN_TEST(test_report_enforces_six_unique_keys);
    RUN_TEST(test_compound_mapping_is_simultaneous);
    RUN_TEST(test_compound_mapping_is_never_partial);
    RUN_TEST(test_compound_mapping_reuses_existing_keys);
    RUN_TEST(test_layer_quick_release_is_tap);
    RUN_TEST(test_layer_activates_after_hold_and_deactivates_on_release);
    RUN_TEST(test_other_input_promotes_pending_and_first_trigger_wins);
    RUN_TEST(test_crc_detects_changes);
    RUN_TEST(test_v2_mappings_expand_without_data_loss);
    return UNITY_END();
}
