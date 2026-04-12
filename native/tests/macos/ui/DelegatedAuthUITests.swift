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

  /// A delegated signin link received as a *cold launch* should open a
  /// dedicated auth window titled "Sign in to Pivox" and skip the main
  /// Pivox window entirely. The user came from a plugin and has no reason
  /// to see the full app — the only UI is the sign-in sheet.
  func testDelegatedSigninColdLaunchOpensOnlyDedicatedWindow() throws {
    launch(deepLink: "pivox://auth/delegate/signin?session=test-code-ui-1")

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

    // The main Pivox window should NOT be created on cold-launch signin —
    // we quit after the flow completes, and there's no reason to flash a
    // second window in front of the user.
    let mainWindow = app.windows["Pivox"]
    XCTAssertFalse(
      mainWindow.exists,
      "Main Pivox window should not be shown on cold-launch signin")
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

  /// A signout deep link delivered as a *cold launch* should sign out of
  /// the default Firebase app and then terminate — there's no reason to
  /// keep an empty Pivox window alive when the user asked for sign-out via
  /// a plugin deep link. Verified by:
  ///   1. The signout launch transitions to .notRunning.
  ///   2. A subsequent plain launch lands on the login screen, proving
  ///      the signout actually cleared the session.
  func testDelegatedSignoutColdLaunchTerminatesAndClearsSession() throws {
    launch()
    registerAccount()
    app.terminate()

    // Relaunch as a signout cold launch. No RESET_AUTH so the emulator
    // session from the first launch is still live going in.
    let signoutApp = XCUIApplication()
    signoutApp.launchEnvironment["USE_AUTH_EMULATOR"] = "1"
    signoutApp.launchEnvironment["PIVOX_TEST_DEEP_LINK"] = "pivox://auth/delegate/signout"
    signoutApp.launch()

    // The app should exit within a couple seconds — there's no UI to
    // interact with on a signout cold launch.
    let terminated = signoutApp.wait(for: .notRunning, timeout: 10)
    XCTAssertTrue(
      terminated,
      "App should terminate after cold-launch signout deep link")

    // Session cleared: a fresh plain launch lands on the login screen.
    let freshApp = XCUIApplication()
    freshApp.launchEnvironment["USE_AUTH_EMULATOR"] = "1"
    freshApp.launch()
    XCTAssertTrue(
      freshApp.textFields["login-email"].waitForExistence(timeout: 10),
      "Plain launch after signout should return to login screen")
    freshApp.terminate()
  }
}
