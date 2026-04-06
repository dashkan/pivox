#include "pch.h"
#include "App.xaml.h"
#include "MainWindow.xaml.h"
#include "firebase_config.h"
#include <winrt/Microsoft.Security.Authentication.OAuth.h>

// NOTE: App.xaml.cpp does NOT include App.g.cpp.
// See docs/dev/winui3-cmake-guide.md constraint #3.

namespace winrt::Pivox::implementation
{
    std::shared_ptr<pivox::WinAppState> App::s_appState =
        std::make_shared<pivox::WinAppState>();
    std::shared_ptr<pivox::WinAuthService> App::s_authService =
        std::make_shared<pivox::WinAuthService>();

    App::App()
    {
#if defined _DEBUG && !defined DISABLE_XAML_GENERATED_BREAK_ON_UNHANDLED_EXCEPTION
        UnhandledException([this](IInspectable const&, Microsoft::UI::Xaml::UnhandledExceptionEventArgs const& e)
        {
            if (IsDebuggerPresent())
            {
                auto errorMessage = e.Message();
                __debugbreak();
            }
        });
#endif

        // Single-instance: if another Pivox is running, handle OAuth callback
        // forwarding and exit. OAuth2Manager::CompleteAuthRequest sends the
        // auth code to the first instance's pending RequestAuthWithParamsAsync.
        HANDLE hMutex = CreateMutexW(nullptr, FALSE, L"Local\\PivoxAppMutex");
        if (hMutex && GetLastError() == ERROR_ALREADY_EXISTS)
        {
            // Check if we were launched with a protocol activation URL.
            int argc = 0;
            LPWSTR* argv = CommandLineToArgvW(GetCommandLineW(), &argc);
            if (argv && argc >= 2)
            {
                try {
                    winrt::Windows::Foundation::Uri uri(argv[1]);
                    winrt::Microsoft::Security::Authentication::OAuth::OAuth2Manager::CompleteAuthRequest(uri);
                } catch (...) {}
            }
            if (argv) LocalFree(argv);
            CloseHandle(hMutex);
            ExitProcess(0);
            return;
        }

        // Register pivox:// URL scheme (for non-Google callbacks).
        pivox::WinAppState::registerProtocolHandler();

        // Initialize Firebase C++ SDK.
        s_authService->initializeFirebase();

        // Connect to Firebase Auth Emulator if requested.
        s_authService->connectToEmulatorIfRequested();

        // Configure OAuth providers.
        pivox::OAuthConfig oauthConfig;
        oauthConfig.googleClientId = pivox::firebase_config::kGoogleSignInClientId;
        s_authService->setOAuthConfig(oauthConfig);
    }

    void App::OnLaunched([[maybe_unused]] Microsoft::UI::Xaml::LaunchActivatedEventArgs const& e)
    {
        // RESET_AUTH=1: sign out and clear auth tokens (not preferences).
        wchar_t resetAuth[2] = {};
        if (GetEnvironmentVariableW(L"RESET_AUTH", resetAuth, 2) > 0 && resetAuth[0] == L'1')
        {
            s_authService->signOut();
        }

        // RESET_PREFS=1: clear non-auth preferences (remembered email).
        wchar_t resetPrefs[2] = {};
        if (GetEnvironmentVariableW(L"RESET_PREFS", resetPrefs, 2) > 0 && resetPrefs[0] == L'1')
        {
            s_appState->saveString("remembered_email", "");
        }

        m_window = winrt::make<MainWindow>();
        m_window.Activate();
    }

    std::shared_ptr<pivox::WinAppState>& App::AppState()
    {
        return s_appState;
    }

    std::shared_ptr<pivox::WinAuthService>& App::AuthService()
    {
        return s_authService;
    }
}
