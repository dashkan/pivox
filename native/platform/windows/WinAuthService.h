#pragma once

#include "auth_state.h"
#include "app_state.h"
#include <functional>
#include <memory>
#include <string>
#include <optional>

namespace pivox {

/// Firebase project configuration.
/// Values come from google-services.json (or environment/build config).
struct FirebaseConfig {
    std::string apiKey;
    std::string projectId;
    std::string appId;
    std::string authDomain;    // e.g. "my-project.firebaseapp.com"

    bool isValid() const {
        return !apiKey.empty() && !projectId.empty() && !appId.empty();
    }
};

/// Error from an auth operation.
enum class AuthError {
    None,
    InvalidEmail,
    WrongPassword,
    UserNotFound,
    EmailAlreadyInUse,
    WeakPassword,
    NetworkError,
    NotConfigured,    // Firebase SDK not configured
    Unknown,
};

/// Result of an auth operation.
struct AuthResult {
    AuthError error = AuthError::None;
    std::string errorMessage;
    AuthUser user;

    bool ok() const { return error == AuthError::None; }
};

/// OAuth provider configuration.
/// Client IDs come from Google Cloud Console / GitHub Developer Settings.
struct OAuthConfig {
    std::string googleClientId;
    std::string githubClientId;

    bool hasGoogle() const { return !googleClientId.empty(); }
    bool hasGitHub() const { return !githubClientId.empty(); }
};

/// Manages authentication on Windows.
/// Wraps Firebase C++ SDK for email/password, OAuth2Manager for social sign-in,
/// manages auth state transitions, and stores/restores tokens via AppState.
class WinAuthService {
public:
    explicit WinAuthService(std::shared_ptr<AppState> appState);

    /// Current auth status.
    AuthStatus status() const { return status_; }

    /// Current user (valid only when status == SignedIn).
    const AuthUser& currentUser() const { return currentUser_; }

    /// Register a callback for auth state changes.
    using AuthStateCallback = std::function<void(AuthStatus, const AuthUser&)>;
    void onAuthStateChanged(AuthStateCallback callback);

    /// Configure OAuth providers. Must be called before social sign-in.
    void setOAuthConfig(const OAuthConfig& config);

    /// Email/password sign-in.
    AuthResult signInWithEmail(const std::string& email, const std::string& password);

    /// Email/password registration.
    AuthResult createAccount(const std::string& email, const std::string& password,
                             const std::string& displayName);

    /// Check if Google sign-in is configured.
    bool isGoogleConfigured() const { return oauthConfig_.hasGoogle(); }

    /// Check if GitHub sign-in is configured.
    bool isGitHubConfigured() const { return oauthConfig_.hasGitHub(); }

    /// Initiate Google sign-in via OAuth2Manager.
    /// Returns NotConfigured if googleClientId is not set.
    /// NOTE: The actual OAuth flow is async (coroutine). This method validates
    /// the config synchronously and returns the error. The async flow will be
    /// triggered from the UI layer using OAuth2Manager::RequestAuthWithParamsAsync.
    AuthResult validateGoogleSignIn() const;

    /// Initiate GitHub sign-in via OAuth2Manager.
    AuthResult validateGitHubSignIn() const;

    /// Sign out — clears session, tokens, and auth state.
    void signOut();

    /// Try to restore a previous session from stored tokens.
    /// Returns true if a valid session was restored.
    bool tryRestoreSession();

private:
    void setAuthState(AuthStatus status, const AuthUser& user = {});

    std::shared_ptr<AppState> appState_;
    AuthStatus status_ = AuthStatus::Unknown;
    AuthUser currentUser_;
    AuthStateCallback callback_;
    OAuthConfig oauthConfig_;
};

} // namespace pivox
