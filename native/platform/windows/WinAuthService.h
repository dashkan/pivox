#pragma once

#include "auth_state.h"
#include "app_state.h"
#include "firebase/app.h"
#include "firebase/auth.h"
#include <functional>
#include <memory>
#include <string>
#include <optional>
#include <vector>
#include <cstdint>
#include <atomic>

namespace pivox {

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
    OAuthInProgress,
    UserCanceled,
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
class WinAuthService {
public:
    explicit WinAuthService(std::shared_ptr<AppState> appState);
    ~WinAuthService();

    AuthStatus status() const { return status_; }
    const AuthUser& currentUser() const { return currentUser_; }

    using AuthStateCallback = std::function<void(AuthStatus, const AuthUser&)>;
    void onAuthStateChanged(AuthStateCallback callback);

    void setOAuthConfig(const OAuthConfig& config);

    void signInWithEmailAsync(const std::string& email, const std::string& password,
                              std::function<void(AuthResult)> callback);
    void createAccountAsync(const std::string& email, const std::string& password,
                            const std::string& displayName,
                            std::function<void(AuthResult)> callback);

    bool isGoogleConfigured() const { return oauthConfig_.hasGoogle(); }
    bool isGitHubConfigured() const { return oauthConfig_.hasGitHub(); }
    AuthResult validateGoogleSignIn() const;
    AuthResult validateGitHubSignIn() const;

    void signInWithGoogleAsync(uint64_t parentWindowIdValue,
                               std::function<void(AuthResult)> callback);

    bool isOAuthInProgress() const { return isOAuthInProgress_; }

    void setTestMode(bool enabled) { testMode_ = enabled; }

    static void setGoogleOAuthLauncher(
        std::function<void(WinAuthService*, uint64_t, std::string,
            std::function<void(AuthResult)>)> launcher);

    void signOut();

    /// Check if Firebase has a valid persisted session.
    bool hasValidSession();

    bool initializeFirebase();
    bool isFirebaseInitialized() const;
    void connectToEmulatorIfRequested();

    // Public for OAuth launcher access (GoogleOAuth.cpp).
    void setAuthState(AuthStatus status, const AuthUser& user = {});
    std::atomic<bool> isOAuthInProgress_ = false;

    firebase::App* firebaseApp_ = nullptr;
    firebase::auth::Auth* firebaseAuth_ = nullptr;
    AuthResult mapFirebaseError(int errorCode) const;
    AuthUser mapFirebaseUser(const firebase::auth::User& user) const;

private:
    std::shared_ptr<AppState> appState_;
    AuthStatus status_ = AuthStatus::Unknown;
    AuthUser currentUser_;
    AuthStateCallback callback_;
    OAuthConfig oauthConfig_;
    bool testMode_ = false;
};

} // namespace pivox
