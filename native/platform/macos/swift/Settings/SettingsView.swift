import SwiftUI

/// Content-only view for the Settings window. The tab strip lives
/// above us in `SettingsWindowController`, rendered by an
/// `NSToolbar` so we get the proper macOS "Preferences" look
/// (icon-over-label toolbar items, colored on selection) that
/// SwiftUI's `TabView` doesn't provide outside a `Settings {}`
/// scene. This view just renders whichever page matches the
/// shared `SettingsSelection.tab`.
struct SettingsView: View {
    @Bindable var selection: SettingsSelection
    private var auth = AuthService.shared

    init(selection: SettingsSelection) {
        self.selection = selection
    }

    enum Tab: String, Hashable, CaseIterable, Identifiable {
        case general
        case ai
        case account
        case security

        var id: String { rawValue }

        var label: String {
            switch self {
            case .general: return "General"
            case .ai: return "AI"
            case .account: return "Account"
            case .security: return "Security"
            }
        }

        var iconSymbol: String {
            switch self {
            case .general: return "gearshape"
            case .ai: return "sparkles"
            case .account: return "person.crop.circle"
            case .security: return "lock.shield"
            }
        }
    }

    var body: some View {
        Group {
            switch selection.tab {
            case .general: GeneralPage()
            case .ai: AIPage()
            case .account: AccountPage()
            case .security: SecurityPage()
            }
        }
        .task {
            // Refresh the user once when the Settings window opens
            // so Account / Security reflect cross-session changes
            // (e.g. a factor unenrolled on another device).
            await auth.refreshUser()
        }
    }
}
