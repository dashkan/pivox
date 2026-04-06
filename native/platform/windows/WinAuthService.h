#pragma once

#include "auth_state.h"
#include "app_state.h"
#include <functional>
#include <memory>
#include <string>
#include <optional>
#include <vector>
#include <cstdint>

#if PIVOX_HAS_FIREBASE
#include "firebase/app.h"
#include "firebase/auth.h"
#endif

namespace pivox {

/// Firebase project configuration.
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
    OAuthInProgress,
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

    AuthResult signInWithEmail(const std::string& email, const std::string& password);
    AuthResult createAccount(const std::string& email, const std::string& password,
                             const std::string& displayName);

    bool isGoogleConfigured() const { return oauthConfig_.hasGoogle(); }
    bool isGitHubConfigured() const { return oauthConfig_.hasGitHub(); }
    AuthResult validateGoogleSignIn() const;
    AuthResult validateGitHubSignIn() const;

    /// Start Google OAuth flow. Opens browser. Callback called when complete.
    void signInWithGoogleAsync(std::function<void(AuthResult)> callback);

    /// Handle protocol activation callback (pivox://oauth-callback/...)
    void handleOAuthCallback(const std::string& callbackUrl);

    bool isOAuthInProgress() const { return isOAuthInProgress_; }

    /// Suppress browser launch for unit testing.
    void setTestMode(bool enabled) { testMode_ = enabled; }

    void signOut();
    bool tryRestoreSession();

    bool initializeFirebase();
    bool isFirebaseInitialized() const;

    /// Connect to Firebase Auth Emulator if USE_AUTH_EMULATOR=1.
    void connectToEmulatorIfRequested();

    // PKCE helpers (public for testing)
    static std::string generateCodeVerifier();
    static std::string generateCodeChallenge(const std::string& verifier);
    static std::string base64UrlEncode(const std::vector<uint8_t>& data);

private:
    void setAuthState(AuthStatus status, const AuthUser& user = {});
    void saveUserTokens(const AuthUser& user, const std::string& idToken,
                        const std::string& refreshToken);

    std::shared_ptr<AppState> appState_;
    AuthStatus status_ = AuthStatus::Unknown;
    AuthUser currentUser_;
    AuthStateCallback callback_;
    OAuthConfig oauthConfig_;

    // OAuth state
    std::string pendingCodeVerifier_;
    std::string pendingStateNonce_;
    std::function<void(AuthResult)> pendingOAuthCallback_;
    bool isOAuthInProgress_ = false;
    bool testMode_ = false;

#if PIVOX_HAS_FIREBASE
    firebase::App* firebaseApp_ = nullptr;
    firebase::auth::Auth* firebaseAuth_ = nullptr;
    AuthResult mapFirebaseError(int errorCode) const;
    AuthUser mapFirebaseUser(const firebase::auth::User& user) const;
#endif
};

} // namespace pivox
