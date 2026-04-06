#include "pch.h"
#include "App.xaml.h"
#include "MainWindow.xaml.h"
#include "firebase_config.h"
#include <thread>

// NOTE: App.xaml.cpp does NOT include App.g.cpp.
// See docs/dev/winui3-cmake-guide.md constraint #3.

static const wchar_t* kMutexName = L"Local\\PivoxAppMutex";
static const wchar_t* kEventName = L"Local\\PivoxOAuthEvent";
static const wchar_t* kCallbackRegKey = L"Software\\Pivox";
static const wchar_t* kCallbackRegValue = L"oauth_callback_url";

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

        // Single-instance check: if another Pivox is running and we have a
        // protocol activation URL, write it to registry and signal the event.
        HANDLE hMutex = CreateMutexW(nullptr, FALSE, kMutexName);
        if (hMutex && GetLastError() == ERROR_ALREADY_EXISTS)
        {
            // Another instance exists. Check if we were launched with a URL.
            int argc = 0;
            LPWSTR* argv = CommandLineToArgvW(GetCommandLineW(), &argc);
            if (argv && argc >= 2)
            {
                // argv[1] is the callback URL (e.g., com.googleusercontent.apps.xxx:/oauth2callback?...)
                std::wstring url = argv[1];

                // Write URL to registry for the running instance to read.
                HKEY hKey;
                if (RegCreateKeyExW(HKEY_CURRENT_USER, kCallbackRegKey, 0, nullptr,
                        0, KEY_WRITE, nullptr, &hKey, nullptr) == ERROR_SUCCESS) {
                    RegSetValueExW(hKey, kCallbackRegValue, 0, REG_SZ,
                        reinterpret_cast<const BYTE*>(url.c_str()),
                        static_cast<DWORD>((url.size() + 1) * sizeof(wchar_t)));
                    RegCloseKey(hKey);
                }

                // Signal the running instance.
                HANDLE hEvent = OpenEventW(EVENT_MODIFY_STATE, FALSE, kEventName);
                if (hEvent)
                {
                    SetEvent(hEvent);
                    CloseHandle(hEvent);
                }
            }
            if (argv) LocalFree(argv);
            CloseHandle(hMutex);
            ExitProcess(0);
            return;
        }

        // We are the main instance. Keep the mutex open for lifetime.
        // (Leaked intentionally — OS cleans up on process exit.)

        // Create the named event for OAuth callbacks.
        HANDLE hEvent = CreateEventW(nullptr, FALSE, FALSE, kEventName);

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

        // Start background thread to listen for OAuth callbacks from second instances.
        std::thread([hEvent]() {
            while (true) {
                WaitForSingleObject(hEvent, INFINITE);

                // Read the callback URL from registry.
                HKEY hKey;
                if (RegOpenKeyExW(HKEY_CURRENT_USER, kCallbackRegKey, 0, KEY_READ, &hKey) == ERROR_SUCCESS) {
                    DWORD size = 0;
                    DWORD type = 0;
                    RegQueryValueExW(hKey, kCallbackRegValue, nullptr, &type, nullptr, &size);
                    if (type == REG_SZ && size > 0) {
                        std::wstring url(size / sizeof(wchar_t), 0);
                        RegQueryValueExW(hKey, kCallbackRegValue, nullptr, nullptr,
                            reinterpret_cast<BYTE*>(url.data()), &size);
                        RegCloseKey(hKey);

                        // Remove trailing null.
                        if (!url.empty() && url.back() == L'\0') url.pop_back();

                        // Clear the registry value.
                        HKEY hKeyW;
                        if (RegOpenKeyExW(HKEY_CURRENT_USER, kCallbackRegKey, 0, KEY_WRITE, &hKeyW) == ERROR_SUCCESS) {
                            RegDeleteValueW(hKeyW, kCallbackRegValue);
                            RegCloseKey(hKeyW);
                        }

                        // Convert to UTF-8 and process.
                        std::string utf8Url(url.begin(), url.end());
                        s_authService->handleOAuthCallback(utf8Url);
                    } else {
                        RegCloseKey(hKey);
                    }
                }
            }
        }).detach();
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

        // Check if this launch itself has a URL argument (first launch with protocol activation).
        int argc = 0;
        LPWSTR* argv = CommandLineToArgvW(GetCommandLineW(), &argc);
        if (argv && argc >= 2)
        {
            std::wstring url = argv[1];
            std::string utf8Url(url.begin(), url.end());
            s_authService->handleOAuthCallback(utf8Url);
        }
        if (argv) LocalFree(argv);

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
