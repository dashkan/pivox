import AppKit
import AuthenticationServices
import CryptoKit
import FirebaseAuth
import FirebaseCore
import Foundation

/// Manages authentication state using Firebase Apple SDK.
/// Observable by SwiftUI views for reactive auth state updates.
@Observable
class AuthService: NSObject {
  static let shared = AuthService()

  var currentUser: User?
  var isSignedIn: Bool {
    #if UITEST
      if ProcessInfo.processInfo.environment["SKIP_AUTH"] == "1" { return true }
    #endif
    return currentUser != nil
  }
  var errorMessage: String?
  private var isOAuthInProgress = false

  // The Firebase Auth instance this service talks to. The default-init path
  // binds to the default Firebase app (`Auth.auth()`). Delegated auth flows
  // (AUTHN-07) construct an AuthService bound to a *named* Firebase app so
  // plugin sign-in stays isolated from the main app's auth state.
  private var boundAuth: Auth?
  private var resolvedAuth: Auth {
    boundAuth ?? Auth.auth()
  }

  // When false, sign-in/out paths skip Keychain and remembered-email writes.
  // Delegated flows set this to false — the plugin session is ephemeral and
  // persisting a token for it would clobber the main user's session.
  private let persistCredentials: Bool

  private var authStateHandle: AuthStateDidChangeListenerHandle?
  private let appState = AppStateBridge.shared()

  private override init() {
    self.persistCredentials = true
    super.init()
  }

  /// Construct an AuthService bound to a specific Firebase Auth instance.
  /// Used by the delegated auth coordinator. Persistence is disabled by
  /// default because the caller owns the session lifetime.
  init(auth: Auth, persistCredentials: Bool = false) {
    self.boundAuth = auth
    self.persistCredentials = persistCredentials
    super.init()
    currentUser = auth.currentUser
    authStateHandle = auth.addStateDidChangeListener { [weak self] _, user in
      self?.currentUser = user
    }
  }

  deinit {
    if let handle = authStateHandle {
      resolvedAuth.removeStateDidChangeListener(handle)
    }
  }

  /// Configure Firebase and start listening for auth state changes.
  /// Call once at app launch (AppDelegate).
  func configure() {
    #if UITEST
      // SKIP_AUTH: bypass Firebase entirely for UI tests that don't need auth.
      // UITEST compilation condition only exists in DebugUITest build config.
      // See docs/dev/ui-testing.md for security considerations.
      if ProcessInfo.processInfo.environment["SKIP_AUTH"] == "1" {
        let uiTesting = ProcessInfo.processInfo.environment["UI_TESTING"] == "1"
        if uiTesting {
          appState.save("", forKey: "selected_section")
        }
        return
      }
    #endif

    FirebaseApp.configure()

    // Point at local emulator for UI tests (must be before any auth calls).
    if ProcessInfo.processInfo.environment["USE_AUTH_EMULATOR"] == "1" {
      resolvedAuth.useEmulator(withHost: "127.0.0.1", port: 9099)
    }

    // Synchronous check — Firebase restores persisted sessions from Keychain
    // immediately after configure(). This prevents the login screen flash.
    currentUser = resolvedAuth.currentUser

    authStateHandle = resolvedAuth.addStateDidChangeListener { [weak self] _, user in
      self?.currentUser = user
    }

    // UI_TESTING resets all state: auth tokens, preferences, sticky UI.
    // Individual flags kept for backward compat but UI_TESTING is the
    // single flag tests should use going forward.
    let uiTesting = ProcessInfo.processInfo.environment["UI_TESTING"] == "1"

    if uiTesting || ProcessInfo.processInfo.environment["RESET_AUTH"] == "1" {
      try? resolvedAuth.signOut()
      appState.deleteSecure(forKey: "firebase_id_token")
      appState.deleteSecure(forKey: "firebase_refresh_token")
    }
    if uiTesting || ProcessInfo.processInfo.environment["RESET_PREFS"] == "1" {
      appState.save("", forKey: "remembered_email")
      appState.save("", forKey: "selected_section")
    }
  }

