#include "WinAuthService.h"
#include "firebase_config.h"
#include <windows.h>
#include <bcrypt.h>
#include <shellapi.h>
#include <sstream>

#pragma comment(lib, "bcrypt.lib")

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
        return { AuthError::InvalidEmail, auth_error::kInvalidEmail };
    }
    if (password.empty()) {
        return { AuthError::WrongPassword, auth_error::kInvalidCredential };
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
        return { AuthError::InvalidEmail, auth_error::kInvalidEmail };
    }
    if (password.size() < 8) {
        return { AuthError::WeakPassword, auth_error::kWeakPassword };
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
// PKCE helpers
// ---------------------------------------------------------------------------

std::string WinAuthService::base64UrlEncode(const std::vector<uint8_t>& data) {
    static const char table[] =
        "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
    std::string result;
    result.reserve(4 * ((data.size() + 2) / 3));
    for (size_t i = 0; i < data.size(); i += 3) {
        uint32_t n = static_cast<uint32_t>(data[i]) << 16;
        if (i + 1 < data.size()) n |= static_cast<uint32_t>(data[i + 1]) << 8;
        if (i + 2 < data.size()) n |= static_cast<uint32_t>(data[i + 2]);
        result += table[(n >> 18) & 0x3F];
        result += table[(n >> 12) & 0x3F];
        if (i + 1 < data.size()) result += table[(n >> 6) & 0x3F];
        if (i + 2 < data.size()) result += table[n & 0x3F];
    }
    return result;
}

std::string WinAuthService::generateCodeVerifier() {
    std::vector<uint8_t> randomBytes(32);
    BCryptGenRandom(nullptr, randomBytes.data(),
        static_cast<ULONG>(randomBytes.size()), BCRYPT_USE_SYSTEM_PREFERRED_RNG);
    return base64UrlEncode(randomBytes);
}

std::string WinAuthService::generateCodeChallenge(const std::string& verifier) {
    BCRYPT_ALG_HANDLE hAlg = nullptr;
    BCRYPT_HASH_HANDLE hHash = nullptr;
    std::vector<uint8_t> hash(32); // SHA256 = 32 bytes

    BCryptOpenAlgorithmProvider(&hAlg, BCRYPT_SHA256_ALGORITHM, nullptr, 0);
    BCryptCreateHash(hAlg, &hHash, nullptr, 0, nullptr, 0, 0);
    BCryptHashData(hHash, reinterpret_cast<PUCHAR>(const_cast<char*>(verifier.data())),
        static_cast<ULONG>(verifier.size()), 0);
    BCryptFinishHash(hHash, hash.data(), static_cast<ULONG>(hash.size()), 0);
    BCryptDestroyHash(hHash);
    BCryptCloseAlgorithmProvider(hAlg, 0);

    return base64UrlEncode(hash);
}

// ---------------------------------------------------------------------------
// Google OAuth flow
// ---------------------------------------------------------------------------

void WinAuthService::signInWithGoogleAsync(std::function<void(AuthResult)> callback) {
    if (!oauthConfig_.hasGoogle()) {
        callback({ AuthError::NotConfigured,
                   "Google sign-in not configured. Set googleClientId." });
        return;
    }

    if (isOAuthInProgress_) {
        callback({ AuthError::OAuthInProgress, "Sign-in already in progress." });
        return;
    }

    isOAuthInProgress_ = true;
    pendingOAuthCallback_ = std::move(callback);
    pendingCodeVerifier_ = generateCodeVerifier();
    pendingStateNonce_ = generateCodeVerifier(); // reuse as random nonce

    std::string codeChallenge = generateCodeChallenge(pendingCodeVerifier_);

    // Build Google OAuth URL.
    std::ostringstream url;
    url << "https://accounts.google.com/o/oauth2/v2/auth"
        << "?client_id=" << oauthConfig_.googleClientId
        << "&redirect_uri=pivox%3A%2F%2Foauth-callback%2F"
        << "&response_type=code"
        << "&scope=openid%20email%20profile"
        << "&code_challenge=" << codeChallenge
        << "&code_challenge_method=S256"
        << "&state=" << pendingStateNonce_;

    // Open system browser (skipped in test mode).
    if (!testMode_) {
        std::string urlStr = url.str();
        std::wstring wurl(urlStr.begin(), urlStr.end());
        ShellExecuteW(nullptr, L"open", wurl.c_str(), nullptr, nullptr, SW_SHOWNORMAL);
    }
}

void WinAuthService::handleOAuthCallback(const std::string& callbackUrl) {
    if (!isOAuthInProgress_ || !pendingOAuthCallback_) {
        return;
    }

    // Parse query parameters from pivox://oauth-callback/?code=...&state=...
    auto qpos = callbackUrl.find('?');
    if (qpos == std::string::npos) {
        auto cb = std::move(pendingOAuthCallback_);
        isOAuthInProgress_ = false;
        cb({ AuthError::Unknown, auth_error::kUnknown });
        return;
    }

    std::string query = callbackUrl.substr(qpos + 1);
    std::string code, state;

    // Simple query string parser.
    std::istringstream qs(query);
    std::string param;
    while (std::getline(qs, param, '&')) {
        auto eq = param.find('=');
        if (eq == std::string::npos) continue;
        auto key = param.substr(0, eq);
        auto val = param.substr(eq + 1);
        if (key == "code") code = val;
        else if (key == "state") state = val;
    }

    auto cb = std::move(pendingOAuthCallback_);
    auto verifier = std::move(pendingCodeVerifier_);
    auto expectedState = std::move(pendingStateNonce_);
    isOAuthInProgress_ = false;

    // Validate state nonce.
    if (state != expectedState) {
        cb({ AuthError::Unknown, auth_error::kUnknown });
        return;
    }

    if (code.empty()) {
        cb({ AuthError::Unknown, auth_error::kUnknown });
        return;
    }

    // TODO: Exchange auth code for tokens via POST to https://oauth2.googleapis.com/token
    // Then create Firebase credential and sign in.
    // For now, return a clear error that the token exchange is not yet implemented.
    cb({ AuthError::NotConfigured,
         "Google OAuth code received. Token exchange not yet implemented." });
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
