#include "WinAuthService.h"
#include "firebase_config.h"
#include "firebase/auth/credential.h"
#include <windows.h>

namespace pivox {

static bool isValidEmail(const std::string& email) {
    if (email.empty()) return false;
    auto at = email.find('@');
    if (at == std::string::npos) return false;
    auto dot = email.find('.', at);
    return dot != std::string::npos && dot > at + 1 && dot < email.size() - 1;
}

WinAuthService::WinAuthService() {}

WinAuthService::~WinAuthService() {
    if (firebaseAuth_) {
        delete firebaseAuth_;
        firebaseAuth_ = nullptr;
    }
    if (firebaseApp_) {
        delete firebaseApp_;
        firebaseApp_ = nullptr;
    }
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

// ---------------------------------------------------------------------------
// Firebase initialization
// ---------------------------------------------------------------------------

bool WinAuthService::initializeFirebase() {
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
}

void WinAuthService::connectToEmulatorIfRequested() {
    if (!firebaseAuth_) return;
    wchar_t useEmu[2] = {};
    if (GetEnvironmentVariableW(L"USE_AUTH_EMULATOR", useEmu, 2) > 0 && useEmu[0] == L'1')
    {
        firebaseAuth_->UseEmulator("127.0.0.1", 9099);
    }
}

bool WinAuthService::isFirebaseInitialized() const {
    return firebaseAuth_ != nullptr;
}

// ---------------------------------------------------------------------------
// Session restore — Firebase manages persistence
// ---------------------------------------------------------------------------

bool WinAuthService::hasValidSession() {
    if (firebaseAuth_ && firebaseAuth_->current_user().is_valid()) {
        currentUser_ = mapFirebaseUser(firebaseAuth_->current_user());
        status_ = AuthStatus::SignedIn;
        if (callback_) callback_(status_, currentUser_);
        return true;
    }
    status_ = AuthStatus::SignedOut;
    return false;
}

// ---------------------------------------------------------------------------
// Email/password sign-in
// ---------------------------------------------------------------------------

void WinAuthService::signInWithEmailAsync(const std::string& email, const std::string& password,
                                            std::function<void(AuthResult)> callback) {
    if (!isValidEmail(email)) {
        callback({ AuthError::InvalidEmail, auth_error::kInvalidEmail });
        return;
    }
    if (password.empty()) {
        callback({ AuthError::WrongPassword, auth_error::kInvalidCredential });
        return;
    }
    if (!firebaseAuth_) {
        callback({ AuthError::NotConfigured, "Firebase not initialized." });
        return;
    }

    auto future = firebaseAuth_->SignInWithEmailAndPassword(email.c_str(), password.c_str());
    future.OnCompletion([this, cb = std::move(callback)](
        const firebase::Future<firebase::auth::AuthResult>& f) {
        if (f.error() == 0) {
            auto user = mapFirebaseUser(f.result()->user);
            setAuthState(AuthStatus::SignedIn, user);
            cb({ AuthError::None, "", user });
        } else {
            cb(mapFirebaseError(f.error()));
        }
    });
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
    if (!firebaseAuth_) {
        callback({ AuthError::NotConfigured, "Firebase not initialized." });
        return;
    }

    auto future = firebaseAuth_->CreateUserWithEmailAndPassword(email.c_str(), password.c_str());
    future.OnCompletion([this, displayName, cb = std::move(callback)](
        const firebase::Future<firebase::auth::AuthResult>& f) {
        if (f.error() != 0) {
            cb(mapFirebaseError(f.error()));
            return;
        }

        auto& fbUser = f.result()->user;
        firebase::auth::User::UserProfile profile;
        profile.display_name = displayName.c_str();
        const_cast<firebase::auth::User&>(fbUser).UpdateUserProfile(profile);

        auto user = mapFirebaseUser(fbUser);
        user.displayName = displayName;
        setAuthState(AuthStatus::SignedIn, user);
        cb({ AuthError::None, "", user });
    });
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
    if (firebaseAuth_) {
        firebaseAuth_->SignOut();
    }
    currentUser_ = {};
    setAuthState(AuthStatus::SignedOut);
}

// ---------------------------------------------------------------------------
// Firebase helpers
// ---------------------------------------------------------------------------

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

} // namespace pivox
