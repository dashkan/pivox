#pragma once

// Firebase project configuration — shared across all platforms.
// Values from GoogleService-Info.plist / Firebase Console.
// These are NOT secrets — they identify the Firebase project,
// not authenticate it.  The API key is restricted server-side.
//
// Copied from native/core/firebase_config.h.  The dotnet/ tree
// owns this copy; it may diverge from native/ over time.

namespace pivox {
namespace firebase_config {

constexpr const char* kApiKey = "AIzaSyCh7rSoEoBlQleV8aIsCFg41rR3rXhneck";
constexpr const char* kProjectId = "pivox-cloud";
constexpr const char* kStorageBucket = "pivox-cloud.firebasestorage.app";
constexpr const char* kGcmSenderId = "45920224787";
// Firebase App ID (GOOGLE_APP_ID from GoogleService-Info.plist).
// Same across macOS, Windows — single Firebase app registration.
// NOT the OAuth client ID (that's kGoogleSignInClientId below).
constexpr const char* kAppId =
    "1:45920224787:ios:795ea9910b900a400361e6";
constexpr const char* kBundleId = "app.pivox.native";

// Google Sign-In OAuth client ID — iOS-type client, works on all
// platforms.  Used by the C# OAuth flow, not by this C++ bridge.
constexpr const char* kGoogleSignInClientId =
    "45920224787-gb662gbotfv763cqjis53748ctgigncl.apps.googleusercontent.com";

// Reversed client ID used as redirect scheme for Google OAuth.
constexpr const char* kGoogleRedirectScheme =
    "com.googleusercontent.apps.45920224787-gb662gbotfv763cqjis53748ctgigncl";

} // namespace firebase_config
} // namespace pivox
