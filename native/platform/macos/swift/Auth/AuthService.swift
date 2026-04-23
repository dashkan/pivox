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
  /// Monotonically-bumped counter that triggers `@Observable`
  /// re-evaluation for consumers outside this file. Firebase mutates
  /// its `User` reference in place on profile edits (photoURL,
  /// displayName, etc.), so `@Observable`'s identity-based change
  /// detection doesn't reliably fire for downstream views that read
  /// `currentUser.photoURL` etc. Any mutation path that edits the
  /// user profile in place should bump this; views that need to react
  /// to such changes should read it inside their body (often just
  /// `_ = auth.profileRevision` is enough) to establish the dep.
  var profileRevision: Int = 0
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

  // MARK: - GitHub Sign-In
  //
  // Flow: ASWebAuthenticationSession opens github.com/login/oauth/authorize
  // with redirect_uri pointing at our Cloud Function. GitHub redirects
  // to the function with a code; the function exchanges the code using
  // its server-side client_secret and then bounces the user back via
  // `pivox://oauth/github/callback?access_token=…&state=…`, which
  // ASWebAuthenticationSession intercepts in-process.
  //
  // Why via our function rather than a direct pivox:// callback: the
  // code-for-token exchange needs client_secret, and we won't ship
  // secrets in the binary. Using one Cloud Function as the OAuth App
  // callback also lets web share the same OAuth App and the same
  // server-side exchange path.

  private let githubClientID = "Ov23lizatEPgvs7FVA8X"
  private let githubCallbackURL =
    "https://us-central1-pivox-cloud.cloudfunctions.net/githubOAuthCallback"
  private let githubFinalScheme = "pivox"

  func signInWithGitHub() async {
    guard !isOAuthInProgress else { return }
    isOAuthInProgress = true
    defer { isOAuthInProgress = false }
    errorMessage = nil

    guard !githubClientID.isEmpty else {
      errorMessage =
        "GitHub sign-in is not configured. Set `githubClientID` in AuthService."
      return
    }

    do {
      let nonce = UUID().uuidString
      let state = "native:\(nonce)"

      var components = URLComponents(string: "https://github.com/login/oauth/authorize")!
      components.queryItems = [
        URLQueryItem(name: "client_id", value: githubClientID),
        URLQueryItem(name: "redirect_uri", value: githubCallbackURL),
        URLQueryItem(name: "scope", value: "read:user user:email"),
        URLQueryItem(name: "state", value: state),
        URLQueryItem(name: "allow_signup", value: "true"),
      ]

      let finalURL = try await withCheckedThrowingContinuation {
        (continuation: CheckedContinuation<URL, Error>) in
        let session = ASWebAuthenticationSession(
          url: components.url!,
          callbackURLScheme: githubFinalScheme
        ) { url, error in
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

      let items = URLComponents(url: finalURL, resolvingAgainstBaseURL: false)?.queryItems ?? []

      // Validate state before trusting anything else in the URL.
      guard items.first(where: { $0.name == "state" })?.value == state else {
        throw NSError(
          domain: "AuthService", code: -1,
          userInfo: [NSLocalizedDescriptionKey: "GitHub OAuth state mismatch"])
      }

      if let errorCode = items.first(where: { $0.name == "error" })?.value {
        let description =
          items.first(where: { $0.name == "error_description" })?.value ?? errorCode
        throw NSError(
          domain: "AuthService", code: -1,
          userInfo: [NSLocalizedDescriptionKey: "GitHub sign-in failed: \(description)"])
      }

      guard let accessToken = items.first(where: { $0.name == "access_token" })?.value else {
        throw NSError(
          domain: "AuthService", code: -1,
          userInfo: [NSLocalizedDescriptionKey: "Callback missing access_token"])
      }

      let credential = GitHubAuthProvider.credential(withToken: accessToken)
      let result = try await resolvedAuth.signIn(with: credential)
      currentUser = result.user

      if persistCredentials, let token = try? await result.user.getIDToken() {
        appState.saveSecure(token, forKey: "firebase_id_token")
      }
    } catch let error as ASWebAuthenticationSessionError where error.code == .canceledLogin {
      return
    } catch {
      errorMessage = firebaseErrorMessage(error)
    }
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

  // MARK: - Profile mutations

  /// Refresh the locally-cached Firebase User from the server. Call
  /// before opening the profile UI so cross-session changes (name
  /// edits, unlinked providers, newly-verified email) show up.
  func refreshUser() async {
    guard let user = resolvedAuth.currentUser else { return }
    try? await user.reload()
    currentUser = resolvedAuth.currentUser
  }

  /// Update the displayName on the current user's Firebase profile.
  /// Throws a user-facing message on failure.
  func updateDisplayName(_ name: String) async throws {
    guard let user = resolvedAuth.currentUser else {
      throw ProfileError.notSignedIn
    }
    let req = user.createProfileChangeRequest()
    req.displayName = name
    do {
      try await req.commitChanges()
      try await user.reload()
      currentUser = resolvedAuth.currentUser
      profileRevision &+= 1
    } catch {
      throw ProfileError.message(firebaseErrorMessage(error))
    }
  }

  /// Set the photo URL on the current user's Firebase profile. Pass
  /// an empty string or `nil` to clear.
  ///
  /// Setting to nil via `UserProfileChangeRequest.photoURL = nil`
  /// doesn't actually delete the field on the server — the SDK
  /// updates the local User but doesn't emit the `deleteAttribute`
  /// payload the Identity Toolkit REST API needs. `user.reload()`
  /// then restores the old URL from the server. Confirmed
  /// empirically via unified-log traces. So for the clear case we
  /// call the REST API directly with `deleteAttribute: ["PHOTO_URL"]`.
  func setPhotoURL(_ urlString: String?) async throws {
    guard let user = resolvedAuth.currentUser else {
      throw ProfileError.notSignedIn
    }
    let newURL = urlString.flatMap { $0.isEmpty ? nil : URL(string: $0) }

    do {
      if let newURL = newURL {
        let req = user.createProfileChangeRequest()
        req.photoURL = newURL
        try await req.commitChanges()
      } else {
        try await deletePhotoURLViaRESTAPI(user: user)
      }
      try await user.reload()
      currentUser = resolvedAuth.currentUser
      profileRevision &+= 1
    } catch {
      throw ProfileError.message(firebaseErrorMessage(error))
    }
  }

  /// Calls Identity Toolkit's `accounts:update` endpoint with
  /// `deleteAttribute: ["PHOTO_URL"]`. The iOS SDK's
  /// `UserProfileChangeRequest.photoURL = nil` + `commitChanges()`
  /// clears the field client-side but doesn't include
  /// `deleteAttribute` in the request payload, so the server keeps
  /// the old URL and `reload()` restores it. Bypassing the SDK for
  /// the clear case is the only way to actually delete.
  private func deletePhotoURLViaRESTAPI(user: User) async throws {
    let idToken = try await user.getIDToken()
    guard let apiKey = FirebaseApp.app()?.options.apiKey, !apiKey.isEmpty else {
      throw ProfileError.message("Firebase API key is not configured.")
    }

    let endpoint = URL(
      string: "https://identitytoolkit.googleapis.com/v1/accounts:update?key=\(apiKey)")!
    var request = URLRequest(url: endpoint)
    request.httpMethod = "POST"
    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    let payload: [String: Any] = [
      "idToken": idToken,
      "deleteAttribute": ["PHOTO_URL"],
    ]
    request.httpBody = try JSONSerialization.data(withJSONObject: payload)

    let (_, response) = try await URLSession.shared.data(for: request)
    guard let http = response as? HTTPURLResponse, http.statusCode == 200 else {
      throw ProfileError.message("Failed to remove photo.")
    }
  }

  /// Send a verification email to the current user's email address.
  func sendVerificationEmail() async throws {
    guard let user = resolvedAuth.currentUser else {
      throw ProfileError.notSignedIn
    }
    do {
      try await user.sendEmailVerification()
    } catch {
      throw ProfileError.message(firebaseErrorMessage(error))
    }
  }

  /// Delete the current user's Firebase account. Throws
  /// `ProfileError.requiresRecentLogin` when Firebase rejects the
  /// request due to a stale session; the caller prompts the user to
  /// re-auth and retry. In Pass 1 we surface this as a message.
  func deleteAccount() async throws {
    guard let user = resolvedAuth.currentUser else {
      throw ProfileError.notSignedIn
    }
    do {
      try await user.delete()
      currentUser = nil
      if persistCredentials {
        appState.deleteSecure(forKey: "firebase_id_token")
        appState.deleteSecure(forKey: "firebase_refresh_token")
      }
    } catch {
      let ns = error as NSError
      if ns.domain == AuthErrorDomain,
         AuthErrorCode(rawValue: ns.code) == .requiresRecentLogin {
        throw ProfileError.requiresRecentLogin
      }
      throw ProfileError.message(firebaseErrorMessage(error))
    }
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
  /// These strings MUST match the constants in core/auth/auth_constants.h auth_error namespace
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
      return "Incorrect email or password."
    case .emailAlreadyInUse:
      return "An account with this email already exists."
    case .accountExistsWithDifferentCredential:
      return
        "This email is already linked to a different sign-in method. Sign in with that method first, then link the new provider from your profile."
    case .weakPassword:
      return "Password is too weak. Use at least 6 characters."
    case .networkError:
      return "Network error. Check your connection."
    case .tooManyRequests:
      return "Too many attempts. Try again later."
    case .operationNotAllowed:
      return "This sign-in provider is not enabled."
    default:
      return "Something went wrong. Please try again."
    }
  }
}

/// User-facing errors thrown by profile mutation methods. `message`
/// already contains a localized, user-appropriate string. The re-auth
/// case is surfaced distinctly so callers can route into the re-auth
/// flow when we add it (Pass 2).
enum ProfileError: Error {
  case notSignedIn
  case requiresRecentLogin
  case message(String)

  var userMessage: String {
    switch self {
    case .notSignedIn: return "Not signed in"
    case .requiresRecentLogin:
      return "For your security, please sign out and sign in again before making this change."
    case .message(let m): return m
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