  // MARK: - Email/Password

  func signIn(email: String, password: String) async {
    errorMessage = nil
    do {
      let result = try await resolvedAuth.signIn(withEmail: email, password: password)
      currentUser = result.user

      // Save token for session restore.
      if persistCredentials, let token = try? await result.user.getIDToken() {
        appState.saveSecure(token, forKey: "firebase_id_token")
      }
    } catch {
      errorMessage = firebaseErrorMessage(error)
    }
  }

  func createAccount(email: String, password: String, displayName: String) async {
    errorMessage = nil
    do {
      let result = try await resolvedAuth.createUser(withEmail: email, password: password)

      // Set display name.
      let changeRequest = result.user.createProfileChangeRequest()
      changeRequest.displayName = displayName
      try await changeRequest.commitChanges()

      // Reload to get updated profile.
      try await result.user.reload()
      currentUser = resolvedAuth.currentUser

      if persistCredentials, let token = try? await result.user.getIDToken() {
        appState.saveSecure(token, forKey: "firebase_id_token")
      }
    } catch {
      errorMessage = firebaseErrorMessage(error)
    }
  }

  // MARK: - Google Sign-In (via ASWebAuthenticationSession)

  private let googleClientID =
    "45920224787-gb662gbotfv763cqjis53748ctgigncl.apps.googleusercontent.com"

  func signInWithGoogle() async {
    guard !isOAuthInProgress else { return }
    isOAuthInProgress = true
    defer { isOAuthInProgress = false }
    errorMessage = nil

    do {
      let (idToken, accessToken) = try await performGoogleOAuth()
      let credential = GoogleAuthProvider.credential(
        withIDToken: idToken,
        accessToken: accessToken
      )
      let authResult = try await resolvedAuth.signIn(with: credential)
      currentUser = authResult.user

      if persistCredentials, let token = try? await authResult.user.getIDToken() {
        appState.saveSecure(token, forKey: "firebase_id_token")
      }
    } catch let error as ASWebAuthenticationSessionError where error.code == .canceledLogin {
      // User canceled the browser sheet — not an error.
      return
    } catch {
      errorMessage = firebaseErrorMessage(error)
    }
  }

  // swiftlint:disable:next function_body_length
  private func performGoogleOAuth() async throws -> (idToken: String, accessToken: String) {
    let nonce = UUID().uuidString
    let codeVerifier = generateCodeVerifier()
    let codeChallenge = generateCodeChallenge(from: codeVerifier)

    var components = URLComponents(string: "https://accounts.google.com/o/oauth2/v2/auth")!
    components.queryItems = [
      URLQueryItem(name: "client_id", value: googleClientID),
      URLQueryItem(
        name: "redirect_uri",
        value:
          "com.googleusercontent.apps.45920224787-gb662gbotfv763cqjis53748ctgigncl:/oauth2callback"),
      URLQueryItem(name: "response_type", value: "code"),
      URLQueryItem(name: "scope", value: "openid email profile"),
      URLQueryItem(name: "code_challenge", value: codeChallenge),
      URLQueryItem(name: "code_challenge_method", value: "S256"),
      URLQueryItem(name: "state", value: nonce),
    ]

    let authURL = components.url!
    let callbackScheme = "com.googleusercontent.apps.45920224787-gb662gbotfv763cqjis53748ctgigncl"

    let callbackURL = try await withCheckedThrowingContinuation {
      (continuation: CheckedContinuation<URL, Error>) in
      let session = ASWebAuthenticationSession(url: authURL, callbackURLScheme: callbackScheme) {
        url, error in
        if let error = error {
          continuation.resume(throwing: error)
        } else if let url = url {
          continuation.resume(returning: url)
        } else {
          continuation.resume(
            throwing: NSError(
              domain: "AuthService", code: -1,
              userInfo: [NSLocalizedDescriptionKey: "No callback URL received"]))
        }
      }
      session.presentationContextProvider = self
      session.prefersEphemeralWebBrowserSession = false
      session.start()
    }

    // Extract auth code from callback URL.
    guard
      let queryItems = URLComponents(url: callbackURL, resolvingAgainstBaseURL: false)?.queryItems,
      let code = queryItems.first(where: { $0.name == "code" })?.value
    else {
      throw NSError(
        domain: "AuthService", code: -1,
        userInfo: [NSLocalizedDescriptionKey: "No auth code in callback"])
    }

    // Exchange code for tokens.
    let tokenURL = URL(string: "https://oauth2.googleapis.com/token")!
    var request = URLRequest(url: tokenURL)
    request.httpMethod = "POST"
    request.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")

    let body = [
      "code=\(code)",
      "client_id=\(googleClientID)",
      "redirect_uri=com.googleusercontent.apps.45920224787-gb662gbotfv763cqjis53748ctgigncl:/oauth2callback",
      "grant_type=authorization_code",
      "code_verifier=\(codeVerifier)",
    ].joined(separator: "&")
    request.httpBody = body.data(using: .utf8)

    let (data, _) = try await URLSession.shared.data(for: request)
    guard let json = try JSONSerialization.jsonObject(with: data) as? [String: Any],
      let idToken = json["id_token"] as? String,
      let accessToken = json["access_token"] as? String
    else {
      throw NSError(
        domain: "AuthService", code: -1,
        userInfo: [NSLocalizedDescriptionKey: "Failed to get tokens from Google"])
    }

    return (idToken, accessToken)
  }

