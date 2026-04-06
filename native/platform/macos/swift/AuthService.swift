import Foundation
import FirebaseCore
import FirebaseAuth

/// Manages authentication state using Firebase Apple SDK.
/// Observable by SwiftUI views for reactive auth state updates.
@Observable
class AuthService {
    static let shared = AuthService()

    var currentUser: User?
    var isSignedIn: Bool { currentUser != nil }
    var errorMessage: String?

    private var authStateHandle: AuthStateDidChangeListenerHandle?
    private let appState = AppStateBridge.shared()!

    private init() {}

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

    // MARK: - Sign Out

    func signOut() {
        do {
            try Auth.auth().signOut()
            currentUser = nil
            appState.deleteSecure(forKey: "firebase_id_token")
            appState.deleteSecure(forKey: "firebase_refresh_token")
        } catch {
            errorMessage = "Failed to sign out: \(error.localizedDescription)"
        }
    }

    // MARK: - Error Mapping

    private func firebaseErrorMessage(_ error: Error) -> String {
        // Log the full error for debugging.
        let debugMsg = "[AuthService] Error: \(error)\nNSError: \(error as NSError)"
        try? debugMsg.write(toFile: "/tmp/pivox-auth-error.txt", atomically: true, encoding: .utf8)
        let nsError = error as NSError
        guard nsError.domain == AuthErrorDomain else {
            return error.localizedDescription
        }

        switch AuthErrorCode(rawValue: nsError.code) {
        case .invalidEmail:
            return "Invalid email address."
        case .wrongPassword:
            return "Incorrect password."
        case .userNotFound:
            return "No account found with this email."
        case .emailAlreadyInUse:
            return "An account with this email already exists."
        case .weakPassword:
            return "Password is too weak. Use at least 6 characters."
        case .networkError:
            return "Network error. Check your connection."
        case .tooManyRequests:
            return "Too many attempts. Try again later."
        default:
            return error.localizedDescription
        }
    }
}
