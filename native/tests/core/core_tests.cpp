#include <gtest/gtest.h>
#include "core.h"

TEST(CoreTests, VersionIsNotEmpty) {
    const char* v = pivox::version();
    ASSERT_NE(v, nullptr);
    EXPECT_STRNE(v, "");
}

TEST(CoreTests, VersionMatchesExpected) {
    EXPECT_STREQ(pivox::version(), "0.1.0");
}