  // MARK: - PKCE Helpers

  private func generateCodeVerifier() -> String {
    var bytes = [UInt8](repeating: 0, count: 32)
    _ = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
    return Data(bytes).base64EncodedString()
      .replacingOccurrences(of: "+", with: "-")
      .replacingOccurrences(of: "/", with: "_")
      .replacingOccurrences(of: "=", with: "")
  }

  private func generateCodeChallenge(from verifier: String) -> String {
    let hash = SHA256.hash(data: Data(verifier.utf8))
    return Data(hash).base64EncodedString()
      .replacingOccurrences(of: "+", with: "-")
      .replacingOccurrences(of: "/", with: "_")
      .replacingOccurrences(of: "=", with: "")
  }

  // MARK: - Sign Out

  func signOut() {
    errorMessage = nil
    do {
      try resolvedAuth.signOut()
      currentUser = nil
      if persistCredentials {
        appState.deleteSecure(forKey: "firebase_id_token")
        appState.deleteSecure(forKey: "firebase_refresh_token")
      }
    } catch {
      errorMessage = "Failed to sign out: \(error.localizedDescription)"
    }
  }

  // MARK: - Sign Out + Error Mapping

  /// Maps Firebase errors to user-facing messages.
  /// These strings MUST match the constants in core/auth_constants.h auth_error namespace
  /// so that all platforms show identical error messages.
  private func firebaseErrorMessage(_ error: Error) -> String {
    let nsError = error as NSError
    guard nsError.domain == AuthErrorDomain else {
      return "Something went wrong. Please try again."
    }

    switch AuthErrorCode(rawValue: nsError.code) {
    case .invalidEmail:
      return "Invalid email address."
    case .wrongPassword, .userNotFound, .invalidCredential:
      // Security: don't reveal whether the email exists.
      return "Incorrect email or password."
    case .emailAlreadyInUse:
      return "An account with this email already exists."
    case .weakPassword:
      return "Password is too weak. Use at least 6 characters."
    case .networkError:
      return "Network error. Check your connection."
    case .tooManyRequests:
      return "Too many attempts. Try again later."
    default:
      return "Something went wrong. Please try again."
    }
  }
}

// MARK: - ASWebAuthenticationPresentationContextProviding

extension AuthService: ASWebAuthenticationPresentationContextProviding {
  func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
    // Must return an existing window — never create a new NSWindow here.
    // This can be called off the main thread by ASWebAuthenticationSession.
    return NSApplication.shared.windows.first ?? NSApplication.shared.keyWindow!
  }
}
