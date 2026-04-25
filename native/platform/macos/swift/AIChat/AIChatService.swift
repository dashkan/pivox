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

    // TODO: resolve from authenticated user's org membership
    // instead of a hardcoded value.
    let orgName = "local-corp"
    private let endpoint = "localhost:50051"

    /// Long-lived `ConversationViewModel` cache keyed by
    /// conversation name. Lifting these out of the view tree is
    /// what makes dock/detach safe for in-flight requests — the
    /// streaming Task lives on the view model, and the view model
    /// lives here, so re-mounting the panel after a mode swap
    /// re-attaches to the same stream rather than tearing it down.
    ///
    /// Entries persist for the lifetime of the app session, which
    /// is fine for normal use (conversations are scoped per
    /// session). `reset()` clears them on sign-out.
    private var viewModels: [String: ConversationViewModel] = [:]

    private init() {}

    /// Idempotent connect — returns immediately if already
    /// connected. Called from the container view's `.task` so the
    /// connection lifecycle still tracks "the chat surface is on
    /// screen" without forcing every consumer to call manually.
    func connect() async {
        guard client == nil, initError == nil else { return }
        do {
            // Auth header is attached per-RPC by the shared auth
            // interceptor (see PivoxAuthBridge); ChatClient
            // construction is synchronous from our perspective.
            client = try ChatClient(endpoint: endpoint)
        } catch {
            initError = error.localizedDescription
        }
    }

    /// Get or create the view model for a conversation. Same
    /// instance returned across calls so multiple views (or the
    /// same view re-mounted after a mode swap) observe the same
    /// streaming state.
    func viewModel(for conversationName: String, isNew: Bool = false) -> ConversationViewModel? {
        if let existing = viewModels[conversationName] {
            return existing
        }
        guard let client else { return nil }
        let vm = ConversationViewModel(
            client: client, conversationName: conversationName, isNew: isNew)
        viewModels[conversationName] = vm
        return vm
    }

    /// Reset state — use only when the signed-in user changes,
    /// since the gRPC channel is bound to that user's auth token.
    func reset() {
        viewModels.values.forEach { $0.cancel() }
        viewModels.removeAll()
        client = nil
        initError = nil
    }
}
