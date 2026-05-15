#pragma once

#include "FirebaseAuthBridge.g.h"

// Firebase C++ SDK — headers only; libs linked via vcxproj settings.
#include "firebase/app.h"
#include "firebase/auth.h"
#include "firebase/auth/credential.h"

namespace winrt::Pivox::Firebase::Native::implementation
{
    struct FirebaseAuthBridge : FirebaseAuthBridgeT<FirebaseAuthBridge>
    {
        FirebaseAuthBridge() = default;
        ~FirebaseAuthBridge();

        bool Initialize();

        // ── sign-in paths ───────────────────────────────────────

        winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
            SignInWithEmailAsync(winrt::hstring email, winrt::hstring password);

        winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
            SignInWithGoogleCredentialAsync(
                winrt::hstring idToken, winrt::hstring accessToken);

        winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
            SignInWithGitHubCredentialAsync(winrt::hstring accessToken);

        winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
            SignInWithOidcCredentialAsync(
                winrt::hstring providerId,
                winrt::hstring idToken,
                winrt::hstring rawNonce);

        // ── account lifecycle ───────────────────────────────────

        winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
            CreateAccountAsync(
                winrt::hstring email,
                winrt::hstring password,
                winrt::hstring displayName);

        winrt::Windows::Foundation::IAsyncAction
            SendPasswordResetAsync(winrt::hstring email);

        // ── token + state ───────────────────────────────────────

        winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
            GetIdTokenAsync(bool forceRefresh);

        void SignOut();
        bool IsSignedIn();

        winrt::event_token AuthStateChanged(
            winrt::Windows::Foundation::TypedEventHandler<
                winrt::Pivox::Firebase::Native::FirebaseAuthBridge, bool> const& handler);
        void AuthStateChanged(winrt::event_token const& token) noexcept;

    private:
        template <typename T>
        winrt::Windows::Foundation::IAsyncAction AwaitFuture(
            ::firebase::Future<T> const& future);

        winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
            GetCurrentUserTokenAsync(bool forceRefresh);

        // Signs in with a pre-built credential and returns the JWT.
        winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
            SignInWithCredentialInternalAsync(
                ::firebase::auth::Credential const& credential);

        ::firebase::App* m_app{ nullptr };
        ::firebase::auth::Auth* m_auth{ nullptr };

        struct StateListener;
        std::unique_ptr<StateListener> m_listener;

        winrt::event<winrt::Windows::Foundation::TypedEventHandler<
            winrt::Pivox::Firebase::Native::FirebaseAuthBridge, bool>> m_authStateChanged;
    };
}

namespace winrt::Pivox::Firebase::Native::factory_implementation
{
    struct FirebaseAuthBridge : FirebaseAuthBridgeT<FirebaseAuthBridge, implementation::FirebaseAuthBridge>
    {
    };
}
