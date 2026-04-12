#include <gtest/gtest.h>
#include "DeepLink.h"

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

TEST_F(DeepLinkParseTest, GoogleRedirectSchemeIgnored) {
    auto link = ParseDeepLinkFromUrl(
        L"com.googleusercontent.apps.12345:/oauth2callback?code=abc");
    EXPECT_FALSE(link.isDelegatedAuth);
}

TEST_F(DeepLinkParseTest, DelegatePathWithoutAction) {
    auto link = ParseDeepLinkFromUrl(L"pivox://auth/delegate/");
    EXPECT_TRUE(link.isDelegatedAuth);
    EXPECT_TRUE(link.action.empty());
}

TEST_F(DeepLinkParseTest, NonAuthHost) {
    auto link = ParseDeepLinkFromUrl(L"pivox://settings/delegate/signin");
    EXPECT_FALSE(link.isDelegatedAuth);
}
