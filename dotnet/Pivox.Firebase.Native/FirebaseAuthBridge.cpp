#include "pch.h"
#include "FirebaseAuthBridge.h"
#include "FirebaseAuthBridge.g.cpp"
#include "firebase_config.h"

#include "firebase/auth/user.h"

#include <windows.h>

namespace winrt::Pivox::Firebase::Native::implementation
{
    // ── Auth state listener ──────────────────────────────────────

    struct FirebaseAuthBridge::StateListener
        : public ::firebase::auth::AuthStateListener
    {
        FirebaseAuthBridge* owner;
        std::atomic<bool> active{ true };
        explicit StateListener(FirebaseAuthBridge* o) : owner(o) {}

        void OnAuthStateChanged(::firebase::auth::Auth*) override
        {
            // Guard against teardown race: RemoveAuthStateListener
            // returns before in-progress callbacks complete.
            if (!active.load()) return;
            bool signedIn = owner->IsSignedIn();
            owner->m_authStateChanged(*owner, signedIn);
        }
    };

    // ── Lifecycle ────────────────────────────────────────────────

    FirebaseAuthBridge::~FirebaseAuthBridge()
    {
        if (m_auth && m_listener)
        {
            m_listener->active.store(false);
            m_auth->RemoveAuthStateListener(m_listener.get());
        }
        // Auth::GetAuth() returns a cached pointer owned by the App.
        // Don't delete m_auth — the App destructor frees it.
        m_auth = nullptr;
        if (m_app) { delete m_app; m_app = nullptr; }
    }

    bool FirebaseAuthBridge::Initialize()
    {
        if (m_app) return true;

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

        m_listener = std::make_unique<StateListener>(this);
        m_auth->AddAuthStateListener(m_listener.get());

        wchar_t buf[8]{};
        if (GetEnvironmentVariableW(L"USE_AUTH_EMULATOR", buf, 8) > 0)
        {
            m_auth->UseEmulator("127.0.0.1", 9099);
            OutputDebugStringA("[FirebaseAuthBridge] Connected to auth emulator\n");
        }

        OutputDebugStringA("[FirebaseAuthBridge] Initialized successfully\n");
        return true;
    }

    // ── Internal helpers ─────────────────────────────────────────

    // Encodes the Firebase AuthError enum into a FACILITY_ITF HRESULT
    // so the C# side can extract the numeric code from Exception.HResult.
    // Layout: 0x80040000 | (authError & 0xFFFF).
    static HRESULT AuthErrorToHresult(int authError)
    {
        return MAKE_HRESULT(SEVERITY_ERROR, FACILITY_ITF,
            static_cast<WORD>(authError & 0xFFFF));
    }

    // Throws a winrt::hresult_error carrying the Firebase error code
    // in the HRESULT and the SDK's English message as the description.
    [[noreturn]]
    static void ThrowAuthError(int authError, const char* message)
    {
        throw winrt::hresult_error(
            AuthErrorToHresult(authError),
            winrt::to_hstring(message));
    }

    template <typename T>
    winrt::Windows::Foundation::IAsyncAction
    FirebaseAuthBridge::AwaitFuture(::firebase::Future<T> const& future)
    {
        // Shared so the handle stays alive until both the coroutine
        // frame AND the Firebase completion callback are done. If the
        // IAsyncAction is cancelled while suspended, the coroutine
        // frame is destroyed — without shared ownership, SetEvent
        // would be called on a closed handle.
        auto event = std::make_shared<winrt::handle>(
            CreateEvent(nullptr, TRUE, FALSE, nullptr));
        HANDLE raw = event->get();

        const_cast<::firebase::Future<T>&>(future).OnCompletion(
            [event, raw](const ::firebase::Future<T>&) { SetEvent(raw); });

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
            ThrowAuthError(future.error(), future.error_message());
        }

