#include <gtest/gtest.h>

#include <cstring>

#include "auth_state.h"

// AuthUser and AuthStatus were moved to platform-specific code
// (WinAuthService.h). auth_state.h now only contains shared error string
// constants.

TEST(AuthErrorStrings, AllConstantsAreNonEmpty) {
  EXPECT_GT(std::strlen(pivox::auth_error::kInvalidEmail), 0);
  EXPECT_GT(std::strlen(pivox::auth_error::kInvalidCredential), 0);
  EXPECT_GT(std::strlen(pivox::auth_error::kEmailAlreadyInUse), 0);
  EXPECT_GT(std::strlen(pivox::auth_error::kWeakPassword), 0);
  EXPECT_GT(std::strlen(pivox::auth_error::kNetworkError), 0);
  EXPECT_GT(std::strlen(pivox::auth_error::kTooManyRequests), 0);
  EXPECT_GT(std::strlen(pivox::auth_error::kUnknown), 0);
}

TEST(AuthErrorStrings, EndWithPeriod) {
  // All user-facing error strings should end with a period for consistency.
  auto endsWith = [](const char* s, char c) {
    size_t len = std::strlen(s);
    return len > 0 && s[len - 1] == c;
  };
  EXPECT_TRUE(endsWith(pivox::auth_error::kInvalidEmail, '.'));
  EXPECT_TRUE(endsWith(pivox::auth_error::kInvalidCredential, '.'));
  EXPECT_TRUE(endsWith(pivox::auth_error::kEmailAlreadyInUse, '.'));
  EXPECT_TRUE(endsWith(pivox::auth_error::kWeakPassword, '.'));
  EXPECT_TRUE(endsWith(pivox::auth_error::kNetworkError, '.'));
  EXPECT_TRUE(endsWith(pivox::auth_error::kTooManyRequests, '.'));
  EXPECT_TRUE(endsWith(pivox::auth_error::kUnknown, '.'));
}
