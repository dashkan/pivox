#pragma once

#include "auth_state.h"
#include <functional>
#include <string>
#include <cstdint>
#include <atomic>

// Forward-declare Firebase types — headers in WinAuthService.cpp only.
namespace firebase { class App; namespace auth { class Auth; class User; } }

namespace pivox {

/// Authenticated user identity.
struct AuthUser {
    std::string uid;
    std::string email;
    std::string displayName;
    std::string photoURL;
    bool emailVerified = false;
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

/// Auth state change callback — fired when user signs in or out.
using AuthStateCallback = std::function<void(bool signedIn)>;

/// Manages authentication on Windows.
/// Firebase SDK handles session persistence — no manual state tracking.
class WinAuthService {
public:
    WinAuthService();
    ~WinAuthService();

    /// Get current user from Firebase (reads SDK state directly).
    AuthUser currentUser() const;

    /// Check if a user is signed in (reads SDK state directly).
    bool isSignedIn() const;

    /// Register a callback for auth state changes (sign in / sign out).
    /// Firebase's AuthStateListener drives this.
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

    bool initializeFirebase();
    bool isFirebaseInitialized() const;
    void connectToEmulatorIfRequested();

    // Public for OAuth launcher access (GoogleOAuth.cpp).
    std::atomic<bool> isOAuthInProgress_ = false;
    firebase::App* firebaseApp_ = nullptr;
    firebase::auth::Auth* firebaseAuth_ = nullptr;
    AuthResult mapFirebaseError(int errorCode) const;
    AuthUser mapFirebaseUser(const firebase::auth::User& user) const;

private:
    OAuthConfig oauthConfig_;
    AuthStateCallback authStateCallback_;
    bool testMode_ = false;

    // Firebase AuthStateListener implementation (defined in .cpp).
    struct AuthListener;
    AuthListener* authListener_ = nullptr;
};

} // namespace pivox
