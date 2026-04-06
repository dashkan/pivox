#pragma once

#include <string>
#include <vector>

namespace pivox {

/// Authenticated user identity — platform-independent.
/// Populated by the native auth SDK (Firebase Apple SDK or C++ SDK)
/// and passed to the shared core for gRPC calls, UI state, etc.
struct AuthUser {
    std::string uid;
    std::string email;
    std::string displayName;
    std::string photoURL;
    bool emailVerified = false;

    /// Identity providers linked to this account (e.g., "google.com", "github.com").
    std::vector<std::string> providers;
};

/// Auth state observable by the UI and core layers.
enum class AuthStatus {
    /// Not yet determined — app is checking stored credentials.
    Unknown,

    /// No user is signed in.
    SignedOut,

    /// User is signed in and authenticated.
    SignedIn,
};

/// Shared error messages — use these on ALL platforms for consistent UX.
/// macOS/iOS: match these strings in firebaseErrorMessage().
/// Windows: #include and use directly from WinAuthService.
namespace auth_error {
    constexpr const char* kInvalidEmail       = "Invalid email address.";
    constexpr const char* kInvalidCredential  = "Incorrect email or password.";
    constexpr const char* kEmailAlreadyInUse  = "An account with this email already exists.";
    constexpr const char* kWeakPassword       = "Password is too weak. Use at least 6 characters.";
    constexpr const char* kNetworkError       = "Network error. Check your connection.";
    constexpr const char* kTooManyRequests    = "Too many attempts. Try again later.";
    constexpr const char* kUnknown            = "Something went wrong. Please try again.";
} // namespace auth_error

} // namespace pivox
