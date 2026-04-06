#pragma once

// Firebase project configuration — shared across all platforms.
// Values from GoogleService-Info.plist / Firebase Console.
// These are NOT secrets — they identify the Firebase project,
// not authenticate it. The API key is restricted server-side.

namespace pivox {
namespace firebase_config {

constexpr const char* kApiKey = "AIzaSyCh7rSoEoBlQleV8aIsCFg41rR3rXhneck";
constexpr const char* kProjectId = "pivox-cloud";
constexpr const char* kStorageBucket = "pivox-cloud.firebasestorage.app";
constexpr const char* kGcmSenderId = "45920224787";
constexpr const char* kFirebaseClientId = "45920224787-aolngdbet2ka86f435j5ejm982iuur5k.apps.googleusercontent.com";
constexpr const char* kBundleId = "app.pivox.native";

// Google Sign-In OAuth client IDs — platform-specific.
#if __APPLE__
constexpr const char* kGoogleSignInClientId = "45920224787-gb662gbotfv763cqjis53748ctgigncl.apps.googleusercontent.com";
#elif _WIN32
constexpr const char* kGoogleSignInClientId = ""; // TODO: Create Desktop client at console.cloud.google.com
#endif

} // namespace firebase_config
} // namespace pivox
