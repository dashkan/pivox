#pragma once
// OAuthPopup — WebView2 popup window for OAuth sign-in.
//
// Replaces the system browser + protocol handler approach.
// Intercepts navigation to the redirect URI scheme, extracts the auth
// code, and closes the popup. Same pattern as ASWebAuthenticationSession
// on macOS.
//
// Works in both the WinUI app and ActiveX (XAML Islands) because
// WinUI Window + WebView2 can be launched from either context.

#include <string>
#include <functional>

namespace Pivox {

struct OAuthPopupResult {
    bool success = false;
    std::string authCode;
    std::string error;
};

using OAuthPopupCallback = std::function<void(OAuthPopupResult)>;

/// Launch a WebView2 popup window for OAuth authorization.
/// @param authUrl      Full authorization URL (with PKCE params, scope, etc.)
/// @param callbackScheme  The redirect URI scheme to intercept (e.g., "com.googleusercontent.apps.xxx")
/// @param callback     Called when auth completes, is cancelled, or fails.
void LaunchOAuthPopup(
    const std::wstring& authUrl,
    const std::wstring& callbackScheme,
    OAuthPopupCallback callback);

} // namespace Pivox
