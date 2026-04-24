import Foundation
import Observation

/// Placeholder organization directory + current-selection state.
///
/// The real implementation will front-end a Firestore-backed
/// service that lists the orgs the signed-in user is a member of
/// and the org currently active for requests (RBAC, data scoping,
/// etc.). This fake fills in for UI work — the sidebar profile bar
/// and its menu can render final-looking content while the server
/// side is still being designed.
///
/// Shared singleton so that ProfileBar and anything that needs the
/// current org name (detail headers, request scoping, etc.) all
/// read from the same source. `@Observable` makes the rendered
/// name refresh automatically when `current` flips.
@Observable
@MainActor
final class OrgDirectory {
    static let shared = OrgDirectory()

    struct Org: Identifiable, Hashable {
        let id: String
        let name: String
    }

    private(set) var all: [Org]
    private(set) var current: Org

    private init() {
        let seeded = [
            Org(id: "acme", name: "Acme Inc"),
            Org(id: "widgets", name: "Widgets Corp"),
            Org(id: "personal", name: "Personal"),
        ]
        self.all = seeded
        self.current = seeded.first!
    }

    /// Switch the active org. Unknown IDs are no-ops.
    func switchTo(_ id: String) {
        guard let next = all.first(where: { $0.id == id }) else { return }
        current = next
    }
}
