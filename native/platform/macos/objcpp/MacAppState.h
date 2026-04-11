#pragma once

#include "app_state.h"

namespace pivox {

/// macOS implementation of AppState.
/// Uses NSUserDefaults for general state, Keychain for secure storage.
class MacAppState : public AppState {
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

}  // namespace pivox
