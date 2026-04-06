#include "WinAuthService.h"
#include <algorithm>

namespace pivox {

// Minimal email validation — matches auth_validation_tests.cpp logic.
static bool isValidEmail(const std::string& email) {
    if (email.empty()) return false;
    auto at = email.find('@');
    if (at == std::string::npos) return false;
    auto dot = email.find('.', at);
    return dot != std::string::npos && dot > at + 1 && dot < email.size() - 1;
}

WinAuthService::WinAuthService(std::shared_ptr<AppState> appState)
    : appState_(std::move(appState)) {}

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
// Email/password sign-in
// ---------------------------------------------------------------------------

AuthResult WinAuthService::signInWithEmail(const std::string& email, const std::string& password) {
    // Client-side validation.
    if (!isValidEmail(email)) {
        return { AuthError::InvalidEmail, "Invalid email address." };
    }
    if (password.empty()) {
        return { AuthError::WrongPassword, "Password is required." };
    }

    // TODO: Call Firebase C++ SDK — auth->SignInWithEmailAndPassword(email, password).
    // Until Firebase SDK is integrated, return NotConfigured.
    // When configured, a successful sign-in will:
    //   1. Get the user object from Firebase
    //   2. Save tokens via AppState.saveSecure
    //   3. Update auth state to SignedIn
    return { AuthError::NotConfigured, "Firebase C++ SDK not yet configured. "
             "Add google-services.json and integrate firebase-cpp-sdk." };
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

    // TODO: Call Firebase C++ SDK — auth->CreateUserWithEmailAndPassword(email, password).
    // Then auth->GetCurrentUser()->UpdateUserProfile({displayName}).
    return { AuthError::NotConfigured, "Firebase C++ SDK not yet configured. "
             "Add google-services.json and integrate firebase-cpp-sdk." };
}

// ---------------------------------------------------------------------------
// Sign out
// ---------------------------------------------------------------------------

void WinAuthService::signOut() {
    // Clear stored tokens.
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
    auto refreshToken = appState_->loadSecure("auth.refreshToken");
    auto uid = appState_->loadSecure("auth.uid");

    if (!idToken.has_value() || !uid.has_value()) {
        setAuthState(AuthStatus::SignedOut);
        return false;
    }

    // Restore user from saved data.
    // TODO: When Firebase SDK is integrated, validate the token with
    // auth->SignInWithCustomToken or use the refresh token to get a new ID token.
    AuthUser user;
    user.uid = uid.value();
    user.email = appState_->loadSecure("auth.email").value_or("");
    user.displayName = appState_->loadSecure("auth.displayName").value_or("");

    setAuthState(AuthStatus::SignedIn, user);
    return true;
}

} // namespace pivox
