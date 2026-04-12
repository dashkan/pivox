#include "pch.h"
#include "App.xaml.h"
#include "MainWindow.xaml.h"
#include "PivoxServices.h"
#include "WinAuthService.h"
#include "OAuthPopup.h"
#include "DelegatedAuthClient.h"
#include "DeepLink.h"
#include "firebase_config.h"
#include <winrt/Microsoft.Security.Authentication.OAuth.h>
#include <thread>

// NOTE: App.xaml.cpp does NOT include App.g.cpp.
// See docs/dev/winui3-cmake-guide.md constraint #3.

namespace winrt::Pivox::implementation
{
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

        auto deepLink = ParseDeepLink();

        // --- Single-instance enforcement ---
        // Delegated auth deep links (pivox://auth/delegate/*) skip the mutex.
        // They run as an isolated instance with their own Firebase app name
        // (the session code), so they don't interfere with the main app's
        // Firebase state or persistence.
        //
        // Normal launches enforce single-instance. If another instance is
        // already running, forward the OAuth callback and exit.
        if (!deepLink.isDelegatedAuth) {
            HANDLE hMutex = CreateMutexW(nullptr, FALSE, L"Local\\PivoxMutex");
            if (hMutex && GetLastError() == ERROR_ALREADY_EXISTS)
            {
                // Forward Google OAuth redirect to the first instance.
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
        }

        // Register pivox:// URL scheme (for OAuth and delegate callbacks).
        pivox::WinAppState::registerProtocolHandler();

        // Initialize shared services.
        auto appState = std::make_shared<pivox::WinAppState>();
        auto authService = std::make_shared<pivox::WinAuthService>();
        pivox::PivoxServices::initialize(appState, authService);

        // Firebase initialization is deferred to OnLaunched where we know
        // the session code (needed for the isolated Firebase app name).
        m_isDelegatedAuth = deepLink.isDelegatedAuth;
    }

    void App::OnLaunched([[maybe_unused]] Microsoft::UI::Xaml::LaunchActivatedEventArgs const& e)
    {
        auto deepLink = ParseDeepLink();

        // --- Initialize Firebase ---
        // Delegated auth: use the session code as the Firebase app name so this
        // instance gets its own persistence file, completely isolated from the
        // main app and other delegate instances.
        auto authService = pivox::PivoxServices::authService();
        if (m_isDelegatedAuth && !deepLink.sessionCode.empty()) {
            OutputDebugStringA(("[PivoxApp] Initializing Firebase with isolated name: pivox-delegate-"
                + deepLink.sessionCode + "\n").c_str());
            authService->initializeFirebase("pivox-delegate-" + deepLink.sessionCode);
        } else {
            authService->initializeFirebase();
        }
        authService->connectToEmulatorIfRequested();

        // Configure OAuth providers (needed for Google sign-in popup).
        pivox::OAuthConfig oauthConfig;
        oauthConfig.googleClientId = pivox::firebase_config::kGoogleSignInClientId;
        authService->setOAuthConfig(oauthConfig);

        // Dev overrides.
        wchar_t resetAuth[2] = {};
        if (GetEnvironmentVariableW(L"RESET_AUTH", resetAuth, 2) > 0 && resetAuth[0] == L'1')
        {
            authService->signOut();
        }
        wchar_t resetPrefs[2] = {};
        if (GetEnvironmentVariableW(L"RESET_PREFS", resetPrefs, 2) > 0 && resetPrefs[0] == L'1')
        {
            pivox::PivoxServices::appState()->saveString("remembered_email", "");
        }

        m_delegatedSessionCode = deepLink.sessionCode;

        // --- Dispatch deep link actions ---

        if (deepLink.action == "signout") {
            OutputDebugStringA("[PivoxApp] Deep link: signout\n");
            authService->signOut();
            ExitProcess(0);
            return;
        }

        // Show the main window. For delegated auth, the user authenticates here
        // (email/password, Google, etc.) then the window hides and the session
        // is completed on the backend.
        m_window = winrt::make<MainWindow>();
        m_window.Activate();

        if (deepLink.action == "signin" && !deepLink.sessionCode.empty()) {
            OutputDebugStringA(("[PivoxApp] Delegated auth session: " + deepLink.sessionCode + "\n").c_str());
            WatchForAuthAndComplete();
        } else if (deepLink.action == "profile") {
            OutputDebugStringA("[PivoxApp] Deep link: navigate to profile\n");
            // TODO: Signal MainWindow to navigate to profile section.
        }
    }

    // --- Delegated auth completion ---
    // When launched via pivox://auth/delegate/signin?session=<code>, this method
    // subscribes to the Firebase AuthStateListener. When the user signs in
    // (any method), we:
    //   1. Hide the window (user is going back to the plugin)
    //   2. Get the Firebase ID token
    //   3. Call completeDelegatedAuthSession on the backend
    //   4. Clean up the isolated Firebase instance + heartbeat file
    //   5. Exit
    void App::WatchForAuthAndComplete()
    {
        auto dispatcher = winrt::Microsoft::UI::Dispatching::DispatcherQueue::GetForCurrentThread();
        auto code = m_delegatedSessionCode;
        auto window = m_window;

        pivox::PivoxServices::authService()->onAuthStateChanged(
            [dispatcher, code, window](bool signedIn) {
                if (!signedIn) return;

                dispatcher.TryEnqueue([code, window]() {
                    // Hide immediately — user is going back to the plugin.
                    // Use Hide() not Close() — Close() tears down the dispatcher
                    // queue, which kills in-flight coroutines (CompleteSession).
                    window.AppWindow().Hide();
                    OutputDebugStringA("[PivoxApp] Auth completed, getting ID token\n");

                    pivox::PivoxServices::authService()->getIdTokenAsync(
                        [code](std::string idToken) {
                            if (idToken.empty()) {
                                OutputDebugStringA("[PivoxApp] ERROR: Failed to get ID token\n");
                                ExitProcess(1);
                                return;
                            }

                            OutputDebugStringA("[PivoxApp] Got ID token, completing delegated session\n");
                            pivox::DelegatedAuthClient::CompleteSession(code, idToken,
                                [code](bool ok) {
                                    if (ok) {
                                        OutputDebugStringA("[PivoxApp] Delegated session completed successfully\n");
                                    } else {
                                        OutputDebugStringA("[PivoxApp] ERROR: Failed to complete delegated session\n");
                                    }

                                    // Clean up: sign out the isolated Firebase instance and
                                    // delete its heartbeat file from %LOCALAPPDATA%.
                                    pivox::PivoxServices::authService()->signOut();
                                    auto appName = "pivox-delegate-" + code;
                                    wchar_t localAppData[MAX_PATH];
                                    if (GetEnvironmentVariableW(L"LOCALAPPDATA", localAppData, MAX_PATH)) {
                                        auto heartbeat = std::wstring(localAppData)
                                            + L"\\firebase-heartbeat\\heartbeats-"
                                            + std::wstring(appName.begin(), appName.end());
                                        DeleteFileW(heartbeat.c_str());
                                    }

                                    // Delay exit to let Firebase flush any pending writes.
                                    std::thread([]() {
                                        std::this_thread::sleep_for(std::chrono::seconds(2));
                                        ExitProcess(0);
                                    }).detach();
                                });
                        });
                });
            });
    }

}
