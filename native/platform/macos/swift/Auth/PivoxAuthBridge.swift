import FirebaseAuth
import Foundation

/// Bridges the Firebase Apple SDK's async token fetch into the shared
/// C++ gRPC auth interceptor. Called once at app startup from
/// AppDelegate, after `AuthService.shared.configure()`.
///
/// Why the indirection: Swift's coercion from `{ ... }` to a C function
/// pointer forbids capturing *any* local context — including parameters
/// indirectly referenced by an inner closure (e.g. a `Task { }` inside).
/// We keep the outermost closure capture-free by forwarding immediately
/// to a static helper, which can then spawn the Task and do real work.
enum PivoxAuthBridge {
    static func registerTokenProvider() {
        // Outer closure has zero captures — the only thing inside is a
        // call to a static method, which Swift treats as a global symbol
        // reference, not a context capture.
        pivox_auth_register_provider { ctx, completion in
            PivoxAuthBridge.beginTokenFetch(ctx: ctx, completion: completion)
        }
    }

    /// Fire-and-forget: spawns a Task that awaits Firebase's cached
    /// `getIDToken()` and then invokes the C completion trampoline.
    /// Firebase refreshes the token internally when it's close to expiry.
    private static func beginTokenFetch(
        ctx: UnsafeMutableRawPointer?,
        completion: pivox_token_completion_fn?
    ) {
        Task {
            let token: String?
            do {
                token = try await Auth.auth().currentUser?.getIDToken()
            } catch {
                let nsError = error as NSError
                NSLog("[PivoxAuth] getIDToken failed (code \(nsError.code)): \(nsError.localizedDescription)")
                if isReAuthRequired(nsError) {
                    // Server-side session is dead (revoked, disabled,
                    // deleted, mismatched). Firebase does NOT auto-
                    // sign-out on this; we have to force it so the UI's
                    // sign-in-state observer routes back to login.
                    await MainActor.run {
                        AuthService.shared.signOut()
                    }
                }
                token = nil
            }
            invokeCompletion(completion, ctx: ctx, token: token)
        }
    }

    /// Classifies a Firebase auth error as "session is dead, user must
    /// sign in again" vs transient (network, timeout, rate limit). Only
    /// the former triggers a forced sign-out; transients let the caller
    /// retry without losing the user's session.
    ///
    /// The canonical list of conditions lives in `auth_constants.h`
    /// (`pivox::auth_reauth` doc comment). Windows has an equivalent
    /// classifier mapping its Firebase SDK's error codes onto the same
    /// list — keep both sides in sync if this changes.
    private static func isReAuthRequired(_ error: NSError) -> Bool {
        guard error.domain == AuthErrors.domain else { return false }
        guard let code = AuthErrorCode(rawValue: error.code) else { return false }
        switch code {
        case .userTokenExpired,  // refresh token rejected
             .invalidUserToken,  // refresh token malformed / revoked
             .userDisabled,      // account disabled
             .userNotFound,      // account deleted
             .userMismatch,      // token belongs to a different user
             .requiresRecentLogin:  // sensitive op needs fresh login
            return true
        default:
            return false
        }
    }

    /// Invokes the C trampoline with a NUL-terminated copy of `token`.
    /// The C side synchronously copies the bytes, so `withCString`'s
    /// transient pointer is safe.
    private static func invokeCompletion(
        _ completion: pivox_token_completion_fn?,
        ctx: UnsafeMutableRawPointer?,
        token: String?
    ) {
        guard let completion else { return }
        if let token, !token.isEmpty {
            token.withCString { cString in
                completion(ctx, cString)
            }
        } else {
            completion(ctx, nil)
        }
    }
}