        co_return winrt::to_hstring(*future.result());
    }

    winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
    FirebaseAuthBridge::SignInWithCredentialInternalAsync(
        ::firebase::auth::Credential const& credential)
    {
        if (!m_auth)
            throw winrt::hresult_error(E_FAIL, L"Firebase not initialized.");

        auto future = m_auth->SignInWithCredential(credential);
        co_await AwaitFuture(future);

        if (future.error() != ::firebase::auth::kAuthErrorNone)
        {
            ThrowAuthError(future.error(), future.error_message());
        }

        // Force refresh so Firebase validates the session server-side.
        // Without this, a disabled account gets a valid-looking cached
        // JWT, the router swaps to Shell, then the listener's forced
        // refresh fails and routes back — causing a flicker.
        co_return co_await GetCurrentUserTokenAsync(true);
    }

    // ── Sign-in paths ────────────────────────────────────────────

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
            ThrowAuthError(future.error(), future.error_message());
        }

        co_return co_await GetCurrentUserTokenAsync(true);
    }

    winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
    FirebaseAuthBridge::SignInWithGoogleCredentialAsync(
        winrt::hstring idToken, winrt::hstring accessToken)
    {
        auto credential = ::firebase::auth::GoogleAuthProvider::GetCredential(
            winrt::to_string(idToken).c_str(),
            winrt::to_string(accessToken).c_str());

        co_return co_await SignInWithCredentialInternalAsync(credential);
    }

    winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
    FirebaseAuthBridge::SignInWithGitHubCredentialAsync(
        winrt::hstring accessToken)
    {
        auto credential = ::firebase::auth::GitHubAuthProvider::GetCredential(
            winrt::to_string(accessToken).c_str());

        co_return co_await SignInWithCredentialInternalAsync(credential);
    }

    winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
    FirebaseAuthBridge::SignInWithOidcCredentialAsync(
        winrt::hstring providerId,
        winrt::hstring idToken,
        winrt::hstring rawNonce)
    {
        // 4-arg overload: (provider, id_token, raw_nonce, access_token).
        // access_token is nullptr for OIDC — the broker doesn't issue one.
        auto credential = ::firebase::auth::OAuthProvider::GetCredential(
            winrt::to_string(providerId).c_str(),
            winrt::to_string(idToken).c_str(),
            winrt::to_string(rawNonce).c_str(),
            nullptr);

        co_return co_await SignInWithCredentialInternalAsync(credential);
    }

    // ── Account lifecycle ────────────────────────────────────────

    winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
    FirebaseAuthBridge::CreateAccountAsync(
        winrt::hstring email, winrt::hstring password, winrt::hstring displayName)
    {
        if (!m_auth)
            throw winrt::hresult_error(E_FAIL, L"Firebase not initialized.");

        // Step 1: create the Firebase user.
        auto createFuture = m_auth->CreateUserWithEmailAndPassword(
            winrt::to_string(email).c_str(),
            winrt::to_string(password).c_str());

        co_await AwaitFuture(createFuture);

        if (createFuture.error() != ::firebase::auth::kAuthErrorNone)
        {
            ThrowAuthError(createFuture.error(), createFuture.error_message());
        }

        // Step 2: set displayName on the new user.
        auto user = m_auth->current_user();
        if (user.is_valid())
        {
            ::firebase::auth::User::UserProfile profile;
            auto nameStr = winrt::to_string(displayName);
            profile.display_name = nameStr.c_str();

            auto updateFuture = user.UpdateUserProfile(profile);
            co_await AwaitFuture(updateFuture);

            if (updateFuture.error() != ::firebase::auth::kAuthErrorNone)
            {
                // Non-fatal — account exists, display name just didn't
                // stick. Log it; don't throw.
                OutputDebugStringA("[FirebaseAuthBridge] UpdateUserProfile failed\n");
            }
            else
            {
                // Step 3: reload to pick up the updated JWT with the
                // name claim.
                auto reloadFuture = user.Reload();
                co_await AwaitFuture(reloadFuture);
            }
        }

        // Return a fresh token (post-profile-update, carries the
        // name claim if the update succeeded).
        co_return co_await GetCurrentUserTokenAsync(/* forceRefresh */ true);
    }

    winrt::Windows::Foundation::IAsyncAction
    FirebaseAuthBridge::SendPasswordResetAsync(winrt::hstring email)
    {
        if (!m_auth)
            throw winrt::hresult_error(E_FAIL, L"Firebase not initialized.");

        auto future = m_auth->SendPasswordResetEmail(
            winrt::to_string(email).c_str());

        co_await AwaitFuture(future);

        if (future.error() != ::firebase::auth::kAuthErrorNone)
        {
            ThrowAuthError(future.error(), future.error_message());
        }
    }

    // ── Token + state ────────────────────────────────────────────

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
