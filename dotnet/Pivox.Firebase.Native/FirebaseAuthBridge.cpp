#include "pch.h"
#include "FirebaseAuthBridge.h"
#include "FirebaseAuthBridge.g.cpp"
#include "firebase_config.h"

#include <windows.h>

namespace winrt::Pivox::Firebase::Native::implementation
{
    // ── Auth state listener ──────────────────────────────────────

    struct FirebaseAuthBridge::StateListener
        : public ::firebase::auth::AuthStateListener
    {
        FirebaseAuthBridge* owner;
        explicit StateListener(FirebaseAuthBridge* o) : owner(o) {}

        void OnAuthStateChanged(::firebase::auth::Auth*) override
        {
            bool signedIn = owner->IsSignedIn();
            owner->m_authStateChanged(*owner, signedIn);
        }
    };

    // ── Lifecycle ────────────────────────────────────────────────

    FirebaseAuthBridge::~FirebaseAuthBridge()
    {
        if (m_auth && m_listener)
        {
            m_auth->RemoveAuthStateListener(m_listener.get());
        }
        // Auth::GetAuth() returns a cached pointer owned by the App.
        // Don't delete m_auth — the App destructor frees it.
        m_auth = nullptr;
        if (m_app) { delete m_app; m_app = nullptr; }
    }

    bool FirebaseAuthBridge::Initialize()
    {
        if (m_app) return true; // Already initialized.

        ::firebase::AppOptions options;
        options.set_api_key(pivox::firebase_config::kApiKey);
        options.set_project_id(pivox::firebase_config::kProjectId);
        options.set_storage_bucket(pivox::firebase_config::kStorageBucket);
        options.set_messaging_sender_id(pivox::firebase_config::kGcmSenderId);
        options.set_app_id(pivox::firebase_config::kAppId);

        m_app = ::firebase::App::Create(options);
        if (!m_app)
        {
            OutputDebugStringA("[FirebaseAuthBridge] App::Create failed\n");
            return false;
        }

        ::firebase::InitResult initResult;
        m_auth = ::firebase::auth::Auth::GetAuth(m_app, &initResult);
        if (initResult != ::firebase::kInitResultSuccess || !m_auth)
        {
            OutputDebugStringA("[FirebaseAuthBridge] Auth::GetAuth failed\n");
            return false;
        }

        // Wire the state listener — Firebase fires it once on
        // registration with the current state (restores persisted
        // session from Windows credential storage).
        m_listener = std::make_unique<StateListener>(this);
        m_auth->AddAuthStateListener(m_listener.get());

        // Connect to emulator if env var is set.
        wchar_t buf[8]{};
        if (GetEnvironmentVariableW(L"USE_AUTH_EMULATOR", buf, 8) > 0)
        {
            m_auth->UseEmulator("127.0.0.1", 9099);
            OutputDebugStringA("[FirebaseAuthBridge] Connected to auth emulator\n");
        }

        OutputDebugStringA("[FirebaseAuthBridge] Initialized successfully\n");
        return true;
    }

    // ── Helpers ──────────────────────────────────────────────────

    // Bridges firebase::Future<T> to a coroutine-awaitable pattern.
    // Signals a Win32 event from OnCompletion, then co_awaits the
    // signal so the coroutine resumes on completion without polling.
    template <typename T>
    winrt::Windows::Foundation::IAsyncAction
    FirebaseAuthBridge::AwaitFuture(::firebase::Future<T> const& future)
    {
        winrt::handle event{ CreateEvent(nullptr, TRUE, FALSE, nullptr) };
        HANDLE raw = event.get();

        // OnCompletion fires on Firebase's internal thread.
        const_cast<::firebase::Future<T>&>(future).OnCompletion(
            [raw](const ::firebase::Future<T>&) { SetEvent(raw); });

        co_await winrt::resume_on_signal(raw);
    }

    winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
    FirebaseAuthBridge::GetCurrentUserTokenAsync(bool forceRefresh)
    {
        if (!m_auth || !m_auth->current_user().is_valid())
        {
            throw winrt::hresult_error(E_FAIL, L"Not signed in.");
        }

        auto future = m_auth->current_user().GetToken(forceRefresh);
        co_await AwaitFuture(future);

        if (future.error() != ::firebase::auth::kAuthErrorNone)
        {
            throw winrt::hresult_error(E_FAIL,
                winrt::to_hstring(future.error_message()));
        }

        co_return winrt::to_hstring(*future.result());
    }

    // ── IAuthService-shaped operations ───────────────────────────

    winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
    FirebaseAuthBridge::SignInWithEmailAsync(
        winrt::hstring email, winrt::hstring password)
    {
        if (!m_auth)
            throw winrt::hresult_error(E_FAIL, L"Firebase not initialized.");

        auto future = m_auth->SignInWithEmailAndPassword(
            winrt::to_string(email).c_str(),
            winrt::to_string(password).c_str());

        co_await AwaitFuture(future);

        if (future.error() != ::firebase::auth::kAuthErrorNone)
        {
            throw winrt::hresult_error(E_FAIL,
                winrt::to_hstring(future.error_message()));
        }

        // Sign-in succeeded — fetch the ID token.
        co_return co_await GetCurrentUserTokenAsync(false);
    }

    winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
    FirebaseAuthBridge::SignInWithCredentialAsync(
        winrt::hstring providerId,
        winrt::hstring idToken,
        winrt::hstring accessToken)
    {
        if (!m_auth)
            throw winrt::hresult_error(E_FAIL, L"Firebase not initialized.");

        // Build the credential from provider ID + tokens.
        // Currently only Google is wired; extend for other providers.
        ::firebase::auth::Credential credential;

        auto provider = winrt::to_string(providerId);
        if (provider == "google.com")
        {
            credential = ::firebase::auth::GoogleAuthProvider::GetCredential(
                winrt::to_string(idToken).c_str(),
                winrt::to_string(accessToken).c_str());
        }
        else
        {
            // OAuthProvider::GetCredential for generic OIDC providers.
            credential = ::firebase::auth::OAuthProvider::GetCredential(
                provider.c_str(),
                winrt::to_string(idToken).c_str(),
                winrt::to_string(accessToken).c_str());
        }

        auto future = m_auth->SignInWithCredential(credential);
        co_await AwaitFuture(future);

        if (future.error() != ::firebase::auth::kAuthErrorNone)
        {
            throw winrt::hresult_error(E_FAIL,
                winrt::to_hstring(future.error_message()));
        }

        co_return co_await GetCurrentUserTokenAsync(false);
    }

    winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
    FirebaseAuthBridge::GetIdTokenAsync(bool forceRefresh)
    {
        co_return co_await GetCurrentUserTokenAsync(forceRefresh);
    }

    void FirebaseAuthBridge::SignOut()
    {
        if (m_auth)
        {
            m_auth->SignOut();
        }
    }

    bool FirebaseAuthBridge::IsSignedIn()
    {
        return m_auth && m_auth->current_user().is_valid();
    }

    // ── Event accessors ─────────────────────────────────────────

    winrt::event_token FirebaseAuthBridge::AuthStateChanged(
        winrt::Windows::Foundation::TypedEventHandler<
            winrt::Pivox::Firebase::Native::FirebaseAuthBridge, bool> const& handler)
    {
        return m_authStateChanged.add(handler);
    }

    void FirebaseAuthBridge::AuthStateChanged(
        winrt::event_token const& token) noexcept
    {
        m_authStateChanged.remove(token);
    }
}
