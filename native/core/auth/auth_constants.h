#pragma once

namespace pivox {

/// Shared error messages — use on ALL platforms for consistent UX.
/// macOS/iOS: match these strings in firebaseErrorMessage().
/// Windows: #include and use directly from WinAuthService.
namespace auth_error {
constexpr const char* kInvalidEmail = "Invalid email address.";
constexpr const char* kInvalidCredential = "Incorrect email or password.";
constexpr const char* kEmailAlreadyInUse =
    "An account with this email already exists.";
constexpr const char* kWeakPassword =
    "Password is too weak. Use at least 6 characters.";
constexpr const char* kNetworkError = "Network error. Check your connection.";
constexpr const char* kTooManyRequests = "Too many attempts. Try again later.";
constexpr const char* kUnknown = "Something went wrong. Please try again.";
}  // namespace auth_error

/// Canonical sign-out policy for Firebase auth errors.
///
/// Each platform has its own Firebase SDK error enum (iOS AuthErrorCode,
/// Firebase C++ SDK's firebase::auth::Error, etc.) — the raw numeric
/// codes do NOT match across platforms. What MUST match is the policy:
/// which logical error conditions trigger a forced sign-out vs which
/// are transient and let the user retry.
///
/// Platforms implement their own isReAuthRequired() classifier mapping
/// SDK-specific codes onto this list. Both platforms' lists must match
/// these exact cases or behavior diverges (user stuck signed in with a
/// dead session on one platform, routed to login on the other).
///
/// Trigger forced sign-out on:
///   - USER_TOKEN_EXPIRED     — refresh token rejected by server
///   - INVALID_USER_TOKEN     — refresh token malformed / revoked
///   - USER_DISABLED          — account disabled by admin
///   - USER_NOT_FOUND         — account deleted server-side
///   - USER_MISMATCH          — token belongs to a different user
///   - REQUIRES_RECENT_LOGIN  — sensitive op needs fresh login
///
/// All other Firebase auth errors (network, timeout, rate limit,
/// INTERNAL_ERROR, TOO_MANY_REQUESTS) are transient — do NOT sign out.
namespace auth_reauth {}  // intentionally empty — comment is the contract

}  // namespace pivox
