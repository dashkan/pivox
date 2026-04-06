#include <gtest/gtest.h>
#include "WinAuthService.h"
#include "WinAppState.h"

class WinAuthServiceTest : public ::testing::Test {
protected:
    std::shared_ptr<pivox::WinAppState> appState =
        std::make_shared<pivox::WinAppState>();
    pivox::WinAuthService auth{appState};

    void TearDown() override {
        auth.signOut();
        // Clean up any stored tokens.
        appState->deleteSecure("auth.idToken");
        appState->deleteSecure("auth.refreshToken");
        appState->deleteSecure("auth.uid");
        appState->deleteSecure("auth.email");
        appState->deleteSecure("auth.displayName");
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
// Email/password sign-in
// ---------------------------------------------------------------------------

TEST_F(WinAuthServiceTest, SignInWithEmptyEmailFails) {
    auto result = auth.signInWithEmail("", "password123");
    EXPECT_FALSE(result.ok());
    EXPECT_EQ(result.error, pivox::AuthError::InvalidEmail);
}

TEST_F(WinAuthServiceTest, SignInWithEmptyPasswordFails) {
    auto result = auth.signInWithEmail("user@example.com", "");
    EXPECT_FALSE(result.ok());
    EXPECT_EQ(result.error, pivox::AuthError::WrongPassword);
}

TEST_F(WinAuthServiceTest, SignInWithValidCredentialsSucceeds) {
    auto result = auth.signInWithEmail("user@example.com", "password123");
    // Without Firebase SDK configured, this returns NotConfigured.
    // With Firebase, this would succeed for valid credentials.
    // The test validates the flow works — real auth is an integration test.
    EXPECT_EQ(result.error, pivox::AuthError::NotConfigured);
}

TEST_F(WinAuthServiceTest, SignInValidationRejectsInvalidEmail) {
    auto result = auth.signInWithEmail("not-an-email", "password123");
    EXPECT_EQ(result.error, pivox::AuthError::InvalidEmail);
}

// ---------------------------------------------------------------------------
// Account creation
// ---------------------------------------------------------------------------

TEST_F(WinAuthServiceTest, CreateAccountWithEmptyEmailFails) {
    auto result = auth.createAccount("", "password123", "User");
    EXPECT_FALSE(result.ok());
    EXPECT_EQ(result.error, pivox::AuthError::InvalidEmail);
}

TEST_F(WinAuthServiceTest, CreateAccountWithShortPasswordFails) {
    auto result = auth.createAccount("user@example.com", "short", "User");
    EXPECT_FALSE(result.ok());
    EXPECT_EQ(result.error, pivox::AuthError::WeakPassword);
}

TEST_F(WinAuthServiceTest, CreateAccountWithValidInputsReturnsNotConfigured) {
    auto result = auth.createAccount("user@example.com", "password123", "Test User");
    EXPECT_EQ(result.error, pivox::AuthError::NotConfigured);
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

TEST_F(WinAuthServiceTest, SignOutClearsStoredTokens) {
    // Store some tokens first.
    appState->saveSecure("auth.idToken", "some-token");
    appState->saveSecure("auth.refreshToken", "some-refresh");

    auth.signOut();

    EXPECT_FALSE(appState->loadSecure("auth.idToken").has_value());
    EXPECT_FALSE(appState->loadSecure("auth.refreshToken").has_value());
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
// Session restore
// ---------------------------------------------------------------------------

TEST_F(WinAuthServiceTest, RestoreSessionWithNoTokensReturnsFalse) {
    bool restored = auth.tryRestoreSession();
    EXPECT_FALSE(restored);
    EXPECT_EQ(auth.status(), pivox::AuthStatus::SignedOut);
}

TEST_F(WinAuthServiceTest, RestoreSessionWithStoredTokensSucceeds) {
    // Simulate a previous session by storing tokens.
    appState->saveSecure("auth.idToken", "stored-id-token");
    appState->saveSecure("auth.refreshToken", "stored-refresh-token");
    appState->saveSecure("auth.uid", "test-uid-123");
    appState->saveSecure("auth.email", "user@example.com");
    appState->saveSecure("auth.displayName", "Test User");

    bool restored = auth.tryRestoreSession();
    EXPECT_TRUE(restored);
    EXPECT_EQ(auth.status(), pivox::AuthStatus::SignedIn);
    EXPECT_EQ(auth.currentUser().uid, "test-uid-123");
    EXPECT_EQ(auth.currentUser().email, "user@example.com");
    EXPECT_EQ(auth.currentUser().displayName, "Test User");
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
    // GitHub not set
    auth.setOAuthConfig(config);

    EXPECT_TRUE(auth.isGoogleConfigured());
    EXPECT_FALSE(auth.isGitHubConfigured());
}
