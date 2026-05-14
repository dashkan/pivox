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

        winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
            SignInWithEmailAsync(winrt::hstring email, winrt::hstring password);

        winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
            SignInWithCredentialAsync(
                winrt::hstring providerId,
                winrt::hstring idToken,
                winrt::hstring accessToken);

        winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
            GetIdTokenAsync(bool forceRefresh);

        void SignOut();
        bool IsSignedIn();

        winrt::event_token AuthStateChanged(
            winrt::Windows::Foundation::TypedEventHandler<
                winrt::Pivox::Firebase::Native::FirebaseAuthBridge, bool> const& handler);
        void AuthStateChanged(winrt::event_token const& token) noexcept;

    private:
        // Waits for a firebase::Future to complete, then returns.
        // Throws winrt::hresult_error if the future has an error.
        template <typename T>
        winrt::Windows::Foundation::IAsyncAction AwaitFuture(
            ::firebase::Future<T> const& future);

        // Gets the ID token from the current Firebase user.
        winrt::Windows::Foundation::IAsyncOperation<winrt::hstring>
            GetCurrentUserTokenAsync(bool forceRefresh);

        ::firebase::App* m_app{ nullptr };
        ::firebase::auth::Auth* m_auth{ nullptr };

        // Auth state listener forwarding to the WinRT event.
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
