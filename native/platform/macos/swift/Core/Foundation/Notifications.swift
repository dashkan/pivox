import Foundation

/// Cross-feature notification names. Anything posted by one feature
/// and observed by another lives here so neither side has to import
/// the other — observers and posters depend on `Core/` only and the
/// dependency-direction rule (see `AGENTS.md`) stays satisfied.
///
/// Feature-internal notifications (e.g., `aiChatFocusRequested` for
/// AIChat's intra-component coordination) stay in their feature
/// folder since no other feature observes them.
extension Notification.Name {
    /// Posted on the main actor by `AuthService.signOut` after the
    /// underlying Firebase sign-out completes. Observers should tear
    /// down any auth-bound resources (gRPC channels keyed to the
    /// previous user's ID token, cached per-user state, in-flight
    /// requests, etc.) on receipt.
    ///
    /// Observers receive the notification on the main thread (post is
    /// synchronous and `signOut` is `@MainActor`). Use
    /// `Task { @MainActor in … }` or `MainActor.assumeIsolated { … }`
    /// inside the observer to call back into `@MainActor`-isolated
    /// state.
    public static let userDidSignOut = Notification.Name("pivox.auth.userDidSignOut")
}
