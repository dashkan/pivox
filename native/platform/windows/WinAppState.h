#pragma once

#include "app_state.h"

namespace pivox {

/// Windows implementation of AppState.
/// Uses Windows Registry (HKCU\Software\Pivox) for general state,
/// PasswordVault for secure storage (auth tokens, credentials).
class WinAppState : public AppState {
public:
    void saveWindowState(const WindowState& state) override;
    std::optional<WindowState> loadWindowState() override;

    void saveString(const std::string& key, const std::string& value) override;
    std::optional<std::string> loadString(const std::string& key) override;
    void saveBool(const std::string& key, bool value) override;
    std::optional<bool> loadBool(const std::string& key) override;

    void saveSecure(const std::string& key, const std::string& value) override;
    std::optional<std::string> loadSecure(const std::string& key) override;
    void deleteSecure(const std::string& key) override;
};

} // namespace pivox
