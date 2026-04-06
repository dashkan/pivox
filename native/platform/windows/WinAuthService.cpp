#include "WinAuthService.h"
#include "firebase_config.h"

#if PIVOX_HAS_FIREBASE
#include "firebase/auth/credential.h"
#endif

namespace pivox {

static bool isValidEmail(const std::string& email) {
    if (email.empty()) return false;
    auto at = email.find('@');
    if (at == std::string::npos) return false;
    auto dot = email.find('.', at);
    return dot != std::string::npos && dot > at + 1 && dot < email.size() - 1;
}

WinAuthService::WinAuthService(std::shared_ptr<AppState> appState)
    : appState_(std::move(appState)) {}

WinAuthService::~WinAuthService() {
#if PIVOX_HAS_FIREBASE
    if (firebaseAuth_) {
        delete firebaseAuth_;
        firebaseAuth_ = nullptr;
    }
    if (firebaseApp_) {
        delete firebaseApp_;
        firebaseApp_ = nullptr;
    }
#endif
}

void WinAuthService::onAuthStateChanged(AuthStateCallback callback) {
    callback_ = std::move(callback);
}

void WinAuthService::setAuthState(AuthStatus status, const AuthUser& user) {
    status_ = status;
    currentUser_ = user;
    if (callback_) {
        callback_(status_, currentUser_);
    }
}

void WinAuthService::saveUserTokens(const AuthUser& user, const std::string& idToken,
                                     const std::string& refreshToken) {
    appState_->saveSecure("auth.idToken", idToken);
    appState_->saveSecure("auth.refreshToken", refreshToken);
    appState_->saveSecure("auth.uid", user.uid);
    appState_->saveSecure("auth.email", user.email);
    appState_->saveSecure("auth.displayName", user.displayName);
}

// ---------------------------------------------------------------------------
// Firebase initialization
// ---------------------------------------------------------------------------

bool WinAuthService::initializeFirebase() {
#if PIVOX_HAS_FIREBASE
    if (firebaseApp_) return true;

    firebase::AppOptions options;
    options.set_api_key(firebase_config::kApiKey);
    options.set_project_id(firebase_config::kProjectId);
    options.set_storage_bucket(firebase_config::kStorageBucket);
    options.set_messaging_sender_id(firebase_config::kGcmSenderId);
    options.set_app_id(firebase_config::kFirebaseClientId);

    firebaseApp_ = firebase::App::Create(options);
    if (!firebaseApp_) return false;

    firebase::InitResult initResult;
    firebaseAuth_ = firebase::auth::Auth::GetAuth(firebaseApp_, &initResult);
    return initResult == firebase::kInitResultSuccess;
#else
    return false;
#endif
}

bool WinAuthService::isFirebaseInitialized() const {
#if PIVOX_HAS_FIREBASE
    return firebaseAuth_ != nullptr;
#else
    return false;
#endif
}

// ---------------------------------------------------------------------------
// Email/password sign-in
// ---------------------------------------------------------------------------

AuthResult WinAuthService::signInWithEmail(const std::string& email, const std::string& password) {
    if (!isValidEmail(email)) {
        return { AuthError::InvalidEmail, "Invalid email address." };
    }
    if (password.empty()) {
        return { AuthError::WrongPassword, "Password is required." };
    }

#if PIVOX_HAS_FIREBASE
    if (!firebaseAuth_) {
        return { AuthError::NotConfigured, "Firebase not initialized. Call initializeFirebase() first." };
    }

    auto future = firebaseAuth_->SignInWithEmailAndPassword(email.c_str(), password.c_str());
    future.OnCompletion([](const firebase::Future<firebase::auth::AuthResult>&) {});

    // Block until complete (desktop SDK supports synchronous wait).
    while (future.status() == firebase::kFutureStatusPending) {
        // Process pending events.
    }

    if (future.status() == firebase::kFutureStatusComplete && future.error() == 0) {
        auto* result = future.result();
        auto user = mapFirebaseUser(result->user);

        // Get ID token asynchronously, then save.
        auto tokenFuture = const_cast<firebase::auth::User&>(result->user).GetToken(false);
        while (tokenFuture.status() == firebase::kFutureStatusPending) {}
        std::string idToken = (tokenFuture.error() == 0) ? *tokenFuture.result() : "";

        saveUserTokens(user, idToken, "");
        setAuthState(AuthStatus::SignedIn, user);
        return { AuthError::None, "", user };
    }

    return mapFirebaseError(future.error());
#else
    return { AuthError::NotConfigured, "Firebase C++ SDK not available. "
             "Build with PIVOX_HAS_FIREBASE=1 after downloading the SDK." };
#endif
}

// ---------------------------------------------------------------------------
// Account creation
// ---------------------------------------------------------------------------

AuthResult WinAuthService::createAccount(const std::string& email, const std::string& password,
                                         const std::string& displayName) {
    if (!isValidEmail(email)) {
        return { AuthError::InvalidEmail, "Invalid email address." };
    }
    if (password.size() < 8) {
        return { AuthError::WeakPassword, "Password must be at least 8 characters." };
    }

#if PIVOX_HAS_FIREBASE
    if (!firebaseAuth_) {
        return { AuthError::NotConfigured, "Firebase not initialized." };
    }

    auto future = firebaseAuth_->CreateUserWithEmailAndPassword(email.c_str(), password.c_str());
    while (future.status() == firebase::kFutureStatusPending) {}

    if (future.status() == firebase::kFutureStatusComplete && future.error() == 0) {
        auto* result = future.result();

        // Update display name.
        firebase::auth::User::UserProfile profile;
        profile.display_name = displayName.c_str();
        const_cast<firebase::auth::User&>(result->user).UpdateUserProfile(profile);

        auto user = mapFirebaseUser(result->user);
        user.displayName = displayName;

        auto tokenFuture = const_cast<firebase::auth::User&>(result->user).GetToken(false);
        while (tokenFuture.status() == firebase::kFutureStatusPending) {}
        std::string idToken = (tokenFuture.error() == 0) ? *tokenFuture.result() : "";

        saveUserTokens(user, idToken, "");
        setAuthState(AuthStatus::SignedIn, user);
        return { AuthError::None, "", user };
    }

    return mapFirebaseError(future.error());
#else
    return { AuthError::NotConfigured, "Firebase C++ SDK not available. "
             "Build with PIVOX_HAS_FIREBASE=1 after downloading the SDK." };
#endif
}

// ---------------------------------------------------------------------------
// OAuth config
// ---------------------------------------------------------------------------

void WinAuthService::setOAuthConfig(const OAuthConfig& config) {
    oauthConfig_ = config;
}

AuthResult WinAuthService::validateGoogleSignIn() const {
    if (!oauthConfig_.hasGoogle()) {
        return { AuthError::NotConfigured,
                 "Google sign-in not configured. Set googleClientId in OAuthConfig." };
    }
    return { AuthError::None };
}

AuthResult WinAuthService::validateGitHubSignIn() const {
    if (!oauthConfig_.hasGitHub()) {
        return { AuthError::NotConfigured,
                 "GitHub sign-in not configured. Set githubClientId in OAuthConfig." };
    }
    return { AuthError::None };
}

// ---------------------------------------------------------------------------
// Sign out
// ---------------------------------------------------------------------------

void WinAuthService::signOut() {
#if PIVOX_HAS_FIREBASE
    if (firebaseAuth_) {
        firebaseAuth_->SignOut();
    }
#endif

    appState_->deleteSecure("auth.idToken");
    appState_->deleteSecure("auth.refreshToken");
    appState_->deleteSecure("auth.uid");
    appState_->deleteSecure("auth.email");
    appState_->deleteSecure("auth.displayName");

    setAuthState(AuthStatus::SignedOut);
}

// ---------------------------------------------------------------------------
// Session restore
// ---------------------------------------------------------------------------

bool WinAuthService::tryRestoreSession() {
    auto idToken = appState_->loadSecure("auth.idToken");
    auto uid = appState_->loadSecure("auth.uid");

    if (!idToken.has_value() || !uid.has_value()) {
        setAuthState(AuthStatus::SignedOut);
        return false;
    }

    // TODO: When Firebase SDK is active, validate/refresh the token.
    // For now, restore from saved data.
    AuthUser user;
    user.uid = uid.value();
    user.email = appState_->loadSecure("auth.email").value_or("");
    user.displayName = appState_->loadSecure("auth.displayName").value_or("");

    setAuthState(AuthStatus::SignedIn, user);
    return true;
}

// ---------------------------------------------------------------------------
// Firebase helpers
// ---------------------------------------------------------------------------

#if PIVOX_HAS_FIREBASE
AuthResult WinAuthService::mapFirebaseError(int errorCode) const {
    using namespace firebase::auth;
    switch (errorCode) {
        case kAuthErrorInvalidEmail:
            return { AuthError::InvalidEmail, "Invalid email address." };
        case kAuthErrorWrongPassword:
            return { AuthError::WrongPassword, "Incorrect password." };
        case kAuthErrorUserNotFound:
            return { AuthError::UserNotFound, "No account found with this email." };
        case kAuthErrorEmailAlreadyInUse:
            return { AuthError::EmailAlreadyInUse, "An account with this email already exists." };
        case kAuthErrorWeakPassword:
            return { AuthError::WeakPassword, "Password is too weak." };
        case kAuthErrorNetworkRequestFailed:
            return { AuthError::NetworkError, "Network error. Check your connection." };
        default:
            return { AuthError::Unknown, "Authentication failed." };
    }
}

AuthUser WinAuthService::mapFirebaseUser(const firebase::auth::User& user) const {
    AuthUser authUser;
    if (user.is_valid()) {
        authUser.uid = user.uid();
        authUser.email = user.email();
        authUser.displayName = user.display_name();
        authUser.photoURL = user.photo_url();
        authUser.emailVerified = user.is_email_verified();
    }
    return authUser;
}
#endif

} // namespace pivox
