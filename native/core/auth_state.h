#pragma once

namespace pivox {

/// Shared error messages — use on ALL platforms for consistent UX.
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
}

} // namespace pivox
