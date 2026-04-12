#include <gtest/gtest.h>
#include "DeepLink.h"
#include "DelegatedAuthClient.h"
#include "WinAuthService.h"

// ---------------------------------------------------------------------------
// Deep link parsing
// ---------------------------------------------------------------------------

class DeepLinkParseTest : public ::testing::Test {};

TEST_F(DeepLinkParseTest, EmptyUrlReturnsDefault) {
    auto link = ParseDeepLinkFromUrl(L"");
    EXPECT_FALSE(link.isDelegatedAuth);
    EXPECT_TRUE(link.action.empty());
    EXPECT_TRUE(link.sessionCode.empty());
}

TEST_F(DeepLinkParseTest, NonPivoxSchemeReturnsDefault) {
    auto link = ParseDeepLinkFromUrl(L"https://example.com/auth/delegate/signin");
    EXPECT_FALSE(link.isDelegatedAuth);
}

TEST_F(DeepLinkParseTest, PivoxNonDelegatePathReturnsDefault) {
    auto link = ParseDeepLinkFromUrl(L"pivox://auth/callback?token=abc");
    EXPECT_FALSE(link.isDelegatedAuth);
    EXPECT_TRUE(link.action.empty());
}

TEST_F(DeepLinkParseTest, SigninWithSessionCode) {
    auto link = ParseDeepLinkFromUrl(
        L"pivox://auth/delegate/signin?session=abc-123-def");
    EXPECT_TRUE(link.isDelegatedAuth);
    EXPECT_EQ(link.action, "signin");
    EXPECT_EQ(link.sessionCode, "abc-123-def");
}

TEST_F(DeepLinkParseTest, SigninWithoutSessionCode) {
    auto link = ParseDeepLinkFromUrl(L"pivox://auth/delegate/signin");
    EXPECT_TRUE(link.isDelegatedAuth);
    EXPECT_EQ(link.action, "signin");
    EXPECT_TRUE(link.sessionCode.empty());
}

TEST_F(DeepLinkParseTest, SigninWithExtraQueryParams) {
    auto link = ParseDeepLinkFromUrl(
        L"pivox://auth/delegate/signin?foo=bar&session=my-code&baz=qux");
    EXPECT_TRUE(link.isDelegatedAuth);
    EXPECT_EQ(link.action, "signin");
    EXPECT_EQ(link.sessionCode, "my-code");
}

TEST_F(DeepLinkParseTest, ProfileAction) {
    auto link = ParseDeepLinkFromUrl(L"pivox://auth/delegate/profile");
    EXPECT_TRUE(link.isDelegatedAuth);
    EXPECT_EQ(link.action, "profile");
    EXPECT_TRUE(link.sessionCode.empty());
}

TEST_F(DeepLinkParseTest, SignoutAction) {
    auto link = ParseDeepLinkFromUrl(L"pivox://auth/delegate/signout");
    EXPECT_TRUE(link.isDelegatedAuth);
    EXPECT_EQ(link.action, "signout");
    EXPECT_TRUE(link.sessionCode.empty());
}

TEST_F(DeepLinkParseTest, UnknownDelegateAction) {
    auto link = ParseDeepLinkFromUrl(L"pivox://auth/delegate/unknown");
    EXPECT_TRUE(link.isDelegatedAuth);
    EXPECT_EQ(link.action, "unknown");
}

TEST_F(DeepLinkParseTest, UuidSessionCode) {
    auto link = ParseDeepLinkFromUrl(
        L"pivox://auth/delegate/signin?session=e5aca8a5-b701-4ccb-be57-36a2b6841a66");
    EXPECT_EQ(link.sessionCode, "e5aca8a5-b701-4ccb-be57-36a2b6841a66");
}

TEST_F(DeepLinkParseTest, MalformedUrlReturnsDefault) {
    auto link = ParseDeepLinkFromUrl(L"not a url at all");
    EXPECT_FALSE(link.isDelegatedAuth);
}

// ---------------------------------------------------------------------------
// DelegatedAuthClient — backend URL resolution
// ---------------------------------------------------------------------------

class DelegatedAuthClientTest : public ::testing::Test {
protected:
    void SetUp() override {
        // Clear the env var so tests get the default.
        SetEnvironmentVariableW(L"PIVOX_BACKEND_URL", nullptr);
    }
    void TearDown() override {
        SetEnvironmentVariableW(L"PIVOX_BACKEND_URL", nullptr);
    }
};

TEST_F(DelegatedAuthClientTest, DefaultBackendUrl) {
    EXPECT_EQ(pivox::DelegatedAuthClient::backendUrl(), "https://pivox.ngrok.app");
}

TEST_F(DelegatedAuthClientTest, EnvVarOverridesDefault) {
    SetEnvironmentVariableW(L"PIVOX_BACKEND_URL", L"http://localhost:8080");
    EXPECT_EQ(pivox::DelegatedAuthClient::backendUrl(), "http://localhost:8080");
}

TEST_F(DelegatedAuthClientTest, EmptyEnvVarUsesDefault) {
    SetEnvironmentVariableW(L"PIVOX_BACKEND_URL", L"");
    EXPECT_EQ(pivox::DelegatedAuthClient::backendUrl(), "https://pivox.ngrok.app");
}

// ---------------------------------------------------------------------------
// WinAuthService — delegated auth methods (without Firebase)
// ---------------------------------------------------------------------------

class WinAuthServiceDelegatedTest : public ::testing::Test {
protected:
    pivox::WinAuthService auth{};
};

TEST_F(WinAuthServiceDelegatedTest, SignInWithCustomTokenFailsWithoutFirebase) {
    pivox::AuthResult result;
    auth.signInWithCustomTokenAsync("fake-token",
        [&](pivox::AuthResult r) { result = r; });
    EXPECT_FALSE(result.ok());
    EXPECT_EQ(result.error, pivox::AuthError::NotConfigured);
}

TEST_F(WinAuthServiceDelegatedTest, GetIdTokenReturnsEmptyWithoutFirebase) {
    std::string token = "not-empty";
    auth.getIdTokenAsync([&](std::string t) { token = t; });
    EXPECT_TRUE(token.empty());
}

TEST_F(WinAuthServiceDelegatedTest, IsFirebaseInitializedFalseByDefault) {
    EXPECT_FALSE(auth.isFirebaseInitialized());
}
