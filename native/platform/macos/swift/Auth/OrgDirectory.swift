import Foundation
import Observation
import PivoxModels

/// Backend-backed organization directory + current-selection state.
///
/// Loaded after sign-in via `bootstrap()`. The cloud `ListOrganizations`
/// RPC scopes to the caller's account (server filters by membership),
/// so the result is exactly the orgs the signed-in user can act in.
///
/// Loading lifecycle:
///   - `.idle`     → before sign-in or after sign-out
///   - `.loading`  → bootstrap in flight
///   - `.ready`    → at least one membership; `current` is a member org
///   - `.empty`    → signed in but memberless → routed to onboarding
///   - `.error`    → bootstrap failed; surfaced to UI
///
/// `@Observable` so SwiftUI views (ProfileBar, ContentView routing)
/// react automatically to state changes.
@Observable
@MainActor
final class OrgService {
    static let shared = OrgService()

    enum LoadState {
        case idle
        case loading
        case ready
        case empty
        case error(String)
    }

    struct Org: Identifiable, Hashable {
        /// Stable resource ID — the trailing segment of `name`,
        /// e.g. "acme" for "organizations/acme".
        let id: String
        /// Full resource name, e.g. "organizations/acme". This is
        /// what downstream services (AIChat, etc.) scope requests by.
        let resourceName: String
        let displayName: String
    }

    private(set) var state: LoadState = .idle
    private(set) var all: [Org] = []
    /// The currently-active org. `nil` until `bootstrap()` succeeds
    /// with at least one membership. UI should not render org-scoped
    /// surfaces while this is `nil`.
    private(set) var current: Org?

    private var client: OrgsClient?
    private let appState = AppStateBridge.shared()
    private static let selectedOrgKey = "selected_org_id"

    private init() {
        // Drop cached memberships on sign-out — the next sign-in's
        // user has their own org list. AuthService posts
        // `.userDidSignOut`; we observe and `reset()`.
        NotificationCenter.default.addObserver(
            forName: .userDidSignOut, object: nil, queue: .main
        ) { [weak self] _ in
            // Observer closure isn't @MainActor-isolated by default;
            // hop into the actor before touching state.
            Task { @MainActor [weak self] in
                self?.reset()
            }
        }
    }

    /// Call after a successful sign-in. Fetches memberships and
    /// promotes the persisted (or first) org to `current`. Idempotent
    /// — re-bootstrapping after `.ready` is a no-op; call `reload()`
    /// to force a refresh (e.g. after creating an org).
    func bootstrap() async {
        if case .ready = state { return }
        if case .loading = state { return }
        await reload()
    }

    /// Force a refresh from the server. Used after creating an org or
    /// when the user retries from an error state.
    func reload() async {
        state = .loading
        do {
            let client = try resolveClient()
            let orgs = try await client.listOrganizations()
            let mapped = orgs.map(Self.mapOrg)
            all = mapped
            if mapped.isEmpty {
                current = nil
                state = .empty
                return
            }
            // Restore persisted selection if still a member, else
            // pick the first org. The directory order is server-
            // chosen (id ASC); fine as a default.
            let savedID = appState.loadString(forKey: Self.selectedOrgKey) ?? ""
            current = mapped.first { $0.id == savedID } ?? mapped.first
            state = .ready
        } catch {
            current = nil
            state = .error(Self.userFacing(error))
        }
    }

    /// Switch the active org. Unknown IDs are no-ops. Persists the
    /// new selection so it survives relaunches.
    func switchTo(_ id: String) {
        guard let next = all.first(where: { $0.id == id }) else { return }
        current = next
        appState.save(id, forKey: Self.selectedOrgKey)
    }

    /// Create an org via the cloud and adopt it as `current`.
    /// Surfaces a user-facing message on failure; throws so the
    /// onboarding form can drive its own error state.
    func create(displayName: String, organizationID: String) async throws {
        let client = try resolveClient()
        let created = try await client.createOrganization(
            displayName: displayName,
            organizationID: organizationID
        )
        let org = Self.mapOrg(created)
        all.append(org)
        current = org
        appState.save(org.id, forKey: Self.selectedOrgKey)
        state = .ready
    }

    /// Tear down on sign-out. Cancels the gRPC channel and clears
    /// state so a future sign-in re-bootstraps cleanly with the new
    /// user's memberships.
    func reset() {
        client?.cancel()
        client = nil
        all = []
        current = nil
        state = .idle
    }

    // MARK: -

    private func resolveClient() throws -> OrgsClient {
        if let client = client { return client }
        let new = try OrgsClient()
        client = new
        return new
    }

    private static func mapOrg(_ pb: Pivox_Api_V1_Organization) -> Org {
        // Resource name is "organizations/<id>"; the id is the
        // trailing segment.
        let id = pb.name.split(separator: "/").last.map(String.init) ?? pb.name
        return Org(
            id: id,
            resourceName: pb.name,
            displayName: pb.displayName.isEmpty ? id : pb.displayName
        )
    }

    private static func userFacing(_ error: Error) -> String {
        // Generic fallback — Firebase/auth errors are mapped inside
        // FirebaseAuthInterceptor and surface here as
        // ChatClientError; everything else is a network/server fault.
        if let chatErr = error as? ChatClientError {
            return chatErr.description
        }
        return "Couldn't load your organizations. Try again."
    }
}

// Backwards-compatible alias for the previous placeholder type. Keeps
// existing call sites (ProfileBar) working without churn.
typealias OrgDirectory = OrgService
