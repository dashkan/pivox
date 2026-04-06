#include "WinAuthService.h"
#include "firebase_config.h"
#include <windows.h>
#include <thread>
#include <chrono>

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

void WinAuthService::connectToEmulatorIfRequested() {
#if PIVOX_HAS_FIREBASE
    if (!firebaseAuth_) return;
    wchar_t useEmu[2] = {};
    if (GetEnvironmentVariableW(L"USE_AUTH_EMULATOR", useEmu, 2) > 0 && useEmu[0] == L'1')
    {
        firebaseAuth_->UseEmulator("127.0.0.1", 9099);
    }
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

void WinAuthService::signInWithEmailAsync(const std::string& email, const std::string& password,
                                            std::function<void(AuthResult)> callback) {
    // Synchronous validation — returns immediately.
    if (!isValidEmail(email)) {
        callback({ AuthError::InvalidEmail, auth_error::kInvalidEmail });
        return;
    }
    if (password.empty()) {
        callback({ AuthError::WrongPassword, auth_error::kInvalidCredential });
        return;
    }

#if PIVOX_HAS_FIREBASE
    if (!firebaseAuth_) {
        callback({ AuthError::NotConfigured, "Firebase not initialized." });
        return;
    }

    // Run Firebase call on background thread to avoid blocking UI.
    auto auth = firebaseAuth_;
    auto self = this;
    std::thread([self, auth, email, password, cb = std::move(callback)]() {
        auto future = auth->SignInWithEmailAndPassword(email.c_str(), password.c_str());
        while (future.status() == firebase::kFutureStatusPending) {
            std::this_thread::sleep_for(std::chrono::milliseconds(10));
        }

        if (future.status() == firebase::kFutureStatusComplete && future.error() == 0) {
            auto* result = future.result();
            auto user = self->mapFirebaseUser(result->user);

            auto tokenFuture = const_cast<firebase::auth::User&>(result->user).GetToken(false);
            while (tokenFuture.status() == firebase::kFutureStatusPending) {
                std::this_thread::sleep_for(std::chrono::milliseconds(10));
            }
            std::string idToken = (tokenFuture.error() == 0) ? *tokenFuture.result() : "";

            self->saveUserTokens(user, idToken, "");
            self->setAuthState(AuthStatus::SignedIn, user);
            cb({ AuthError::None, "", user });
            return;
        }

        cb(self->mapFirebaseError(future.error()));
    }).detach();
#else
    callback({ AuthError::NotConfigured, "Firebase C++ SDK not available. "
               "Build with PIVOX_HAS_FIREBASE=1 after downloading the SDK." });
#endif
}

// ---------------------------------------------------------------------------
// Account creation
// ---------------------------------------------------------------------------

void WinAuthService::createAccountAsync(const std::string& email, const std::string& password,
                                          const std::string& displayName,
                                          std::function<void(AuthResult)> callback) {
    if (!isValidEmail(email)) {
        callback({ AuthError::InvalidEmail, auth_error::kInvalidEmail });
        return;
    }
    if (password.size() < 8) {
        callback({ AuthError::WeakPassword, auth_error::kWeakPassword });
        return;
    }

#if PIVOX_HAS_FIREBASE
    if (!firebaseAuth_) {
        callback({ AuthError::NotConfigured, "Firebase not initialized." });
        return;
    }

    auto auth = firebaseAuth_;
    auto self = this;
    std::thread([self, auth, email, password, displayName, cb = std::move(callback)]() {
        auto future = auth->CreateUserWithEmailAndPassword(email.c_str(), password.c_str());
        while (future.status() == firebase::kFutureStatusPending) {
            std::this_thread::sleep_for(std::chrono::milliseconds(10));
        }

        if (future.status() == firebase::kFutureStatusComplete && future.error() == 0) {
            auto* result = future.result();

            firebase::auth::User::UserProfile profile;
            profile.display_name = displayName.c_str();
            const_cast<firebase::auth::User&>(result->user).UpdateUserProfile(profile);

            auto user = self->mapFirebaseUser(result->user);
            user.displayName = displayName;

            auto tokenFuture = const_cast<firebase::auth::User&>(result->user).GetToken(false);
            while (tokenFuture.status() == firebase::kFutureStatusPending) {
                std::this_thread::sleep_for(std::chrono::milliseconds(10));
            }
            std::string idToken = (tokenFuture.error() == 0) ? *tokenFuture.result() : "";

            self->saveUserTokens(user, idToken, "");
            self->setAuthState(AuthStatus::SignedIn, user);
            cb({ AuthError::None, "", user });
            return;
        }

        cb(self->mapFirebaseError(future.error()));
    }).detach();
#else
    callback({ AuthError::NotConfigured, "Firebase C++ SDK not available. "
               "Build with PIVOX_HAS_FIREBASE=1 after downloading the SDK." });
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
// Google Sign-In via OAuth2Manager
// ---------------------------------------------------------------------------

// The actual OAuth2Manager coroutine lives in GoogleOAuth.cpp (part of the
// WinUI app target, which has access to WinRT). This static lib just does
// validation and delegates.

// Set by GoogleOAuth.cpp at app startup.
static std::function<void(pivox::WinAuthService*, uint64_t, std::string,
    std::function<void(pivox::AuthResult)>)> s_googleOAuthLauncher;

void WinAuthService::setGoogleOAuthLauncher(
    std::function<void(WinAuthService*, uint64_t, std::string,
        std::function<void(AuthResult)>)> launcher) {
    s_googleOAuthLauncher = std::move(launcher);
}

void WinAuthService::signInWithGoogleAsync(
    uint64_t parentWindowIdValue,
    std::function<void(AuthResult)> callback)
{
    if (!oauthConfig_.hasGoogle()) {
        callback({ AuthError::NotConfigured,
                   "Google sign-in not configured. Set googleClientId." });
        return;
    }

    if (isOAuthInProgress_) {
        callback({ AuthError::OAuthInProgress, "Sign-in already in progress." });
        return;
    }

    if (testMode_) {
        callback({ AuthError::NotConfigured, "OAuth disabled in test mode." });
        return;
    }

    if (!s_googleOAuthLauncher) {
        callback({ AuthError::NotConfigured, "OAuth launcher not registered." });
        return;
    }

    isOAuthInProgress_ = true;
    s_googleOAuthLauncher(this, parentWindowIdValue, oauthConfig_.googleClientId,
                          std::move(callback));
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
            return { AuthError::InvalidEmail, auth_error::kInvalidEmail };
        case kAuthErrorWrongPassword:
            return { AuthError::WrongPassword, auth_error::kInvalidCredential };
        case kAuthErrorUserNotFound:
            return { AuthError::UserNotFound, auth_error::kInvalidCredential };
        case kAuthErrorEmailAlreadyInUse:
            return { AuthError::EmailAlreadyInUse, auth_error::kEmailAlreadyInUse };
        case kAuthErrorWeakPassword:
            return { AuthError::WeakPassword, auth_error::kWeakPassword };
        case kAuthErrorNetworkRequestFailed:
            return { AuthError::NetworkError, auth_error::kNetworkError };
        default:
            return { AuthError::Unknown, auth_error::kUnknown };
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
