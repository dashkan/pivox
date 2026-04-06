#include <gtest/gtest.h>
#include "WinAuthService.h"
#include "WinAppState.h"
#include <chrono>
#include <thread>

class WinAuthServiceTest : public ::testing::Test {
protected:
    std::shared_ptr<pivox::WinAppState> appState =
        std::make_shared<pivox::WinAppState>();
    pivox::WinAuthService auth{appState};

    void SetUp() override {
        auth.setTestMode(true);
    }

    void TearDown() override {
        auth.signOut();
    }
};

// ---------------------------------------------------------------------------
// Initial state
// ---------------------------------------------------------------------------

TEST_F(WinAuthServiceTest, InitialStatusIsUnknown) {
    EXPECT_EQ(auth.status(), pivox::AuthStatus::Unknown);
}

TEST_F(WinAuthServiceTest, CurrentUserIsEmptyInitially) {
    EXPECT_TRUE(auth.currentUser().uid.empty());
    EXPECT_TRUE(auth.currentUser().email.empty());
}

// ---------------------------------------------------------------------------
// Email/password sign-in (async — validation fires synchronously)
// ---------------------------------------------------------------------------

TEST_F(WinAuthServiceTest, SignInWithEmptyEmailFails) {
    pivox::AuthResult result;
    auth.signInWithEmailAsync("", "password123", [&](pivox::AuthResult r) { result = r; });
    EXPECT_FALSE(result.ok());
    EXPECT_EQ(result.error, pivox::AuthError::InvalidEmail);
}

TEST_F(WinAuthServiceTest, SignInWithEmptyPasswordFails) {
    pivox::AuthResult result;
    auth.signInWithEmailAsync("user@example.com", "", [&](pivox::AuthResult r) { result = r; });
    EXPECT_FALSE(result.ok());
    EXPECT_EQ(result.error, pivox::AuthError::WrongPassword);
}

TEST_F(WinAuthServiceTest, SignInValidationRejectsInvalidEmail) {
    pivox::AuthResult result;
    auth.signInWithEmailAsync("not-an-email", "password123",
        [&](pivox::AuthResult r) { result = r; });
    EXPECT_EQ(result.error, pivox::AuthError::InvalidEmail);
}

// ---------------------------------------------------------------------------
// Account creation (async — validation fires synchronously)
// ---------------------------------------------------------------------------

TEST_F(WinAuthServiceTest, CreateAccountWithEmptyEmailFails) {
    pivox::AuthResult result;
    auth.createAccountAsync("", "password123", "User",
        [&](pivox::AuthResult r) { result = r; });
    EXPECT_FALSE(result.ok());
    EXPECT_EQ(result.error, pivox::AuthError::InvalidEmail);
}

TEST_F(WinAuthServiceTest, CreateAccountWithShortPasswordFails) {
    pivox::AuthResult result;
    auth.createAccountAsync("user@example.com", "short", "User",
        [&](pivox::AuthResult r) { result = r; });
    EXPECT_FALSE(result.ok());
    EXPECT_EQ(result.error, pivox::AuthError::WeakPassword);
}

// ---------------------------------------------------------------------------
// Sign out
// ---------------------------------------------------------------------------

TEST_F(WinAuthServiceTest, SignOutSetsStatusToSignedOut) {
    auth.signOut();
    EXPECT_EQ(auth.status(), pivox::AuthStatus::SignedOut);
}

TEST_F(WinAuthServiceTest, SignOutClearsCurrentUser) {
    auth.signOut();
    EXPECT_TRUE(auth.currentUser().uid.empty());
    EXPECT_TRUE(auth.currentUser().email.empty());
}

// ---------------------------------------------------------------------------
// Auth state callback
// ---------------------------------------------------------------------------

TEST_F(WinAuthServiceTest, CallbackFiresOnSignOut) {
    pivox::AuthStatus callbackStatus = pivox::AuthStatus::Unknown;
    auth.onAuthStateChanged([&](pivox::AuthStatus s, const pivox::AuthUser&) {
        callbackStatus = s;
    });

    auth.signOut();
    EXPECT_EQ(callbackStatus, pivox::AuthStatus::SignedOut);
}

// ---------------------------------------------------------------------------
// Session — Firebase manages persistence
// ---------------------------------------------------------------------------

TEST_F(WinAuthServiceTest, HasValidSessionReturnsFalseWithoutInit) {
    // Firebase not initialized — no valid session.
    EXPECT_FALSE(auth.hasValidSession());
}

// ---------------------------------------------------------------------------
// OAuth configuration
// ---------------------------------------------------------------------------

TEST_F(WinAuthServiceTest, GoogleNotConfiguredByDefault) {
    EXPECT_FALSE(auth.isGoogleConfigured());
    auto result = auth.validateGoogleSignIn();
    EXPECT_EQ(result.error, pivox::AuthError::NotConfigured);
}

TEST_F(WinAuthServiceTest, GitHubNotConfiguredByDefault) {
    EXPECT_FALSE(auth.isGitHubConfigured());
    auto result = auth.validateGitHubSignIn();
    EXPECT_EQ(result.error, pivox::AuthError::NotConfigured);
}

TEST_F(WinAuthServiceTest, GoogleConfiguredAfterSetOAuthConfig) {
    pivox::OAuthConfig config;
    config.googleClientId = "123456789.apps.googleusercontent.com";
    auth.setOAuthConfig(config);

    EXPECT_TRUE(auth.isGoogleConfigured());
    auto result = auth.validateGoogleSignIn();
    EXPECT_TRUE(result.ok());
}

TEST_F(WinAuthServiceTest, GitHubConfiguredAfterSetOAuthConfig) {
    pivox::OAuthConfig config;
    config.githubClientId = "gh-client-id-abc";
    auth.setOAuthConfig(config);

    EXPECT_TRUE(auth.isGitHubConfigured());
    auto result = auth.validateGitHubSignIn();
    EXPECT_TRUE(result.ok());
}

TEST_F(WinAuthServiceTest, OAuthConfigIndependentProviders) {
    pivox::OAuthConfig config;
    config.googleClientId = "google-id";
    auth.setOAuthConfig(config);

    EXPECT_TRUE(auth.isGoogleConfigured());
    EXPECT_FALSE(auth.isGitHubConfigured());
}

// ---------------------------------------------------------------------------
// Google OAuth — async sign-in (OAuth2Manager)
// ---------------------------------------------------------------------------

TEST_F(WinAuthServiceTest, GoogleSignInAsyncFailsWhenNotConfigured) {
    pivox::AuthResult received;
    auth.signInWithGoogleAsync(0, [&](pivox::AuthResult r) { received = r; });
    EXPECT_EQ(received.error, pivox::AuthError::NotConfigured);
    EXPECT_FALSE(auth.isOAuthInProgress());
}

TEST_F(WinAuthServiceTest, GoogleSignInInTestModeReturnsNotConfigured) {
    pivox::OAuthConfig config;
    config.googleClientId = "test-client-id";
    auth.setOAuthConfig(config);

    pivox::AuthResult received;
    auth.signInWithGoogleAsync(0, [&](pivox::AuthResult r) { received = r; });
    EXPECT_EQ(received.error, pivox::AuthError::NotConfigured);
    EXPECT_FALSE(auth.isOAuthInProgress());
}
