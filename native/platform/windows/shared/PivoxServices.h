#pragma once

#include "WinAppState.h"
#include "WinAuthService.h"
#include <memory>


namespace pivox {

/// Service locator for shared Windows platform services.
/// The app initializes this during startup; shared pages consume it.
// C4251: std::shared_ptr members don't need DLL interface — only accessed
// through exported methods, never across the DLL boundary directly.
#pragma warning(push)
#pragma warning(disable: 4251)
class PivoxServices {
public:
    static void initialize(std::shared_ptr<WinAppState> appState,
                           std::shared_ptr<WinAuthService> authService);

    static std::shared_ptr<WinAppState>& appState();
    static std::shared_ptr<WinAuthService>& authService();

private:
    static std::shared_ptr<WinAppState> s_appState;
    static std::shared_ptr<WinAuthService> s_authService;
};
#pragma warning(pop)

} // namespace pivox
