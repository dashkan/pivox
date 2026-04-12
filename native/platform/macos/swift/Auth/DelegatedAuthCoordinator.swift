import AppKit
import FirebaseAuth
import FirebaseCore
import Foundation

/// Parsed representation of a `pivox://auth/delegate/*` deep link.
///
/// The path segment after `/delegate/` determines the action; the session
/// code is only populated for `signin`.
struct DelegatedAuthDeepLink: Equatable {
  enum Action: String {
    case signin
    case profile
    case signout
  }

  let action: Action
  let sessionCode: String?

  /// Parse a `pivox://auth/delegate/{signin|profile|signout}` URL. Returns
  /// nil for anything that isn't a recognised delegate action — the caller
  /// passes through to normal URL handling (e.g. OAuth redirects).
  static func parse(_ url: URL) -> DelegatedAuthDeepLink? {
    guard url.scheme == "pivox", url.host == "auth" else { return nil }

    var path = url.path
    if path.hasPrefix("/") { path.removeFirst() }
    guard path.hasPrefix("delegate/") else { return nil }
    let actionString = String(path.dropFirst("delegate/".count))
    guard let action = Action(rawValue: actionString) else { return nil }

    var session: String?
    if action == .signin {
      let components = URLComponents(url: url, resolvingAgainstBaseURL: false)
      session = components?.queryItems?.first(where: { $0.name == "session" })?.value
      // signin without a session code is invalid — drop it.
      guard session?.isEmpty == false else { return nil }
    }
    return DelegatedAuthDeepLink(action: action, sessionCode: session)
  }
}

/// Orchestrates the app-side of delegated auth (AUTHN-07).
///
/// Each `pivox://auth/delegate/signin?session=<code>` deep link spins up a
/// fresh coordinator bound to its own named Firebase app
/// (`pivox-delegate-<session-code>`). That isolation keeps the delegated
/// sign-in from touching the main app's Keychain session — the plugin user
/// might be an entirely different human.
///
/// The coordinator does *not* observe auth state itself. The window view
/// that hosts the delegated login UI watches `authService.currentUser` and
/// calls `userDidSignIn()` when it flips non-nil. This keeps the
/// coordinator's pipeline synchronous and trivially unit-testable.
@MainActor
final class DelegatedAuthCoordinator {
  /// Notification posted when a `pivox://auth/delegate/profile` link fires.
  /// `ContentView` observes this and switches the sidebar to the Profile tab.
  static let openProfileNotification = Notification.Name("PivoxDelegatedAuthOpenProfile")

  /// Prefix for every delegated Firebase app name. The session code is
  /// appended to guarantee uniqueness across concurrent delegate flows.
  static let firebaseAppNamePrefix = "pivox-delegate-"

  /// Called when the full signin flow finishes (success or failure). The
  /// AppDelegate uses this to close the delegated-auth window and, when the
  /// app was launched purely for the flow, terminate the process.
  var onFinished: ((Result<Void, Error>) -> Void)?

  /// Network call the coordinator makes once the user signs in. Overridable
  /// so tests can drive the completion path without real HTTP.
  var completeSession: (String, String) async throws -> Bool = DelegatedAuthClient.completeSession

  /// ID-token retrieval hook. Overridable so tests can drive the completion
  /// path without a real Firebase user.
  var fetchIDToken: (AuthService) async throws -> String = { authService in
    guard let user = authService.currentUser else {
      throw NSError(
        domain: "DelegatedAuthCoordinator", code: 10,
        userInfo: [NSLocalizedDescriptionKey: "No current user to fetch ID token from"])
    }
    return try await user.getIDToken()
  }

  /// Factory for the delegated AuthService. Production path creates a
  /// named Firebase app, wraps it in an AuthService with persistence off,
  /// and returns that. Tests override this to skip Firebase entirely.
  var makeDelegatedAuthService: (String) throws -> AuthService =
    DelegatedAuthCoordinator.defaultAuthServiceFactory

  private var pending: PendingFlow?

  // MARK: - Entry points

