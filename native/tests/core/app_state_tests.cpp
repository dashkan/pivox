#include <gtest/gtest.h>
#include "app_state.h"
#include <map>

namespace {

/// In-memory AppState implementation for testing the interface contract.
class MockAppState : public pivox::AppState {
public:
    void saveWindowState(const pivox::WindowState& state) override {
        windowState_ = state;
        hasWindowState_ = true;
    }

    std::optional<pivox::WindowState> loadWindowState() override {
        if (!hasWindowState_) return std::nullopt;
        return windowState_;
    }

    void saveString(const std::string& key, const std::string& value) override {
        strings_[key] = value;
    }

    std::optional<std::string> loadString(const std::string& key) override {
        auto it = strings_.find(key);
        if (it == strings_.end()) return std::nullopt;
        return it->second;
    }

    void saveBool(const std::string& key, bool value) override {
        bools_[key] = value;
    }

    std::optional<bool> loadBool(const std::string& key) override {
        auto it = bools_.find(key);
        if (it == bools_.end()) return std::nullopt;
        return it->second;
    }

    void saveSecure(const std::string& key, const std::string& value) override {
        secure_[key] = value;
    }

    std::optional<std::string> loadSecure(const std::string& key) override {
        auto it = secure_.find(key);
        if (it == secure_.end()) return std::nullopt;
        return it->second;
    }

    void deleteSecure(const std::string& key) override {
        secure_.erase(key);
    }

private:
    pivox::WindowState windowState_;
    bool hasWindowState_ = false;
    std::map<std::string, std::string> strings_;
    std::map<std::string, bool> bools_;
    std::map<std::string, std::string> secure_;
};

} // namespace

// ---------------------------------------------------------------------------
// Window state
// ---------------------------------------------------------------------------

TEST(AppState, WindowState_DefaultsReturnsNullopt) {
    MockAppState state;
    EXPECT_FALSE(state.loadWindowState().has_value());
}

TEST(AppState, WindowState_SaveAndLoad) {
    MockAppState state;
    pivox::WindowState ws{100, 200, 1280, 800};
    state.saveWindowState(ws);

    auto loaded = state.loadWindowState();
    ASSERT_TRUE(loaded.has_value());
    EXPECT_EQ(loaded->x, 100);
    EXPECT_EQ(loaded->y, 200);
    EXPECT_EQ(loaded->width, 1280);
    EXPECT_EQ(loaded->height, 800);
}

TEST(AppState, WindowState_OverwritesPrevious) {
    MockAppState state;
    state.saveWindowState({0, 0, 800, 600});
    state.saveWindowState({50, 50, 1920, 1080});

    auto loaded = state.loadWindowState();
    ASSERT_TRUE(loaded.has_value());
    EXPECT_EQ(loaded->width, 1920);
    EXPECT_EQ(loaded->height, 1080);
}

// ---------------------------------------------------------------------------
// Remember me (bool state)
// ---------------------------------------------------------------------------

TEST(AppState, Bool_DefaultsReturnsNullopt) {
    MockAppState state;
    EXPECT_FALSE(state.loadBool("rememberMe").has_value());
}

TEST(AppState, Bool_SaveTrue) {
    MockAppState state;
    state.saveBool("rememberMe", true);

    auto loaded = state.loadBool("rememberMe");
    ASSERT_TRUE(loaded.has_value());
    EXPECT_TRUE(loaded.value());
}

TEST(AppState, Bool_SaveFalse) {
    MockAppState state;
    state.saveBool("rememberMe", false);

    auto loaded = state.loadBool("rememberMe");
    ASSERT_TRUE(loaded.has_value());
    EXPECT_FALSE(loaded.value());
}

TEST(AppState, Bool_OverwritesTrueWithFalse) {
    MockAppState state;
    state.saveBool("rememberMe", true);
    state.saveBool("rememberMe", false);

    EXPECT_FALSE(state.loadBool("rememberMe").value());
}

TEST(AppState, Bool_IndependentKeys) {
    MockAppState state;
    state.saveBool("rememberMe", true);
    state.saveBool("darkMode", false);

    EXPECT_TRUE(state.loadBool("rememberMe").value());
    EXPECT_FALSE(state.loadBool("darkMode").value());
    EXPECT_FALSE(state.loadBool("nonexistent").has_value());
}

// ---------------------------------------------------------------------------
// String state
// ---------------------------------------------------------------------------

TEST(AppState, String_DefaultsReturnsNullopt) {
    MockAppState state;
    EXPECT_FALSE(state.loadString("lastSection").has_value());
}

TEST(AppState, String_SaveAndLoad) {
    MockAppState state;
    state.saveString("lastSection", "Designer");

    auto loaded = state.loadString("lastSection");
    ASSERT_TRUE(loaded.has_value());
    EXPECT_EQ(loaded.value(), "Designer");
}

// ---------------------------------------------------------------------------
// Secure storage
// ---------------------------------------------------------------------------

TEST(AppState, Secure_DefaultsReturnsNullopt) {
    MockAppState state;
    EXPECT_FALSE(state.loadSecure("authToken").has_value());
}

TEST(AppState, Secure_SaveAndLoad) {
    MockAppState state;
    state.saveSecure("authToken", "eyJhbGciOiJIUzI1NiJ9.test");

    auto loaded = state.loadSecure("authToken");
    ASSERT_TRUE(loaded.has_value());
    EXPECT_EQ(loaded.value(), "eyJhbGciOiJIUzI1NiJ9.test");
}

TEST(AppState, Secure_DeleteRemovesKey) {
    MockAppState state;
    state.saveSecure("authToken", "secret");
    state.deleteSecure("authToken");

    EXPECT_FALSE(state.loadSecure("authToken").has_value());
}

TEST(AppState, Secure_DeleteNonexistentIsNoop) {
    MockAppState state;
    state.deleteSecure("nonexistent"); // should not crash
    EXPECT_FALSE(state.loadSecure("nonexistent").has_value());
}

TEST(AppState, Secure_OverwritesPrevious) {
    MockAppState state;
    state.saveSecure("refreshToken", "old-token");
    state.saveSecure("refreshToken", "new-token");

    EXPECT_EQ(state.loadSecure("refreshToken").value(), "new-token");
}

// ---------------------------------------------------------------------------
// Default WindowState values
// ---------------------------------------------------------------------------

TEST(AppState, WindowState_DefaultValues) {
    pivox::WindowState ws;
    EXPECT_EQ(ws.x, 0);
    EXPECT_EQ(ws.y, 0);
    EXPECT_EQ(ws.width, 1280);
    EXPECT_EQ(ws.height, 800);
}
