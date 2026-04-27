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

  var currentUser: User? {
    didSet {
      // Flush cached avatar images whenever the user changes (sign
      // in, sign out, account switch). Prevents the next account
      // from briefly rendering the previous user's photo.
      if oldValue?.uid != currentUser?.uid {
        AvatarImageCache.shared.clear()
      }
    }
  }
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

  /// Non-nil while a sign-in attempt has succeeded past the first
  /// factor but still needs a TOTP code. The UI observes this and
  /// swaps to a 6-digit input screen; `completeMFASignIn(code:)`
  /// finishes the flow, `cancelMFASignIn()` discards it.
  ///
  /// Lives at the service layer rather than being thrown out of
  /// `signIn(...)` because the sign-in methods here set
  /// `errorMessage` instead of throwing, and the MFA-required case
  /// is *not* an error — it's a continuation point.
  var pendingMFAResolver: MultiFactorResolver?

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
      if let resolver = multiFactorResolver(from: error) {
        pendingMFAResolver = resolver
        return
      }
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
      if let resolver = multiFactorResolver(from: error) {
        pendingMFAResolver = resolver
        return
      }
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
  // Flow:
  //   1. ASWebAuthenticationSession opens `<broker>/api/oauth/github/start`
  //      on our start-app backend.
  //   2. The broker 302's to github.com/login/oauth/authorize with its
  //      own client_id and redirect_uri (the broker's own callback).
  //      Client_id + client_secret live server-side only — no secrets
  //      in this binary.
  //   3. GitHub redirects to the broker's callback with a code.
  //      Broker exchanges code→token server-side, then 302's to
  //      `pivox://auth-complete#provider=github&kind=github_access_token&token=…`.
  //   4. ASWebAuthenticationSession intercepts the `pivox://` URL
  //      because it matches our registered callback scheme and hands
  //      the full URL back to us here.
  //   5. We parse the access_token from the URL fragment and finish
  //      the Firebase sign-in with `signInWithCredential`.
  //
  // The broker handles state (CSRF) signing and verification itself;
  // native doesn't need to generate or validate its own nonce.

  /// Base URL for the Pivox backend. Hardcoded for now — swap when
  /// the host changes, rebuild, ship.
  private static let brokerBaseURL = "https://pivox.ngrok.app"

  private let githubReturnURL = "pivox://auth-complete"
  private let githubCallbackScheme = "pivox"

  func signInWithGitHub() async {
    guard !isOAuthInProgress else { return }
    isOAuthInProgress = true
    defer { isOAuthInProgress = false }
    errorMessage = nil

    do {
      let accessToken = try await performGitHubOAuth()
      let credential = GitHubAuthProvider.credential(withToken: accessToken)
      let result = try await resolvedAuth.signIn(with: credential)
      currentUser = result.user

      if persistCredentials, let token = try? await result.user.getIDToken() {
        appState.saveSecure(token, forKey: "firebase_id_token")
      }
    } catch let error as ASWebAuthenticationSessionError where error.code == .canceledLogin {
      return
    } catch {
      if let resolver = multiFactorResolver(from: error) {
        pendingMFAResolver = resolver
        return
      }
      errorMessage = firebaseErrorMessage(error)
    }
  }

  /// Factored GitHub broker flow. Opens the broker in an
  /// ASWebAuthenticationSession, validates the returned fragment,
  /// and yields the GitHub access token. Shared between initial
  /// sign-in and the reauth path used by sensitive profile ops.
  private func performGitHubOAuth() async throws -> String {
    guard
      let encodedReturn = githubReturnURL.addingPercentEncoding(
        withAllowedCharacters: .urlQueryAllowed),
      let startURL = URL(
        string: "\(Self.brokerBaseURL)/api/oauth/github/start?return=\(encodedReturn)")
    else {
      throw NSError(
        domain: "AuthService", code: -1,
        userInfo: [NSLocalizedDescriptionKey: "Failed to build GitHub broker URL"])
    }

    let finalURL = try await withCheckedThrowingContinuation {
      (continuation: CheckedContinuation<URL, Error>) in
      let session = ASWebAuthenticationSession(
        url: startURL,
        callbackURLScheme: githubCallbackScheme
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

    let fragment = finalURL.fragment ?? ""
    let items = URLComponents(string: "?\(fragment)")?.queryItems ?? []

    if let errorCode = items.first(where: { $0.name == "error" })?.value {
      let description =
        items.first(where: { $0.name == "error_description" })?.value ?? errorCode
      throw NSError(
        domain: "AuthService", code: -1,
        userInfo: [NSLocalizedDescriptionKey: "GitHub sign-in failed: \(description)"])
    }

    guard items.first(where: { $0.name == "provider" })?.value == "github" else {
      throw NSError(
        domain: "AuthService", code: -1,
        userInfo: [NSLocalizedDescriptionKey: "Broker returned wrong provider"])
    }
    guard items.first(where: { $0.name == "kind" })?.value == "github_access_token" else {
      throw NSError(
        domain: "AuthService", code: -1,
        userInfo: [NSLocalizedDescriptionKey: "Broker returned unexpected credential kind"])
    }
    guard let accessToken = items.first(where: { $0.name == "token" })?.value else {
      throw NSError(
        domain: "AuthService", code: -1,
        userInfo: [NSLocalizedDescriptionKey: "Callback missing access_token"])
    }
    return accessToken
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
      throw mapToProfileError(error)
    }
  }

  // MARK: - Password

  /// Whether the user has an email/password credential linked. Used
  /// by the Security page to pick between "Set password" (OAuth-only
  /// users) and "Change password" (users who already have one).
  var hasPasswordProvider: Bool {
    (currentUser?.providerData ?? []).contains { $0.providerID == "password" }
  }

  /// Add an email/password credential to the current user. This is
  /// the path for accounts created via OAuth (Google/GitHub) — they
  /// don't have a password yet, and this links one as an additional
  /// sign-in method. Sends a verification email after linking so the
  /// new credential is confirmed.
  func setPassword(_ newPassword: String) async throws {
    guard let user = resolvedAuth.currentUser, let email = user.email else {
      throw ProfileError.notSignedIn
    }
    let credential = EmailAuthProvider.credential(withEmail: email, password: newPassword)
    do {
      _ = try await user.link(with: credential)
      try await user.sendEmailVerification()
      try await user.reload()
      currentUser = resolvedAuth.currentUser
      profileRevision &+= 1
    } catch {
      throw mapToProfileError(error)
    }
  }

  /// Change the password for a user that already has an
  /// email/password credential. Re-authenticates with the current
  /// password first because Firebase requires recent auth for
  /// password changes; if that fails surface the underlying error
  /// (typically "wrong password" or `requiresRecentLogin`).
  func changePassword(currentPassword: String, newPassword: String) async throws {
    guard let user = resolvedAuth.currentUser, let email = user.email else {
      throw ProfileError.notSignedIn
    }
    let credential = EmailAuthProvider.credential(withEmail: email, password: currentPassword)
    do {
      _ = try await user.reauthenticate(with: credential)
      try await user.updatePassword(to: newPassword)
    } catch {
      throw mapToProfileError(error)
    }
  }

  // MARK: - TOTP (Two-factor authentication)

  /// Firebase's factor ID for TOTP. The iOS SDK exposes this as
  /// `PhoneMultiFactorInfo.FIRTOTPMultiFactorID`, an awkward name
  /// inherited from the Obj-C bridge; the raw string is a stable
  /// Firebase contract ("totp") so we use it directly.
  private static let totpFactorID = "totp"

  /// Completes a sign-in that paused on the second-factor step.
  /// Uses the first TOTP hint in the resolver — we only enroll one
  /// factor, so the array has exactly one element in practice. The
  /// resolver is cleared on success so the LoginView falls back to
  /// the normal form.
  func completeMFASignIn(code: String) async throws {
    guard let resolver = pendingMFAResolver else {
      throw ProfileError.message("No sign-in in progress.")
    }
    guard let hint = resolver.hints.first(where: {
      $0.factorID == Self.totpFactorID
    }) else {
      throw ProfileError.message("No authenticator is enrolled on this account.")
    }
    let assertion = TOTPMultiFactorGenerator.assertionForSignIn(
      withEnrollmentID: hint.uid, oneTimePassword: code)
    do {
      let result = try await resolver.resolveSignIn(with: assertion)
      currentUser = result.user
      pendingMFAResolver = nil
      if persistCredentials, let token = try? await result.user.getIDToken() {
        appState.saveSecure(token, forKey: "firebase_id_token")
      }
    } catch {
      // Leave the resolver in place so the user can retry with a
      // fresh code — TOTP codes tick every 30s and a wrong entry
      // shouldn't unwind them back to the email/password form.
      throw mapToProfileError(error)
    }
  }

  /// Drop the pending MFA sign-in. No-op if there's none in flight.
  func cancelMFASignIn() {
    pendingMFAResolver = nil
  }

  /// Pulls a `MultiFactorResolver` out of a sign-in error when
  /// Firebase returned `secondFactorRequired`. Returns nil for any
  /// other error — caller should treat those as real failures.
  private func multiFactorResolver(from error: Error) -> MultiFactorResolver? {
    let ns = error as NSError
    guard
      ns.domain == AuthErrorDomain,
      AuthErrorCode(rawValue: ns.code) == .secondFactorRequired,
      let resolver = ns.userInfo[AuthErrorUserInfoMultiFactorResolverKey]
        as? MultiFactorResolver
    else {
      return nil
    }
    return resolver
  }

  /// Async wrapper around `multiFactor.getSessionWithCompletion`.
  /// Unlike the other MFA methods, the iOS SDK's
  /// `getSessionWithCompletion:` takes a nullable completion block,
  /// so the Swift async bridge doesn't generate an async variant.
  /// Wrapping manually here keeps the call sites async/await-clean.
  private static func getMultiFactorSession(for user: User) async throws -> MultiFactorSession {
    try await withCheckedThrowingContinuation { continuation in
      user.multiFactor.getSessionWithCompletion { session, error in
        if let session {
          continuation.resume(returning: session)
        } else {
          continuation.resume(throwing: error ?? ProfileError.message("Failed to start enrollment."))
        }
      }
    }
  }

  /// Whether a TOTP second factor is enrolled. Firebase's
  /// `multiFactor` is a non-optional member on `User` but the
  /// `enrolledFactors` array may still throw on a stale user object
  /// during a reload window; we defensively treat any access failure
  /// as "not enrolled".
  var isMfaEnrolled: Bool {
    guard let user = currentUser else { return false }
    return user.multiFactor.enrolledFactors.contains {
      $0.factorID == Self.totpFactorID
    }
  }

  /// In-flight TOTP enrollment secret. Held between
  /// `startTotpEnrollment` and `verifyTotpEnrollment` so the verify
  /// assertion can reference the same secret the QR code was built
  /// from. Cleared on cancel / success.
  private var totpSecret: TOTPSecret?

  /// Public DTO returned from `startTotpEnrollment`. Carries just
  /// the info the UI needs to render the QR screen.
  struct TOTPEnrollmentContext {
    /// `otpauth://` URL suitable for rendering as a QR code.
    let qrCodeURL: String
    /// Base32-encoded shared secret — shown alongside the QR for
    /// users whose authenticator app takes manual entry.
    let sharedSecret: String
  }

  /// Begin TOTP enrollment. Firebase requires email verification
  /// before a second factor can be added (otherwise an attacker who
  /// gains the account could enroll their own 2FA and lock the
  /// owner out). Returns the context the UI needs to show a QR
  /// code; the caller must follow up with `verifyTotpEnrollment` or
  /// `cancelTotpEnrollment`.
  func startTotpEnrollment() async throws -> TOTPEnrollmentContext {
    guard let user = resolvedAuth.currentUser else {
      throw ProfileError.notSignedIn
    }
    guard user.isEmailVerified else {
      throw ProfileError.message(
        "Verify your email before enabling two-factor authentication.")
    }
    do {
      let session = try await Self.getMultiFactorSession(for: user)
      let secret = try await TOTPMultiFactorGenerator.generateSecret(with: session)
      self.totpSecret = secret
      return TOTPEnrollmentContext(
        qrCodeURL: secret.generateQRCodeURL(
          withAccountName: user.email ?? "",
          issuer: "Pivox"),
        sharedSecret: secret.sharedSecretKey())
    } catch {
      throw mapToProfileError(error)
    }
  }

  /// Complete TOTP enrollment with the 6-digit code from the user's
  /// authenticator app. `startTotpEnrollment` must have been called
  /// first; throws if there's no secret in flight.
  func verifyTotpEnrollment(code: String) async throws {
    guard let user = resolvedAuth.currentUser else {
      throw ProfileError.notSignedIn
    }
    guard let secret = totpSecret else {
      throw ProfileError.message("No enrollment in progress.")
    }
    let assertion = TOTPMultiFactorGenerator.assertionForEnrollment(
      with: secret, oneTimePassword: code)
    do {
      try await user.multiFactor.enroll(with: assertion, displayName: "Authenticator app")
      totpSecret = nil
      try await user.reload()
      currentUser = resolvedAuth.currentUser
      profileRevision &+= 1
    } catch {
      throw mapToProfileError(error)
    }
  }

  /// Discard any in-flight enrollment secret. Safe to call from any
  /// state — no-op if the user never started enrollment.
  func cancelTotpEnrollment() {
    totpSecret = nil
  }

  /// Remove the currently-enrolled TOTP factor. Firebase requires a
  /// recent login for this operation; stale sessions throw
  /// `ProfileError.requiresRecentLogin` which the caller should
  /// handle by prompting for re-auth.
  func unenrollTotp() async throws {
    guard let user = resolvedAuth.currentUser else {
      throw ProfileError.notSignedIn
    }
    do {
      // Refresh before reading enrolledFactors. Firebase's local
      // user object can lag after a reauthenticate-via-OAuth round
      // trip — `enrolledFactors` returns an empty array until the
      // next reload, which would make the "No authenticator is
      // enrolled" guard fire even though the server-side factor
      // is still there.
      try await user.reload()
      guard let factor = user.multiFactor.enrolledFactors.first(where: {
        $0.factorID == Self.totpFactorID
      }) else {
        throw ProfileError.message("No authenticator is enrolled.")
      }
      try await user.multiFactor.unenroll(with: factor)
      try await user.reload()
      currentUser = resolvedAuth.currentUser
      profileRevision &+= 1
    } catch let profileError as ProfileError {
      throw profileError
    } catch {
      throw mapToProfileError(error)
    }
  }

  // MARK: - Reauthentication

  /// Re-establish a "recent" auth state for sensitive ops. Firebase
  /// gates password change, email change, delete, and second-factor
  /// enroll/unenroll on a fresh session (typically <5 minutes since
  /// last auth), throwing `auth/requires-recent-login` otherwise.
  /// Each linked provider needs its own reauth path — callers pick
  /// the one matching the method the user wants to use now.
  ///
  /// ## MFA-enrolled accounts
  /// When the account has TOTP enrolled, Firebase requires the
  /// reauth flow itself to include the second factor. The first-
  /// factor reauth (password or OAuth) throws
  /// `secondFactorRequired`; we capture the resolver into
  /// `pendingReauthResolver` and throw `.requiresSecondFactor` so
  /// the UI can prompt for the TOTP code. The caller then calls
  /// `completeReauthSecondFactor(code:)` to finish the flow.

  /// Resolver in flight while a reauth is paused on its second
  /// factor. The UI watches `requiresSecondFactor` errors and
  /// drives the OTP step; we hold the resolver here so the second
  /// step has it available.
  private var pendingReauthResolver: MultiFactorResolver?

  func reauthenticateWithPassword(_ password: String) async throws {
    guard let user = resolvedAuth.currentUser, let email = user.email else {
      throw ProfileError.notSignedIn
    }
    let credential = EmailAuthProvider.credential(withEmail: email, password: password)
    do {
      _ = try await user.reauthenticate(with: credential)
    } catch {
      try captureReauthSecondFactorOrRethrow(error)
    }
  }

  func reauthenticateWithGoogle() async throws {
    guard let user = resolvedAuth.currentUser else {
      throw ProfileError.notSignedIn
    }
    do {
      let (idToken, accessToken) = try await performGoogleOAuth()
      let credential = GoogleAuthProvider.credential(
        withIDToken: idToken, accessToken: accessToken)
      _ = try await user.reauthenticate(with: credential)
    } catch let error as ASWebAuthenticationSessionError where error.code == .canceledLogin {
      throw ProfileError.message("Re-authentication cancelled.")
    } catch {
      try captureReauthSecondFactorOrRethrow(error)
    }
  }

  func reauthenticateWithGitHub() async throws {
    guard let user = resolvedAuth.currentUser else {
      throw ProfileError.notSignedIn
    }
    do {
      let accessToken = try await performGitHubOAuth()
      let credential = GitHubAuthProvider.credential(withToken: accessToken)
      _ = try await user.reauthenticate(with: credential)
    } catch let error as ASWebAuthenticationSessionError where error.code == .canceledLogin {
      throw ProfileError.message("Re-authentication cancelled.")
    } catch {
      try captureReauthSecondFactorOrRethrow(error)
    }
  }

  /// Inspect a reauth error: if Firebase asked for the second
  /// factor, stash the resolver and throw
  /// `.requiresSecondFactor` so the UI can drive the OTP step.
  /// Otherwise re-throw via the normal mapper.
  private func captureReauthSecondFactorOrRethrow(_ error: Error) throws {
    if let resolver = multiFactorResolver(from: error) {
      pendingReauthResolver = resolver
      throw ProfileError.requiresSecondFactor
    }
    throw mapToProfileError(error)
  }

  /// Finish a paused reauth using a TOTP code. Mirrors
  /// `completeMFASignIn(code:)` but for the reauth flow:
  /// `resolver.resolveSignIn(with:)` is the same call Firebase
  /// uses for both — internally it completes whichever flow the
  /// resolver was created for. On success the user has a fresh
  /// session and the caller can retry the original sensitive op.
  func completeReauthSecondFactor(code: String) async throws {
    guard let resolver = pendingReauthResolver else {
      throw ProfileError.message("No re-authentication in progress.")
    }
    guard let hint = resolver.hints.first(where: {
      $0.factorID == Self.totpFactorID
    }) else {
      throw ProfileError.message("No authenticator is enrolled on this account.")
    }
    let assertion = TOTPMultiFactorGenerator.assertionForSignIn(
      withEnrollmentID: hint.uid, oneTimePassword: code)
    do {
      _ = try await resolver.resolveSignIn(with: assertion)
      pendingReauthResolver = nil
    } catch {
      throw mapToProfileError(error)
    }
  }

  /// Drop any in-flight reauth resolver. Called when the user
  /// cancels the OTP step or backs out of the reauth view.
  func cancelReauthSecondFactor() {
    pendingReauthResolver = nil
  }

  // MARK: - Sign Out

  @MainActor
  func signOut() {
    errorMessage = nil
    do {
      try resolvedAuth.signOut()
      currentUser = nil
      if persistCredentials {
        appState.deleteSecure(forKey: "firebase_id_token")
        appState.deleteSecure(forKey: "firebase_refresh_token")
      }
      // Tear down auth-bound singletons. The chat client's gRPC channel
      // is bound to the previous user's ID token and must not survive
      // across user identities — leaving it up would let in-flight
      // streams keep running and any newly-signed-in user reuse the
      // channel with their token attached to the prior user's
      // long-lived connection.
      //
      // signOut is @MainActor so this call is statically guaranteed
      // to be on the main actor — no runtime assertion needed.
      AIChatService.shared.reset()
      OrgService.shared.reset()
    } catch {
      errorMessage = "Failed to sign out: \(error.localizedDescription)"
    }
  }

  // MARK: - Error Mapping

  /// Converts a raw Firebase error into a `ProfileError`, preserving
  /// the distinct `requiresRecentLogin` case so the caller can route
  /// into a re-auth flow instead of treating it as a generic failure.
  /// Any other auth-domain error is collapsed into a user-facing
  /// message via `firebaseErrorMessage`.
  private func mapToProfileError(_ error: Error) -> ProfileError {
    let ns = error as NSError
    if ns.domain == AuthErrorDomain,
       AuthErrorCode(rawValue: ns.code) == .requiresRecentLogin {
      return .requiresRecentLogin
    }
    return .message(firebaseErrorMessage(error))
  }

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
      // Fall through to Firebase's localized description for codes
      // we haven't mapped yet (especially MFA-specific ones).
      // "Something went wrong" is useless for diagnosis — the
      // underlying message ("MFA enrollment failed", "Invalid
      // verification code", etc.) is at least actionable.
      return nsError.localizedDescription
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
  /// Reauth started successfully but the account is MFA-enrolled,
  /// so Firebase wants the second-factor verification before the
  /// reauth is considered complete. Caller should drive an OTP
  /// step and finish via `AuthService.completeReauthSecondFactor`.
  case requiresSecondFactor
  case message(String)

  var userMessage: String {
    switch self {
    case .notSignedIn: return "Not signed in"
    case .requiresRecentLogin:
      return "For your security, please sign out and sign in again before making this change."
    case .requiresSecondFactor:
      return "Enter the code from your authenticator app to continue."
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
