#include "pch.h"
#include "WinAuthService.h"
#include "firebase_config.h"
#include <winrt/Microsoft.Security.Authentication.OAuth.h>
#include <winrt/Windows.Data.Json.h>

#include "firebase/auth/credential.h"

using namespace winrt::Microsoft::Security::Authentication::OAuth;

namespace {

winrt::fire_and_forget LaunchGoogleOAuth(
    pivox::WinAuthService* self,
    uint64_t windowIdValue,
    std::string clientId,
    std::function<void(pivox::AuthResult)> cb)
{
    try {
        winrt::Microsoft::UI::WindowId windowId;
        windowId.Value = windowIdValue;

        // 1. Build auth request — PKCE handled automatically by OAuth2Manager.
        auto authParams = AuthRequestParams::CreateForAuthorizationCodeRequest(
            winrt::to_hstring(clientId),
            winrt::Windows::Foundation::Uri(winrt::to_hstring(
                std::string(pivox::firebase_config::kGoogleRedirectUri))));
        authParams.Scope(L"openid email profile");

        // 2. Open browser parented to our window.
        auto authResult = co_await OAuth2Manager::RequestAuthWithParamsAsync(
            windowId,
            winrt::Windows::Foundation::Uri(L"https://accounts.google.com/o/oauth2/v2/auth"),
            authParams);

        if (auto failure = authResult.Failure()) {
            self->isOAuthInProgress_ = false;
            auto errCode = winrt::to_string(failure.Error());
            if (errCode == "access_denied") {
                cb({ pivox::AuthError::UserCanceled, "" });
            } else {
                auto errDesc = winrt::to_string(failure.ErrorDescription());
                cb({ pivox::AuthError::Unknown,
                     errDesc.empty() ? pivox::auth_error::kUnknown : errDesc });
            }
            co_return;
        }

        auto authResponse = authResult.Response();
        if (!authResponse) {
            self->isOAuthInProgress_ = false;
            cb({ pivox::AuthError::Unknown, pivox::auth_error::kUnknown });
            co_return;
        }

        // 3. Exchange code for tokens.
        auto tokenParams = TokenRequestParams::CreateForAuthorizationCodeRequest(authResponse);
        auto tokenResult = co_await OAuth2Manager::RequestTokenAsync(
            winrt::Windows::Foundation::Uri(L"https://oauth2.googleapis.com/token"),
            tokenParams);

        if (auto tokenFailure = tokenResult.Failure()) {
            self->isOAuthInProgress_ = false;
            cb({ pivox::AuthError::Unknown, pivox::auth_error::kUnknown });
            co_return;
        }

        auto tokenResponse = tokenResult.Response();
        if (!tokenResponse) {
            self->isOAuthInProgress_ = false;
            cb({ pivox::AuthError::Unknown, pivox::auth_error::kUnknown });
            co_return;
        }

        // 4. Extract tokens.
        auto accessToken = winrt::to_string(tokenResponse.AccessToken());
        std::string idToken;
        auto additionalParams = tokenResponse.AdditionalParams();
        if (additionalParams && additionalParams.HasKey(L"id_token")) {
            idToken = winrt::to_string(additionalParams.Lookup(L"id_token").GetString());
        }

        // Bring app to foreground after OAuth redirect.
        auto appHwnd = FindWindowW(nullptr, L"Pivox");
        if (appHwnd) {
            SetForegroundWindow(appHwnd);
        }

        // 5. Sign in to Firebase with the Google credential via OnCompletion.
        if (self->firebaseAuth_) {
            auto credential = firebase::auth::GoogleAuthProvider::GetCredential(
                idToken.c_str(), accessToken.empty() ? nullptr : accessToken.c_str());
            auto future = self->firebaseAuth_->SignInWithCredential(credential);
            future.OnCompletion([self, cb](const firebase::Future<firebase::auth::User>& f) {
                if (f.error() == 0) {
                    auto user = self->mapFirebaseUser(*f.result());
                    self->setAuthState(pivox::AuthStatus::SignedIn, user);
                    self->isOAuthInProgress_ = false;
                    cb({ pivox::AuthError::None, "", user });
                } else {
                    self->isOAuthInProgress_ = false;
                    cb(self->mapFirebaseError(f.error()));
                }
            });
            co_return;
        }

        // Should not reach here — Firebase is always initialized.
        self->isOAuthInProgress_ = false;
        cb({ pivox::AuthError::NotConfigured, "Firebase not initialized." });

    } catch (...) {
        self->isOAuthInProgress_ = false;
        cb({ pivox::AuthError::Unknown, pivox::auth_error::kUnknown });
    }
}

struct GoogleOAuthRegistrar {
    GoogleOAuthRegistrar() {
        pivox::WinAuthService::setGoogleOAuthLauncher(
            [](pivox::WinAuthService* self, uint64_t windowId, std::string clientId,
               std::function<void(pivox::AuthResult)> cb) {
                LaunchGoogleOAuth(self, windowId, std::move(clientId), std::move(cb));
            });
    }
} s_registrar;

} // namespace
