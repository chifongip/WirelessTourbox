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
    const uint8_t mappings[4][2] = {{0x01, 0x04}, {0x02, 0x05}, {0x04, 0x04}, {0, 0x06}};
    const bool active[3] = {true, true, true};
    tourbox::KeyboardReport report = tourbox::composeReport(mappings, active, 3, 3);
    TEST_ASSERT_EQUAL_HEX8(0x07, report.modifiers);
    TEST_ASSERT_EQUAL_UINT8(3, report.count);
    TEST_ASSERT_EQUAL_HEX8(0x04, report.keys[0]);
    TEST_ASSERT_EQUAL_HEX8(0x05, report.keys[1]);
    TEST_ASSERT_EQUAL_HEX8(0x06, report.keys[2]);
}

void test_report_enforces_six_unique_keys() {
    const uint8_t mappings[7][2] = {
        {0, 4}, {0, 5}, {0, 6}, {0, 7}, {0, 8}, {0, 9}, {0, 10},
    };
    const bool active[6] = {true, true, true, true, true, true};
    tourbox::KeyboardReport report = tourbox::composeReport(mappings, active, 6, 6);
    TEST_ASSERT_EQUAL_UINT8(6, report.count);
    TEST_ASSERT_FALSE(tourbox::containsKey(report, 10));
}

int main(int, char**) {
    UNITY_BEGIN();
    RUN_TEST(test_debounce_requires_stable_interval);
    RUN_TEST(test_report_combines_modifiers_and_keys);
    RUN_TEST(test_report_enforces_six_unique_keys);
    return UNITY_END();
}
