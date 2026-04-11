#include "WinAuthService.h"
#include "firebase_config.h"
#include "firebase/app.h"
#include "firebase/auth.h"
#include "firebase/auth/credential.h"
#include <windows.h>

namespace pivox {

// Firebase auth state listener — forwards to WinAuthService callback.
struct WinAuthService::AuthListener : public firebase::auth::AuthStateListener {
    WinAuthService* owner;
    AuthListener(WinAuthService* o) : owner(o) {}
    void OnAuthStateChanged(firebase::auth::Auth*) override {
        if (owner->authStateCallback_) {
            owner->authStateCallback_(owner->isSignedIn());
        }
    }
};

static bool isValidEmail(const std::string& email) {
    if (email.empty()) return false;
    auto at = email.find('@');
    if (at == std::string::npos) return false;
    auto dot = email.find('.', at);
    return dot != std::string::npos && dot > at + 1 && dot < email.size() - 1;
}

WinAuthService::WinAuthService() {}

WinAuthService::~WinAuthService() {
    if (firebaseAuth_ && authListener_) {
        firebaseAuth_->RemoveAuthStateListener(authListener_);
    }
    delete authListener_;
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
    authStateCallback_ = std::move(callback);
}

AuthUser WinAuthService::currentUser() const {
    if (firebaseAuth_ && firebaseAuth_->current_user().is_valid()) {
        return mapFirebaseUser(firebaseAuth_->current_user());
    }
    return {};
}

bool WinAuthService::isSignedIn() const {
    return firebaseAuth_ && firebaseAuth_->current_user().is_valid();
}

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
    if (initResult != firebase::kInitResultSuccess) return false;

    // Register auth state listener.
    authListener_ = new AuthListener(this);
    firebaseAuth_->AddAuthStateListener(authListener_);
    return true;
}

void WinAuthService::connectToEmulatorIfRequested() {
    if (!firebaseAuth_) return;
    wchar_t useEmu[2] = {};
    if (GetEnvironmentVariableW(L"USE_AUTH_EMULATOR", useEmu, 2) > 0 && useEmu[0] == L'1')
        firebaseAuth_->UseEmulator("127.0.0.1", 9099);
}

bool WinAuthService::isFirebaseInitialized() const {
    return firebaseAuth_ != nullptr;
}

void WinAuthService::signInWithEmailAsync(const std::string& email, const std::string& password,
                                            std::function<void(AuthResult)> callback) {
    if (!isValidEmail(email)) { callback({ AuthError::InvalidEmail, auth_error::kInvalidEmail }); return; }
    if (password.empty()) { callback({ AuthError::WrongPassword, auth_error::kInvalidCredential }); return; }
    if (!firebaseAuth_) { callback({ AuthError::NotConfigured, "Firebase not initialized." }); return; }
    auto future = firebaseAuth_->SignInWithEmailAndPassword(email.c_str(), password.c_str());
    future.OnCompletion([this, cb = std::move(callback)](const firebase::Future<firebase::auth::AuthResult>& f) {
        if (f.error() == 0) {
            cb({ AuthError::None, "", mapFirebaseUser(f.result()->user) });
        } else {
            OutputDebugStringA(("[PivoxAuth] SignIn error " + std::to_string(f.error()) + ": " + f.error_message() + "\n").c_str());
            cb(mapFirebaseError(f.error()));
        }
    });
}

void WinAuthService::createAccountAsync(const std::string& email, const std::string& password,
                                          const std::string& displayName,
                                          std::function<void(AuthResult)> callback) {
    if (!isValidEmail(email)) { callback({ AuthError::InvalidEmail, auth_error::kInvalidEmail }); return; }
    if (password.size() < 8) { callback({ AuthError::WeakPassword, auth_error::kWeakPassword }); return; }
    if (!firebaseAuth_) { callback({ AuthError::NotConfigured, "Firebase not initialized." }); return; }
    auto future = firebaseAuth_->CreateUserWithEmailAndPassword(email.c_str(), password.c_str());
    future.OnCompletion([this, displayName, cb = std::move(callback)](const firebase::Future<firebase::auth::AuthResult>& f) {
        if (f.error() != 0) { cb(mapFirebaseError(f.error())); return; }
        auto user = mapFirebaseUser(f.result()->user);
        user.displayName = displayName;
        cb({ AuthError::None, "", user });
    });
}

void WinAuthService::setOAuthConfig(const OAuthConfig& config) { oauthConfig_ = config; }

AuthResult WinAuthService::validateGoogleSignIn() const {
    if (!oauthConfig_.hasGoogle()) return { AuthError::NotConfigured, "Google sign-in not configured." };
    return { AuthError::None };
}

AuthResult WinAuthService::validateGitHubSignIn() const {
    if (!oauthConfig_.hasGitHub()) return { AuthError::NotConfigured, "GitHub sign-in not configured." };
    return { AuthError::None };
}

static std::function<void(pivox::WinAuthService*, uint64_t, std::string, std::function<void(pivox::AuthResult)>)> s_googleOAuthLauncher;

void WinAuthService::setGoogleOAuthLauncher(
    std::function<void(WinAuthService*, uint64_t, std::string, std::function<void(AuthResult)>)> launcher) {
    s_googleOAuthLauncher = std::move(launcher);
}

void WinAuthService::signInWithGoogleAsync(uint64_t parentWindowIdValue, std::function<void(AuthResult)> callback) {
    if (!oauthConfig_.hasGoogle()) { callback({ AuthError::NotConfigured, "Google sign-in not configured." }); return; }
    if (isOAuthInProgress_) { callback({ AuthError::OAuthInProgress, "Sign-in already in progress." }); return; }
    if (testMode_) { callback({ AuthError::NotConfigured, "OAuth disabled in test mode." }); return; }
    if (!s_googleOAuthLauncher) { callback({ AuthError::NotConfigured, "OAuth launcher not registered." }); return; }
    isOAuthInProgress_ = true;
    s_googleOAuthLauncher(this, parentWindowIdValue, oauthConfig_.googleClientId, std::move(callback));
}

void WinAuthService::signOut() {
    if (firebaseAuth_) firebaseAuth_->SignOut();
}

AuthResult WinAuthService::mapFirebaseError(int errorCode) const {
    using namespace firebase::auth;
    switch (errorCode) {
        case kAuthErrorInvalidEmail: return { AuthError::InvalidEmail, auth_error::kInvalidEmail };
        case kAuthErrorWrongPassword: return { AuthError::WrongPassword, auth_error::kInvalidCredential };
        case kAuthErrorUserNotFound: return { AuthError::UserNotFound, auth_error::kInvalidCredential };
        case kAuthErrorEmailAlreadyInUse: return { AuthError::EmailAlreadyInUse, auth_error::kEmailAlreadyInUse };
        case kAuthErrorWeakPassword: return { AuthError::WeakPassword, auth_error::kWeakPassword };
        case kAuthErrorNetworkRequestFailed: return { AuthError::NetworkError, auth_error::kNetworkError };
        default: {
            char buf[128];
            snprintf(buf, sizeof(buf), "[PivoxAuth] Unmapped Firebase error code: %d\n", errorCode);
            OutputDebugStringA(buf);
            return { AuthError::Unknown, auth_error::kUnknown };
        }
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
