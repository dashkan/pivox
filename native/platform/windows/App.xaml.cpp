#include "pch.h"
#include "App.xaml.h"
#include "MainWindow.xaml.h"
#include "firebase_config.h"

// NOTE: App.xaml.cpp does NOT include App.g.cpp.
// See docs/dev/winui3-cmake-guide.md constraint #3.

namespace winrt::Pivox::implementation
{
    std::shared_ptr<pivox::WinAppState> App::s_appState =
        std::make_shared<pivox::WinAppState>();
    std::shared_ptr<pivox::WinAuthService> App::s_authService =
        std::make_shared<pivox::WinAuthService>(s_appState);

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

        // Single-instance: if another Pivox is already running, redirect
        // this activation to it and exit. This is how OAuth callbacks
        // (protocol activations) reach the running instance.
        auto mainInstance = Microsoft::Windows::AppLifecycle::AppInstance::FindOrRegisterForKey(L"pivox-main");
        if (!mainInstance.IsCurrent())
        {
            // Another instance owns the key — redirect our activation to it.
            auto activationArgs = Microsoft::Windows::AppLifecycle::AppInstance::GetCurrent().GetActivatedEventArgs();
            mainInstance.RedirectActivationToAsync(activationArgs).get();
            ::ExitProcess(0);
            return;
        }

        // We are the main instance. Listen for redirected activations.
        mainInstance.Activated([this](
            IInspectable const&,
            Microsoft::Windows::AppLifecycle::AppActivationArguments const& args)
        {
            HandleProtocolActivation(args);
        });

        // Register pivox:// and Google reversed-client-ID URL schemes.
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

    void App::HandleProtocolActivation(
        Microsoft::Windows::AppLifecycle::AppActivationArguments const& args)
    {
        if (args.Kind() != Microsoft::Windows::AppLifecycle::ExtendedActivationKind::Protocol)
        {
            return;
        }

        auto protocolArgs = args.Data().as<
            winrt::Windows::ApplicationModel::Activation::IProtocolActivatedEventArgs>();
        if (!protocolArgs) return;

        auto uri = winrt::to_string(protocolArgs.Uri().AbsoluteUri());

        // Debug: write to registry so we can verify the activation arrived.
        OutputDebugStringA(("OAuth callback received: " + uri + "\n").c_str());

        s_authService->handleOAuthCallback(uri);
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

        // Check if this launch itself is a protocol activation (first launch with URL).
        auto activationArgs = Microsoft::Windows::AppLifecycle::AppInstance::GetCurrent().GetActivatedEventArgs();
        if (activationArgs)
        {
            HandleProtocolActivation(activationArgs);
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
