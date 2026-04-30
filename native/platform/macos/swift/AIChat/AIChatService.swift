import Foundation
import Observation

/// Shared, app-lifetime owner of the AI chat's network connection.
///
/// Lives outside `AIChatContainerView` so the same `ChatClient`
/// survives when the chat surface re-mounts during a dock ↔ detach
/// switch. Without this, every mode swap would tear down and
/// rebuild the gRPC channel — a visible "Connecting…" flicker
/// every time the user clicked detach or dock.
///
/// Per-conversation UI state (current conversation name, scroll
/// position, draft) is intentionally NOT held here — those are
/// already persisted to `AppStateBridge` and restored when
/// `AIChatPanel` mounts, so per-mode reset is acceptable.
@Observable
@MainActor
final class AIChatService {
    static let shared = AIChatService()

    /// The active gRPC chat client. Nil until `connect()` has run.
    private(set) var client: ChatClient?

    /// Last initialization failure message, if any. Surfaced by
    /// the container view in place of the chat panel.
    private(set) var initError: String?

    /// Org slug used as the parent prefix when scoping AI Chat
    /// resources. Resolved from `OrgService.shared.current` —
    /// guaranteed non-nil because the chat surface only mounts inside
    /// `mainAppView`, which is only routed to once
    /// `OrgService.state == .ready`.
    var orgName: String {
        OrgService.shared.current?.id ?? ""
    }

    /// The caller's per-Pivox user UUID, read from the
    /// `pivox_user_id` Firebase ID-token custom claim. Cached after
    /// the first successful read since the value is stable for the
    /// life of the firebase_identity. Returns empty string if the
    /// claim is missing (e.g. token was issued before the blocking
    /// function was deployed) — handlers will surface
    /// PermissionDenied which the panel reports as an error.
    private var cachedPivoxUserID: String = ""
    func pivoxUserID() async -> String {
        if !cachedPivoxUserID.isEmpty { return cachedPivoxUserID }
        guard let user = AuthService.shared.currentUser else { return "" }
        do {
            let result = try await user.getIDTokenResult()
            if let uid = result.claims["pivox_user_id"] as? String, !uid.isEmpty {
                cachedPivoxUserID = uid
                return uid
            }
        } catch {
            // Token fetch failed — let callers proceed with empty
            // string; the server will reject and the user can
            // re-auth.
        }
        return ""
    }

    /// Single-slot `ConversationViewModel` cache for the
    /// currently-viewed conversation. Lifting it out of the view
    /// tree is what makes dock/detach safe for in-flight requests —
    /// the streaming Task lives on the view model, and the view
    /// model lives here, so re-mounting the panel after a mode swap
    /// re-attaches to the same stream rather than tearing it down.
    ///
    /// We intentionally keep only ONE cached VM at a time: switching
    /// to a different conversation evicts the previous cache and
    /// builds a fresh VM (which then re-fetches history from the
    /// server). This means revisiting a conversation after viewing
    /// another one always shows the latest server-side state —
    /// important for any future multi-client editing case where
    /// another device may have written into the conversation while
    /// you were away. Re-selecting the SAME conversation is a no-op.
    private var current: (name: String, vm: ConversationViewModel)?

    private init() {}

    /// Idempotent connect — returns immediately if already
    /// connected. Called from the container view's `.task` so the
    /// connection lifecycle still tracks "the chat surface is on
    /// screen" without forcing every consumer to call manually.
    func connect() async {
        guard client == nil, initError == nil else { return }
        do {
            // Auth header is attached per-RPC by ChatClient's own
            // FirebaseAuthInterceptor; construction is synchronous.
            client = try ChatClient()
        } catch {
            initError = error.localizedDescription
        }
    }

    /// Get or create the view model for a conversation.
    ///
    /// Same-name calls return the existing instance — important for
    /// dock/detach (the panel re-mounts but stays on the same
    /// conversation, so we want to re-attach to the same in-flight
    /// stream). Different-name calls evict the previous VM
    /// (canceling any in-flight stream) and build a fresh one,
    /// triggering a re-fetch when the new view's `loadHistory()`
    /// runs.
    func viewModel(for conversationName: String, isNew: Bool = false) -> ConversationViewModel? {
        if let cur = current, cur.name == conversationName {
            return cur.vm
        }
        // Different conversation — drop the previous cache.
        // `cancel()` tears down any in-flight streaming Task so the
        // gRPC call doesn't keep running for a conversation the
        // user has navigated away from.
        current?.vm.cancel()

        guard let client else { return nil }
        let vm = ConversationViewModel(
            client: client, conversationName: conversationName, isNew: isNew)
        current = (conversationName, vm)
        return vm
    }

    /// Reset state — call on sign-out (and any other point where the
    /// signed-in user changes). The gRPC channel is bound to that user's
    /// auth token and must not survive across user identities; in-flight
    /// streaming RPCs must be cancelled. Idempotent.
    func reset() {
        current?.vm.cancel()
        current = nil
        // `cancel()` tears down the run-task and begins graceful
        // shutdown of the underlying HTTP/2 channel. Without this the
        // background `runConnections()` task would leak past sign-out
        // and any persistent stream would keep running until it
        // organically completed.
        client?.cancel()
        client = nil
        initError = nil
    }
}
