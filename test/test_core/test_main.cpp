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
    const tourbox::Mapping mappings[3] = {{0x01, 0x04}, {0x02, 0x05}, {0x04, 0x04}};
    const bool active[3] = {true, true, true};
    const tourbox::Mapping pulse = {0, 0x06};
    tourbox::KeyboardReport report = tourbox::composeReport(mappings, active, 3, &pulse);
    TEST_ASSERT_EQUAL_HEX8(0x07, report.modifiers);
    TEST_ASSERT_EQUAL_UINT8(3, report.count);
    TEST_ASSERT_EQUAL_HEX8(0x04, report.keys[0]);
    TEST_ASSERT_EQUAL_HEX8(0x05, report.keys[1]);
    TEST_ASSERT_EQUAL_HEX8(0x06, report.keys[2]);
}

void test_report_enforces_six_unique_keys() {
    const tourbox::Mapping mappings[6] = {
        {0, 4}, {0, 5}, {0, 6}, {0, 7}, {0, 8}, {0, 9},
    };
    const bool active[6] = {true, true, true, true, true, true};
    const tourbox::Mapping pulse = {0, 10};
    tourbox::KeyboardReport report = tourbox::composeReport(mappings, active, 6, &pulse);
    TEST_ASSERT_EQUAL_UINT8(6, report.count);
    TEST_ASSERT_FALSE(tourbox::containsKey(report, 10));
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

int main(int, char**) {
    UNITY_BEGIN();
    RUN_TEST(test_debounce_requires_stable_interval);
    RUN_TEST(test_report_combines_modifiers_and_keys);
    RUN_TEST(test_report_enforces_six_unique_keys);
    RUN_TEST(test_layer_quick_release_is_tap);
    RUN_TEST(test_layer_activates_after_hold_and_deactivates_on_release);
    RUN_TEST(test_other_input_promotes_pending_and_first_trigger_wins);
    RUN_TEST(test_crc_detects_changes);
    return UNITY_END();
}
