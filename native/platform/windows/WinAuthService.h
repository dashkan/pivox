#pragma once

#include "auth_state.h"
#include "app_state.h"
#include <functional>
#include <memory>
#include <string>
#include <optional>

#if PIVOX_HAS_FIREBASE
#include "firebase/app.h"
#include "firebase/auth.h"
#endif

namespace pivox {

/// Firebase project configuration.
/// Values come from firebase_config.h (compile-time constants).
struct FirebaseConfig {
    std::string apiKey;
    std::string projectId;
    std::string appId;
    std::string authDomain;

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
    NotConfigured,
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
struct OAuthConfig {
    std::string googleClientId;
    std::string githubClientId;

    bool hasGoogle() const { return !googleClientId.empty(); }
    bool hasGitHub() const { return !githubClientId.empty(); }
};

/// Manages authentication on Windows.
/// Uses Firebase C++ SDK for email/password and credential-based sign-in,
/// OAuth2Manager for social sign-in, and AppState for token storage.
class WinAuthService {
public:
    explicit WinAuthService(std::shared_ptr<AppState> appState);
    ~WinAuthService();

    AuthStatus status() const { return status_; }
    const AuthUser& currentUser() const { return currentUser_; }

    using AuthStateCallback = std::function<void(AuthStatus, const AuthUser&)>;
    void onAuthStateChanged(AuthStateCallback callback);

    void setOAuthConfig(const OAuthConfig& config);

    AuthResult signInWithEmail(const std::string& email, const std::string& password);
    AuthResult createAccount(const std::string& email, const std::string& password,
                             const std::string& displayName);

    bool isGoogleConfigured() const { return oauthConfig_.hasGoogle(); }
    bool isGitHubConfigured() const { return oauthConfig_.hasGitHub(); }
    AuthResult validateGoogleSignIn() const;
    AuthResult validateGitHubSignIn() const;

    void signOut();
    bool tryRestoreSession();

    /// Initialize Firebase SDK. Call once at app startup.
    bool initializeFirebase();
    bool isFirebaseInitialized() const;

private:
    void setAuthState(AuthStatus status, const AuthUser& user = {});
    void saveUserTokens(const AuthUser& user, const std::string& idToken,
                        const std::string& refreshToken);

    std::shared_ptr<AppState> appState_;
    AuthStatus status_ = AuthStatus::Unknown;
    AuthUser currentUser_;
    AuthStateCallback callback_;
    OAuthConfig oauthConfig_;

#if PIVOX_HAS_FIREBASE
    firebase::App* firebaseApp_ = nullptr;
    firebase::auth::Auth* firebaseAuth_ = nullptr;
    AuthResult mapFirebaseError(int errorCode) const;
    AuthUser mapFirebaseUser(const firebase::auth::User& user) const;
#endif
};

} // namespace pivox
