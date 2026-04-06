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

/// Manages authentication on Windows.
/// Wraps Firebase C++ SDK for email/password, manages auth state transitions,
/// and stores/restores tokens via AppState.
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

    /// Email/password sign-in.
    AuthResult signInWithEmail(const std::string& email, const std::string& password);

    /// Email/password registration.
    AuthResult createAccount(const std::string& email, const std::string& password,
                             const std::string& displayName);

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
};

} // namespace pivox
