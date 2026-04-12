import XCTest

/// UI tests for delegated auth deep links (AUTHN-07).
///
/// These tests exercise the full app path end-to-end: the app launches,
/// receives a synthesised `pivox://auth/delegate/*` URL via the UITEST-only
/// `PIVOX_TEST_DEEP_LINK` launchEnvironment hook, and the coordinator/UI
/// respond. The main-window state, the delegated login window, and the
/// profile navigation notification are all observable from XCUITest.
class DelegatedAuthUITests: XCTestCase {

  var app: XCUIApplication!

  override func setUpWithError() throws {
    continueAfterFailure = false
    app = XCUIApplication()
    app.launchEnvironment["USE_AUTH_EMULATOR"] = "1"
    app.launchEnvironment["RESET_AUTH"] = "1"
    app.launchEnvironment["RESET_PREFS"] = "1"
  }

  override func tearDownWithError() throws {
    app = nil
  }

  // MARK: - Helpers

  /// Launch the app with an optional synthesised delegated-auth deep link.
  private func launch(deepLink: String? = nil) {
    if let deepLink {
      app.launchEnvironment["PIVOX_TEST_DEEP_LINK"] = deepLink
    }
    app.launch()
  }

  /// Register a fresh account so the main window lands on the signed-in UI.
  /// Mirrors the pattern in AuthUITests. Returns the email used.
  @discardableResult
  private func registerAccount(
    email: String? = nil,
    password: String = "Delegated123!",
    displayName: String = "Delegated User"
  ) -> String {
    let email = email ?? "delegated-\(UUID().uuidString.prefix(8).lowercased())@pivox.app"
    let link =
      app.links["login-switch-register"].exists
      ? app.links["login-switch-register"]
      : app.buttons["login-switch-register"]
    XCTAssertTrue(link.waitForExistence(timeout: 5), "Create one link should exist")
    link.click()

    let emailField = app.textFields["register-email"]
    XCTAssertTrue(emailField.waitForExistence(timeout: 5))
    emailField.click()
    emailField.typeText(email)

    let nameField = app.textFields["register-display-name"]
    nameField.click()
    nameField.typeText(displayName)

    let passField = app.secureTextFields["register-password"]
    passField.click()
    passField.typeText(password)

    let confirmField = app.secureTextFields["register-confirm-password"]
    confirmField.click()
    confirmField.typeText(password)

    app.buttons["register-create-account"].click()

    XCTAssertTrue(
      app.staticTexts["Operator"].waitForExistence(timeout: 10),
      "Should land on main app after registration")
    return email
  }

  // MARK: - Signin deep link

  /// A delegated signin link received while signed out should open a
  /// dedicated auth window titled "Sign in to Pivox" without disturbing
  /// the main login screen.
  func testDelegatedSigninOpensDedicatedWindow() throws {
    launch(deepLink: "pivox://auth/delegate/signin?session=test-code-ui-1")

    // Main login screen still present.
    XCTAssertTrue(
      app.textFields["login-email"].waitForExistence(timeout: 5),
      "Main login screen should still be visible")

    // Delegated auth window — queried by title via the windows collection.
    let delegatedWindow = app.windows["Sign in to Pivox"]
    XCTAssertTrue(
      delegatedWindow.waitForExistence(timeout: 5),
      "A dedicated delegated auth window should appear")

    // The window hosts the LoginView, so the email field accessibility
    // identifier is reachable inside it.
    let emailInDelegated = delegatedWindow.textFields["login-email"]
    XCTAssertTrue(
      emailInDelegated.waitForExistence(timeout: 3),
      "Delegated auth window should host a LoginView")
  }

  // MARK: - Profile deep link

  /// A profile deep link received while signed in should switch the main
  /// sidebar to the Profile section.
  func testDelegatedProfileDeepLinkSwitchesToProfileSection() throws {
    // Launch normally and register an account first.
    launch()
    registerAccount()

    // Re-launch the app with the profile deep link. RESET_AUTH would wipe
    // the session, so omit it — we need the signed-in state to survive.
    app.terminate()
    let secondApp = XCUIApplication()
    secondApp.launchEnvironment["USE_AUTH_EMULATOR"] = "1"
    secondApp.launchEnvironment["PIVOX_TEST_DEEP_LINK"] = "pivox://auth/delegate/profile"
    secondApp.launch()

    // Profile view's display name label should be visible — the notification
    // handler in ContentView switches the sidebar selection to .profile
    // which mounts ProfileView.
    XCTAssertTrue(
      secondApp.staticTexts["profile-display-name"].waitForExistence(timeout: 10),
      "Profile section should be selected after pivox://auth/delegate/profile")
    secondApp.terminate()
  }

  // MARK: - Signout deep link

  /// A signout deep link received while signed in should sign the user
  /// out of the default Firebase app and return the main window to login.
  func testDelegatedSignoutDeepLinkReturnsToLogin() throws {
    launch()
    registerAccount()

    // Re-launch with the signout deep link (no RESET_AUTH so we keep the
    // signed-in session Firebase just wrote to Keychain).
    app.terminate()
    let secondApp = XCUIApplication()
    secondApp.launchEnvironment["USE_AUTH_EMULATOR"] = "1"
    secondApp.launchEnvironment["PIVOX_TEST_DEEP_LINK"] = "pivox://auth/delegate/signout"
    secondApp.launch()

    // Default coordinator behaviour: if the app wasn't launched purely for
    // the signout (it wasn't — launch() created the main window first),
    // sign out and stay running. Expect the login screen to appear.
    XCTAssertTrue(
      secondApp.textFields["login-email"].waitForExistence(timeout: 10),
      "App should return to login screen after signout deep link")
    secondApp.terminate()
  }
}
