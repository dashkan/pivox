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

} // namespace pivox
