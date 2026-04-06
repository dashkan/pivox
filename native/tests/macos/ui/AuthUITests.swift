import XCTest

class AuthUITests: XCTestCase {

    var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = XCUIApplication()
        // Pass test credentials via environment (never committed to git).
        app.launchEnvironment["TEST_EMAIL"] = ProcessInfo.processInfo.environment["PIVOX_TEST_EMAIL"] ?? ""
        app.launchEnvironment["TEST_PASSWORD"] = ProcessInfo.processInfo.environment["PIVOX_TEST_PASSWORD"] ?? ""
        app.launch()
    }

    override func tearDownWithError() throws {
        app = nil
    }

    // MARK: - Focus

    func testEmailFieldIsFocusedOnLaunch() throws {
        let emailField = app.textFields["login-email"]
        XCTAssertTrue(emailField.waitForExistence(timeout: 5), "Email field should exist")

        // Verify email field has keyboard focus by typing into it.
        emailField.typeText("test")
        XCTAssertEqual(emailField.value as? String, "test", "Email field should receive keyboard input on launch")
    }

    func testTabFromEmailToPassword() throws {
        let emailField = app.textFields["login-email"]
        XCTAssertTrue(emailField.waitForExistence(timeout: 5))

        emailField.typeText("user@test.com")
        app.typeKey(.tab, modifierFlags: [])

        // Password field should now be focused — type into it.
        let passwordField = app.secureTextFields["login-password"]
        XCTAssertTrue(passwordField.waitForExistence(timeout: 3))
        passwordField.typeText("secret")
        // SecureField shows dots, value should not be empty.
        XCTAssertNotEqual(passwordField.value as? String, "", "Password field should have received input")
    }

    func testEnterSubmitsFromPasswordField() throws {
        let emailField = app.textFields["login-email"]
        XCTAssertTrue(emailField.waitForExistence(timeout: 5))

        emailField.click()
        emailField.typeText("invalid@test.com\t") // tab to password
        // Now in password field — type password and press Enter.
        app.typeText("wrong\r")

        // Should show an error (invalid credentials) — proving the form submitted.
        // Firebase returns "The supplied auth credential is malformed or has expired."
        let errorText = app.staticTexts["login-error"]
        XCTAssertTrue(errorText.waitForExistence(timeout: 15), "Error message should appear after submitting wrong credentials")
    }

    // MARK: - Accessibility

    func testAccessibilityIdentifiersExist() throws {
        XCTAssertTrue(app.textFields["login-email"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.secureTextFields["login-password"].exists)
        XCTAssertTrue(app.checkBoxes["login-remember-me"].exists || app.toggles["login-remember-me"].exists)
    }

    // MARK: - Real Auth Flow

    func testSignInWithValidCredentials() throws {
        // Credentials from XCUIApplication.launchEnvironment, set in setUp.
        let email = app.launchEnvironment["TEST_EMAIL"] ?? ""
        let password = app.launchEnvironment["TEST_PASSWORD"] ?? ""

        guard !email.isEmpty, !password.isEmpty else {
            throw XCTSkip("Set PIVOX_TEST_EMAIL and PIVOX_TEST_PASSWORD env vars to run auth tests")
        }

        let emailField = app.textFields["login-email"]
        XCTAssertTrue(emailField.waitForExistence(timeout: 5))
        emailField.click()
        emailField.typeText(email)

        let passwordField = app.secureTextFields["login-password"]
        passwordField.click()
        passwordField.typeText(password)

        app.buttons["login-sign-in"].click()

        // After successful sign-in, sidebar should appear with Operator.
        let operator_ = app.staticTexts["Operator"]
        XCTAssertTrue(operator_.waitForExistence(timeout: 10), "Should navigate to main app after sign-in")
    }

    func testGoogleSignInButtonExists() throws {
        let googleButton = app.buttons.matching(NSPredicate(format: "label CONTAINS[c] 'Google'")).firstMatch
        XCTAssertTrue(googleButton.waitForExistence(timeout: 5), "Continue with Google button should exist")
        XCTAssertTrue(googleButton.isEnabled, "Google sign-in button should be enabled")
    }

    func testSignInWithInvalidPassword() throws {
        let emailField = app.textFields["login-email"]
        XCTAssertTrue(emailField.waitForExistence(timeout: 5))
        emailField.click()
        emailField.typeText("ashkan.daie@gmail.com")

        let passwordField = app.secureTextFields["login-password"]
        passwordField.click()
        passwordField.typeText("wrongpassword123")

        let signInButton = app.buttons["login-sign-in"]
        XCTAssertTrue(signInButton.isEnabled, "Sign In should be enabled after entering email and password")
        signInButton.click()

        let errorText = app.staticTexts["login-error"]
        XCTAssertTrue(errorText.waitForExistence(timeout: 15), "Error message should appear for wrong password")
    }
}
