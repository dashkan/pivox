#pragma once
#include "App.xaml.g.h"
#include "WinAppState.h"
#include "WinAuthService.h"
#include <memory>

namespace winrt::Pivox::implementation
{
    struct App : AppT<App>
    {
        App();

        void OnLaunched(Microsoft::UI::Xaml::LaunchActivatedEventArgs const&);

        static std::shared_ptr<pivox::WinAppState>& AppState();
        static std::shared_ptr<pivox::WinAuthService>& AuthService();

    private:
        winrt::Microsoft::UI::Xaml::Window m_window{ nullptr };
        static std::shared_ptr<pivox::WinAppState> s_appState;
        static std::shared_ptr<pivox::WinAuthService> s_authService;

        void HandleProtocolActivation(
            Microsoft::Windows::AppLifecycle::AppActivationArguments const& args);
    };
}
