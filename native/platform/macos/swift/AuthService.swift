import Foundation
import AppKit
import AuthenticationServices
import CryptoKit
import FirebaseCore
import FirebaseAuth

/// Manages authentication state using Firebase Apple SDK.
/// Observable by SwiftUI views for reactive auth state updates.
@Observable
class AuthService: NSObject {
    static let shared = AuthService()

    var currentUser: User?
    var isSignedIn: Bool { currentUser != nil }
    var errorMessage: String?
    private var isOAuthInProgress = false

    private var authStateHandle: AuthStateDidChangeListenerHandle?
    private let appState = AppStateBridge.shared()!

    private override init() {
        super.init()
    }

    /// Configure Firebase and start listening for auth state changes.
    /// Call once at app launch (AppDelegate).
    func configure() {
        FirebaseApp.configure()

        authStateHandle = Auth.auth().addStateDidChangeListener { [weak self] _, user in
            self?.currentUser = user
        }
    }

    // MARK: - Email/Password

    func signIn(email: String, password: String) async {
        errorMessage = nil
        do {
            let result = try await Auth.auth().signIn(withEmail: email, password: password)
            currentUser = result.user

            // Save token for session restore.
            if let token = try? await result.user.getIDToken() {
                appState.saveSecure(token, forKey: "firebase_id_token")
            }
        } catch {
            errorMessage = firebaseErrorMessage(error)
        }
    }

    func createAccount(email: String, password: String, displayName: String) async {
        errorMessage = nil
        do {
            let result = try await Auth.auth().createUser(withEmail: email, password: password)

            // Set display name.
            let changeRequest = result.user.createProfileChangeRequest()
            changeRequest.displayName = displayName
            try await changeRequest.commitChanges()

            // Reload to get updated profile.
            try await result.user.reload()
            currentUser = Auth.auth().currentUser

            if let token = try? await result.user.getIDToken() {
                appState.saveSecure(token, forKey: "firebase_id_token")
            }
        } catch {
            errorMessage = firebaseErrorMessage(error)
        }
    }

    // MARK: - Google Sign-In (via ASWebAuthenticationSession)

    private let googleClientID = "45920224787-gb662gbotfv763cqjis53748ctgigncl.apps.googleusercontent.com"

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
            let authResult = try await Auth.auth().signIn(with: credential)
            currentUser = authResult.user

            if let token = try? await authResult.user.getIDToken() {
                appState.saveSecure(token, forKey: "firebase_id_token")
            }
        } catch let error as ASWebAuthenticationSessionError where error.code == .canceledLogin {
            // User canceled the browser sheet — not an error.
            return
        } catch {
            errorMessage = firebaseErrorMessage(error)
        }
    }

    private func performGoogleOAuth() async throws -> (idToken: String, accessToken: String) {
        let nonce = UUID().uuidString
        let codeVerifier = generateCodeVerifier()
        let codeChallenge = generateCodeChallenge(from: codeVerifier)

        var components = URLComponents(string: "https://accounts.google.com/o/oauth2/v2/auth")!
        components.queryItems = [
            URLQueryItem(name: "client_id", value: googleClientID),
            URLQueryItem(name: "redirect_uri", value: "com.googleusercontent.apps.45920224787-gb662gbotfv763cqjis53748ctgigncl:/oauth2callback"),
            URLQueryItem(name: "response_type", value: "code"),
            URLQueryItem(name: "scope", value: "openid email profile"),
            URLQueryItem(name: "code_challenge", value: codeChallenge),
            URLQueryItem(name: "code_challenge_method", value: "S256"),
            URLQueryItem(name: "state", value: nonce),
        ]

        let authURL = components.url!
        let callbackScheme = "com.googleusercontent.apps.45920224787-gb662gbotfv763cqjis53748ctgigncl"

        let callbackURL = try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<URL, Error>) in
            let session = ASWebAuthenticationSession(url: authURL, callbackURLScheme: callbackScheme) { url, error in
                if let error = error {
                    continuation.resume(throwing: error)
                } else if let url = url {
                    continuation.resume(returning: url)
                } else {
                    continuation.resume(throwing: NSError(domain: "AuthService", code: -1, userInfo: [NSLocalizedDescriptionKey: "No callback URL received"]))
                }
            }
            session.presentationContextProvider = self
            session.prefersEphemeralWebBrowserSession = false
            session.start()
        }

        // Extract auth code from callback URL.
        guard let queryItems = URLComponents(url: callbackURL, resolvingAgainstBaseURL: false)?.queryItems,
              let code = queryItems.first(where: { $0.name == "code" })?.value else {
            throw NSError(domain: "AuthService", code: -1, userInfo: [NSLocalizedDescriptionKey: "No auth code in callback"])
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
        let json = try JSONSerialization.jsonObject(with: data) as! [String: Any]

        guard let idToken = json["id_token"] as? String,
              let accessToken = json["access_token"] as? String else {
            throw NSError(domain: "AuthService", code: -1, userInfo: [NSLocalizedDescriptionKey: "Failed to get tokens from Google"])
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
            try Auth.auth().signOut()
            currentUser = nil
            appState.deleteSecure(forKey: "firebase_id_token")
            appState.deleteSecure(forKey: "firebase_refresh_token")
        } catch {
            errorMessage = "Failed to sign out: \(error.localizedDescription)"
        }
    }

    // MARK: - Sign Out + Error Mapping

    /// Maps Firebase errors to user-facing messages.
    /// These strings MUST match the constants in core/auth_state.h auth_error namespace
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
