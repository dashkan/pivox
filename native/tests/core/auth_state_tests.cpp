#include <gtest/gtest.h>
#include "auth_state.h"

TEST(AuthState, DefaultUserIsEmpty) {
    pivox::AuthUser user;
    EXPECT_TRUE(user.uid.empty());
    EXPECT_TRUE(user.email.empty());
    EXPECT_TRUE(user.displayName.empty());
    EXPECT_TRUE(user.photoURL.empty());
    EXPECT_FALSE(user.emailVerified);
    EXPECT_TRUE(user.providers.empty());
}

TEST(AuthState, UserWithProviders) {
    pivox::AuthUser user;
    user.uid = "abc123";
    user.email = "user@example.com";
    user.displayName = "Test User";
    user.emailVerified = true;
    user.providers = {"google.com", "github.com"};

    EXPECT_EQ(user.uid, "abc123");
    EXPECT_EQ(user.providers.size(), 2);
    EXPECT_EQ(user.providers[0], "google.com");
    EXPECT_EQ(user.providers[1], "github.com");
}

TEST(AuthState, DefaultStatusIsUnknown) {
    pivox::AuthStatus status = pivox::AuthStatus::Unknown;
    EXPECT_EQ(status, pivox::AuthStatus::Unknown);
}

TEST(AuthState, StatusTransitions) {
    pivox::AuthStatus status = pivox::AuthStatus::Unknown;

    // App checks credentials → signed out
    status = pivox::AuthStatus::SignedOut;
    EXPECT_EQ(status, pivox::AuthStatus::SignedOut);

    // User signs in
    status = pivox::AuthStatus::SignedIn;
    EXPECT_EQ(status, pivox::AuthStatus::SignedIn);

    // User signs out
    status = pivox::AuthStatus::SignedOut;
    EXPECT_EQ(status, pivox::AuthStatus::SignedOut);
}