  /// Start a signin flow for the given session code. Returns the
  /// `AuthService` that the login UI should bind to.
  func beginSignin(sessionCode: String) throws -> AuthService {
    // Re-entering with a fresh code wipes any previous in-flight state.
    cancel()
    let appName = Self.firebaseAppNamePrefix + sessionCode
    let authService = try makeDelegatedAuthService(appName)
    pending = PendingFlow(sessionCode: sessionCode, appName: appName, authService: authService)
    return authService
  }

  /// Tear down any in-flight signin — delete the named Firebase app so
  /// the next attempt gets a clean slate.
  func cancel() {
    guard let pending else { return }
    Self.deleteFirebaseApp(named: pending.appName)
    self.pending = nil
  }

  /// Signal that the delegated auth window's user has finished signing in.
  /// Runs the completion pipeline: fetch the ID token, POST it to the
  /// backend, tear down the named Firebase app, notify `onFinished`.
  func userDidSignIn() {
    guard let pending else { return }
    Task { @MainActor in
      do {
        let idToken = try await fetchIDToken(pending.authService)
        _ = try await completeSession(pending.sessionCode, idToken)
        finish(result: .success(()))
      } catch {
        finish(result: .failure(error))
      }
    }
  }

  /// Process a `signout` deep link. Signs out the *default* Firebase app
  /// (the main app's user). Returns true if a user was signed in before
  /// the call — the AppDelegate uses that to decide whether to terminate.
  static func handleSignout(auth: Auth = Auth.auth()) -> Bool {
    let wasSignedIn = auth.currentUser != nil
    try? auth.signOut()
    return wasSignedIn
  }

  /// Post the profile-navigation notification. Broken out for tests.
  static func handleProfile() {
    NotificationCenter.default.post(name: openProfileNotification, object: nil)
  }

  // MARK: - Helpers

  private func finish(result: Result<Void, Error>) {
    guard let pending else { return }
    Self.deleteFirebaseApp(named: pending.appName)
    self.pending = nil
    onFinished?(result)
  }

  private struct PendingFlow {
    let sessionCode: String
    let appName: String
    let authService: AuthService
  }

  private static func deleteFirebaseApp(named name: String) {
    guard let app = FirebaseApp.app(name: name) else { return }
    app.delete { _ in }
  }

  /// Default production factory — creates the named Firebase app from the
  /// bundled `GoogleService-Info.plist` and hands back an AuthService bound
  /// to its `Auth` instance.
  private static func defaultAuthServiceFactory(appName: String) throws -> AuthService {
    // Defensive: if an old instance with the same name is still around,
    // delete it synchronously before reconfiguring.
    if let existing = FirebaseApp.app(name: appName) {
      let semaphore = DispatchSemaphore(value: 0)
      existing.delete { _ in semaphore.signal() }
      _ = semaphore.wait(timeout: .now() + 1)
    }

    guard
      let plistPath = Bundle.main.path(forResource: "GoogleService-Info", ofType: "plist"),
      let options = FirebaseOptions(contentsOfFile: plistPath)
    else {
      throw NSError(
        domain: "DelegatedAuthCoordinator", code: 1,
        userInfo: [NSLocalizedDescriptionKey: "GoogleService-Info.plist missing or unreadable"])
    }

    FirebaseApp.configure(name: appName, options: options)
    guard let app = FirebaseApp.app(name: appName) else {
      throw NSError(
        domain: "DelegatedAuthCoordinator", code: 2,
        userInfo: [NSLocalizedDescriptionKey: "Named Firebase app failed to initialise"])
    }
    let namedAuth = Auth.auth(app: app)
    // Honour the same emulator flag the default app does. The default-app
    // emulator routing is set in AuthService.configure(), which this named
    // instance never visits — configure the emulator endpoint here instead.
    if ProcessInfo.processInfo.environment["USE_AUTH_EMULATOR"] == "1" {
      namedAuth.useEmulator(withHost: "127.0.0.1", port: 9099)
    }
    return AuthService(auth: namedAuth, persistCredentials: false)
  }
}
