#pragma once

#include <string>
#include <optional>

namespace pivox {

struct WindowState {
    int x = 0;
    int y = 0;
    int width = 1280;
    int height = 800;
};

/// Abstract interface for persisting app state across launches.
/// Platform-specific implementations use native storage:
///   macOS: UserDefaults / Keychain
///   Windows: ApplicationData.LocalSettings / Credential Manager
class AppState {
public:
    virtual ~AppState() = default;

    // Window state
    virtual void saveWindowState(const WindowState& state) = 0;
    virtual std::optional<WindowState> loadWindowState() = 0;

    // Generic key-value storage for preferences and UI state.
    virtual void saveString(const std::string& key, const std::string& value) = 0;
    virtual std::optional<std::string> loadString(const std::string& key) = 0;
    virtual void saveBool(const std::string& key, bool value) = 0;
    virtual std::optional<bool> loadBool(const std::string& key) = 0;

    // Secure storage for sensitive data (auth tokens, credentials).
    // Uses Keychain on macOS, Credential Manager on Windows.
    virtual void saveSecure(const std::string& key, const std::string& value) = 0;
    virtual std::optional<std::string> loadSecure(const std::string& key) = 0;
    virtual void deleteSecure(const std::string& key) = 0;
};

} // namespace pivox
