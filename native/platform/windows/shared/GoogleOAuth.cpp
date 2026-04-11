#include "pch.h"
#include "WinAuthService.h"
#include "OAuthPopup.h"
#include "firebase_config.h"

#include "firebase/app.h"
#include "firebase/auth.h"
#include "firebase/auth/credential.h"

#include <random>
#include <sstream>
#include <iomanip>

#include <windows.h>
#include <bcrypt.h>
#pragma comment(lib, "bcrypt.lib")

namespace {

// PKCE: generate a random code_verifier (43-128 chars, A-Z a-z 0-9 -._~).
std::string generateCodeVerifier() {
    static constexpr char chars[] =
        "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~";
    std::random_device rd;
    std::mt19937 gen(rd());
    std::uniform_int_distribution<> dist(0, sizeof(chars) - 2);
    std::string verifier(64, '\0');
    for (auto& c : verifier) { c = chars[dist(gen)]; }
    return verifier;
}

// PKCE: SHA256 hash → Base64URL encode → code_challenge.
std::string generateCodeChallenge(const std::string& verifier) {
    // SHA256 hash.
    BCRYPT_ALG_HANDLE hAlg = nullptr;
    BCryptOpenAlgorithmProvider(&hAlg, BCRYPT_SHA256_ALGORITHM, nullptr, 0);
    BCRYPT_HASH_HANDLE hHash = nullptr;
    BCryptCreateHash(hAlg, &hHash, nullptr, 0, nullptr, 0, 0);
    BCryptHashData(hHash,
        reinterpret_cast<PUCHAR>(const_cast<char*>(verifier.data())),
        static_cast<ULONG>(verifier.size()), 0);
    UCHAR hash[32];
    BCryptFinishHash(hHash, hash, sizeof(hash), 0);
    BCryptDestroyHash(hHash);
    BCryptCloseAlgorithmProvider(hAlg, 0);

    // Base64URL encode (no padding).
    static constexpr char b64[] =
        "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    std::string encoded;
    for (int i = 0; i < 32; i += 3) {
        unsigned int n = (hash[i] << 16);
        if (i + 1 < 32) { n |= (hash[i + 1] << 8); }
        if (i + 2 < 32) { n |= hash[i + 2]; }
        encoded += b64[(n >> 18) & 0x3F];
        encoded += b64[(n >> 12) & 0x3F];
        if (i + 1 < 32) { encoded += b64[(n >> 6) & 0x3F]; }
        if (i + 2 < 32) { encoded += b64[n & 0x3F]; }
    }
    // Base64 → Base64URL: replace + with -, / with _, remove =.
    for (auto& c : encoded) {
        if (c == '+') { c = '-'; }
        else if (c == '/') { c = '_'; }
    }
    return encoded;
}

// Build the full Google OAuth authorization URL with PKCE.
std::wstring buildAuthUrl(const std::string& clientId,
                           const std::string& redirectUri,
                           const std::string& codeChallenge) {
    std::wostringstream url;
    url << L"https://accounts.google.com/o/oauth2/v2/auth"
        << L"?response_type=code"
        << L"&client_id=" << std::wstring(clientId.begin(), clientId.end())
        << L"&redirect_uri=" << std::wstring(redirectUri.begin(), redirectUri.end())
        << L"&scope=openid%20email%20profile"
        << L"&code_challenge=" << std::wstring(codeChallenge.begin(), codeChallenge.end())
        << L"&code_challenge_method=S256";
    return url.str();
}

// Exchange auth code for tokens via HTTP POST.
winrt::Windows::Foundation::IAsyncOperation<winrt::hstring> exchangeCodeForTokens(
    const std::string& authCode,
    const std::string& clientId,
    const std::string& redirectUri,
    const std::string& codeVerifier)
{
    auto httpClient = winrt::Windows::Web::Http::HttpClient();
    auto content = winrt::Windows::Web::Http::HttpFormUrlEncodedContent({
        { L"code", winrt::to_hstring(authCode) },
        { L"client_id", winrt::to_hstring(clientId) },
        { L"redirect_uri", winrt::to_hstring(redirectUri) },
        { L"code_verifier", winrt::to_hstring(codeVerifier) },
        { L"grant_type", L"authorization_code" },
    });

    auto response = co_await httpClient.PostAsync(
        winrt::Windows::Foundation::Uri(L"https://oauth2.googleapis.com/token"),
        content);
    auto body = co_await response.Content().ReadAsStringAsync();
    co_return body;
}

// Launch the OAuth popup and handle the full flow.
void LaunchGoogleOAuth(
    pivox::WinAuthService* self,
    uint64_t /*windowIdValue*/,
    std::string clientId,
    std::function<void(pivox::AuthResult)> cb)
{
    try {
        std::string redirectUri = std::string(pivox::firebase_config::kGoogleRedirectScheme) + ":/oauth2callback";
        std::string codeVerifier = generateCodeVerifier();
        std::string codeChallenge = generateCodeChallenge(codeVerifier);
        std::wstring authUrl = buildAuthUrl(clientId, redirectUri, codeChallenge);
        std::wstring callbackScheme(
            pivox::firebase_config::kGoogleRedirectScheme,
            pivox::firebase_config::kGoogleRedirectScheme + strlen(pivox::firebase_config::kGoogleRedirectScheme));

        // Launch WebView2 popup — non-modal, intercepts redirect.
        auto dispatcher = winrt::Microsoft::UI::Dispatching::DispatcherQueue::GetForCurrentThread();
        Pivox::LaunchOAuthPopup(authUrl, callbackScheme,
            [self, cb, clientId, redirectUri, codeVerifier, dispatcher](Pivox::OAuthPopupResult result) {
                if (!result.success) {
                    self->isOAuthInProgress_ = false;
                    if (result.error == "cancelled" || result.error == "access_denied") {
                        cb({ pivox::AuthError::UserCanceled, "" });
                    } else {
                        cb({ pivox::AuthError::Unknown,
                             result.error.empty() ? pivox::auth_error::kUnknown : result.error });
                    }
                    return;
                }

                // Exchange code for tokens on the dispatcher thread.
                [](auto self, auto cb, auto authCode, auto clientId,
                   auto redirectUri, auto codeVerifier, auto dispatcher) -> winrt::fire_and_forget {
                    try {
                        auto tokenJson = co_await exchangeCodeForTokens(
                            authCode, clientId, redirectUri, codeVerifier);

                        // Parse id_token and access_token from JSON response.
                        auto json = winrt::Windows::Data::Json::JsonObject::Parse(tokenJson);
                        std::string idToken;
                        std::string accessToken;
                        if (json.HasKey(L"id_token")) {
                            idToken = winrt::to_string(json.GetNamedString(L"id_token"));
                        }
                        if (json.HasKey(L"access_token")) {
                            accessToken = winrt::to_string(json.GetNamedString(L"access_token"));
                        }

                        if (idToken.empty()) {
                            self->isOAuthInProgress_ = false;
                            cb({ pivox::AuthError::Unknown, "No id_token in token response." });
                            co_return;
                        }

                        // Sign in to Firebase with the Google credential.
                        auto credential = firebase::auth::GoogleAuthProvider::GetCredential(
                            idToken.c_str(), accessToken.empty() ? nullptr : accessToken.c_str());
                        auto future = self->firebaseAuth_->SignInWithCredential(credential);
                        future.OnCompletion([self, cb](const firebase::Future<firebase::auth::User>& f) {
                            if (f.error() == 0) {
                                auto user = self->mapFirebaseUser(*f.result());
                                self->isOAuthInProgress_ = false;
                                cb({ pivox::AuthError::None, "", user });
                            } else {
                                self->isOAuthInProgress_ = false;
                                cb(self->mapFirebaseError(f.error()));
                            }
                        });
                    } catch (...) {
                        self->isOAuthInProgress_ = false;
                        cb({ pivox::AuthError::Unknown, pivox::auth_error::kUnknown });
                    }
                }(self, cb, result.authCode, clientId, redirectUri, codeVerifier, dispatcher);
            });
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
