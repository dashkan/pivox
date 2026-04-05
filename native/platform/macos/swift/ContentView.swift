import SwiftUI

enum AppSection: String, CaseIterable, Identifiable {
    case operator_ = "Operator"
    case library = "Library"
    case designer = "Designer"
    case engineering = "Engineering"
    case admin = "Admin"

    var id: String { rawValue }

    var icon: String {
        switch self {
        case .operator_: return "play.rectangle"
        case .library: return "photo.on.rectangle"
        case .designer: return "paintbrush"
        case .engineering: return "wrench.and.screwdriver"
        case .admin: return "gearshape"
        }
    }
}

enum SidebarItem: Hashable {
    case section(AppSection)
    case profile
}

enum AuthState {
    case loggedOut
    case loggedIn
}

struct ContentView: View {
    @State private var selectedItem: SidebarItem? = .section(.operator_)
    @State private var authState: AuthState = .loggedOut

    var body: some View {
        Group {
            switch authState {
            case .loggedOut:
                AuthRouter(onAuthenticated: { authState = .loggedIn })
            case .loggedIn:
                mainAppView
            }
        }
    }

    private var mainAppView: some View {
        NavigationSplitView {
            VStack(spacing: 0) {
                List(selection: $selectedItem) {
                    ForEach(AppSection.allCases) { section in
                        Label(section.rawValue, systemImage: section.icon)
                            .tag(SidebarItem.section(section))
                    }
                }
                .listStyle(.sidebar)

                Spacer()

                Divider()

                // Profile pinned to sidebar footer.
                List(selection: $selectedItem) {
                    Label("Profile", systemImage: "person.circle")
                        .tag(SidebarItem.profile)
                }
                .listStyle(.sidebar)
                .frame(height: 40)
            }
            .navigationSplitViewColumnWidth(min: 180, ideal: 200, max: 260)
        } detail: {
            switch selectedItem {
            case .section(let section):
                VStack {
                    Text(section.rawValue)
                        .font(.largeTitle)
                        .foregroundStyle(.secondary)
                    Text("Coming soon")
                        .foregroundStyle(.tertiary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            case .profile:
                ProfileView(onSignOut: {
                    authState = .loggedOut
                })
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            case nil:
                Text("Select a section")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }
}

/// Routes between login and registration screens.
struct AuthRouter: View {
    @State private var showRegister = false
    var onAuthenticated: () -> Void = {}

    var body: some View {
        if showRegister {
            RegisterView(
                onSignUp: onAuthenticated,
                onSwitchToLogin: { showRegister = false }
            )
        } else {
            LoginView(
                onSignIn: onAuthenticated,
                onSwitchToRegister: { showRegister = true }
            )
        }
    }
}
