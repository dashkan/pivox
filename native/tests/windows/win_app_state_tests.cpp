#include <gtest/gtest.h>
#include "WinAppState.h"

// Integration tests for WinAppState — exercises real Windows APIs
// (Registry for general state, PasswordVault for secure storage).
// Each test cleans up after itself to avoid polluting the system.

static const std::string kTestPrefix = "pivox_test_";

class WinAppStateTest : public ::testing::Test {
protected:
    pivox::WinAppState state;

    void TearDown() override {
        // Clean up test keys from registry.
        state.saveString(kTestPrefix + "str", "");
        state.saveBool(kTestPrefix + "bool", false);

        // Clean up secure storage.
        state.deleteSecure(kTestPrefix + "token");
        state.deleteSecure(kTestPrefix + "overwrite");
    }
};

// ---------------------------------------------------------------------------
// Window state round-trip via Registry
// ---------------------------------------------------------------------------

TEST_F(WinAppStateTest, WindowState_SaveAndLoadRoundTrip) {
    pivox::WindowState ws{150, 250, 1400, 900};
    state.saveWindowState(ws);

    auto loaded = state.loadWindowState();
    ASSERT_TRUE(loaded.has_value());
    EXPECT_EQ(loaded->x, 150);
    EXPECT_EQ(loaded->y, 250);
    EXPECT_EQ(loaded->width, 1400);
    EXPECT_EQ(loaded->height, 900);
}

TEST_F(WinAppStateTest, WindowState_OverwritesPrevious) {
    state.saveWindowState({0, 0, 800, 600});
    state.saveWindowState({100, 100, 1920, 1080});

    auto loaded = state.loadWindowState();
    ASSERT_TRUE(loaded.has_value());
    EXPECT_EQ(loaded->x, 100);
    EXPECT_EQ(loaded->width, 1920);
}

TEST_F(WinAppStateTest, WindowState_RejectsAbsurdSizes) {
    state.saveWindowState({0, 0, 50, 50});

    auto loaded = state.loadWindowState();
    EXPECT_FALSE(loaded.has_value());
}

// ---------------------------------------------------------------------------
// String storage via Registry
// ---------------------------------------------------------------------------

TEST_F(WinAppStateTest, String_SaveAndLoad) {
    state.saveString(kTestPrefix + "str", "Designer");

    auto loaded = state.loadString(kTestPrefix + "str");
    ASSERT_TRUE(loaded.has_value());
    EXPECT_EQ(loaded.value(), "Designer");
}

TEST_F(WinAppStateTest, String_NonexistentReturnsNullopt) {
    EXPECT_FALSE(state.loadString("pivox_test_nonexistent_key_xyz").has_value());
}

// ---------------------------------------------------------------------------
// Bool storage via Registry (rememberMe)
// ---------------------------------------------------------------------------

TEST_F(WinAppStateTest, Bool_SaveTrueAndLoad) {
    state.saveBool(kTestPrefix + "bool", true);

    auto loaded = state.loadBool(kTestPrefix + "bool");
    ASSERT_TRUE(loaded.has_value());
    EXPECT_TRUE(loaded.value());
}

TEST_F(WinAppStateTest, Bool_SaveFalseAndLoad) {
    state.saveBool(kTestPrefix + "bool", false);

    auto loaded = state.loadBool(kTestPrefix + "bool");
    ASSERT_TRUE(loaded.has_value());
    EXPECT_FALSE(loaded.value());
}

TEST_F(WinAppStateTest, Bool_NonexistentReturnsNullopt) {
    EXPECT_FALSE(state.loadBool("pivox_test_nonexistent_bool_xyz").has_value());
}

// ---------------------------------------------------------------------------
// Secure storage via PasswordVault
// ---------------------------------------------------------------------------

TEST_F(WinAppStateTest, Secure_SaveAndLoadRoundTrip) {
    state.saveSecure(kTestPrefix + "token", "eyJhbGciOiJIUzI1NiJ9.test");

    auto loaded = state.loadSecure(kTestPrefix + "token");
    ASSERT_TRUE(loaded.has_value());
    EXPECT_EQ(loaded.value(), "eyJhbGciOiJIUzI1NiJ9.test");
}

TEST_F(WinAppStateTest, Secure_DeleteRemovesCredential) {
    state.saveSecure(kTestPrefix + "token", "secret");
    state.deleteSecure(kTestPrefix + "token");

    EXPECT_FALSE(state.loadSecure(kTestPrefix + "token").has_value());
}

TEST_F(WinAppStateTest, Secure_OverwritesPrevious) {
    state.saveSecure(kTestPrefix + "overwrite", "old-token");
    state.saveSecure(kTestPrefix + "overwrite", "new-token");

    EXPECT_EQ(state.loadSecure(kTestPrefix + "overwrite").value(), "new-token");
}

TEST_F(WinAppStateTest, Secure_NonexistentReturnsNullopt) {
    EXPECT_FALSE(state.loadSecure("pivox_test_nonexistent_secure_xyz").has_value());
}

TEST_F(WinAppStateTest, Secure_DeleteNonexistentIsNoop) {
    state.deleteSecure("pivox_test_nonexistent_delete_xyz");
    // Should not crash.
}
