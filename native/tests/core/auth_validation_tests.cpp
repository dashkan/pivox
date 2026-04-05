#include <gtest/gtest.h>
#include <string>
#include <regex>

// Auth validation logic — will move to core/ when we build the real auth module.
// For now, keep it here as test-driven specification of the validation rules.

namespace {

bool isValidEmail(const std::string& email) {
    if (email.empty()) return false;
    // Minimal check: contains @ and a dot after @.
    auto at = email.find('@');
    if (at == std::string::npos) return false;
    auto dot = email.find('.', at);
    return dot != std::string::npos && dot > at + 1 && dot < email.size() - 1;
}

bool isPasswordValid(const std::string& password) {
    return password.size() >= 8;
}

bool doPasswordsMatch(const std::string& password, const std::string& confirm) {
    return password == confirm;
}

bool isDisplayNameValid(const std::string& name) {
    return !name.empty() && name.size() <= 100;
}

} // namespace

// ---------------------------------------------------------------------------
// Email validation
// ---------------------------------------------------------------------------

TEST(AuthValidation, EmptyEmailIsInvalid) {
    EXPECT_FALSE(isValidEmail(""));
}

TEST(AuthValidation, EmailWithoutAtIsInvalid) {
    EXPECT_FALSE(isValidEmail("userexample.com"));
}

TEST(AuthValidation, EmailWithoutDotAfterAtIsInvalid) {
    EXPECT_FALSE(isValidEmail("user@localhost"));
}

TEST(AuthValidation, ValidEmailAccepted) {
    EXPECT_TRUE(isValidEmail("user@example.com"));
}

TEST(AuthValidation, EmailWithSubdomainAccepted) {
    EXPECT_TRUE(isValidEmail("user@mail.example.com"));
}

// ---------------------------------------------------------------------------
// Password validation
// ---------------------------------------------------------------------------

TEST(AuthValidation, EmptyPasswordIsInvalid) {
    EXPECT_FALSE(isPasswordValid(""));
}

TEST(AuthValidation, ShortPasswordIsInvalid) {
    EXPECT_FALSE(isPasswordValid("abc"));
}

TEST(AuthValidation, MinLengthPasswordIsValid) {
    EXPECT_TRUE(isPasswordValid("12345678"));
}

TEST(AuthValidation, LongPasswordIsValid) {
    EXPECT_TRUE(isPasswordValid("a-very-long-and-secure-password"));
}

// ---------------------------------------------------------------------------
// Password matching
// ---------------------------------------------------------------------------

TEST(AuthValidation, MatchingPasswordsAreValid) {
    EXPECT_TRUE(doPasswordsMatch("secret123", "secret123"));
}

TEST(AuthValidation, MismatchedPasswordsAreInvalid) {
    EXPECT_FALSE(doPasswordsMatch("secret123", "secret456"));
}

// ---------------------------------------------------------------------------
// Display name validation
// ---------------------------------------------------------------------------

TEST(AuthValidation, EmptyDisplayNameIsInvalid) {
    EXPECT_FALSE(isDisplayNameValid(""));
}

TEST(AuthValidation, ValidDisplayNameAccepted) {
    EXPECT_TRUE(isDisplayNameValid("Ashkan"));
}

TEST(AuthValidation, TooLongDisplayNameIsInvalid) {
    std::string longName(101, 'a');
    EXPECT_FALSE(isDisplayNameValid(longName));
}

TEST(AuthValidation, MaxLengthDisplayNameIsValid) {
    std::string maxName(100, 'a');
    EXPECT_TRUE(isDisplayNameValid(maxName));
}
