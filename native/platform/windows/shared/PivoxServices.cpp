#include "pch.h"
#include "PivoxServices.h"

namespace pivox {

std::shared_ptr<WinAppState> PivoxServices::s_appState;
std::shared_ptr<WinAuthService> PivoxServices::s_authService;

void PivoxServices::initialize(std::shared_ptr<WinAppState> appState,
                               std::shared_ptr<WinAuthService> authService) {
    s_appState = std::move(appState);
    s_authService = std::move(authService);
}

std::shared_ptr<WinAppState>& PivoxServices::appState() {
    return s_appState;
}

std::shared_ptr<WinAuthService>& PivoxServices::authService() {
    return s_authService;
}

} // namespace pivox
