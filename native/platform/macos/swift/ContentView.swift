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

enum AuthState {
    case loggedOut
    case loggedIn
}

struct ContentView: View {
    @State private var selectedSection: AppSection? = .operator_
    @State private var authState: AuthState = .loggedOut
    @State private var showProfile = false

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
            List(AppSection.allCases, selection: $selectedSection) { section in
                Label(section.rawValue, systemImage: section.icon)
            }
            .listStyle(.sidebar)
            .navigationSplitViewColumnWidth(min: 180, ideal: 200, max: 260)
        } detail: {
            if let section = selectedSection {
                VStack {
                    Text(section.rawValue)
                        .font(.largeTitle)
                        .foregroundStyle(.secondary)
                    Text("Coming soon")
                        .foregroundStyle(.tertiary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                Text("Select a section")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .toolbar {
            ToolbarItem(placement: .automatic) {
                Button(action: { showProfile.toggle() }) {
                    Image(systemName: "person.circle")
                }
                .popover(isPresented: $showProfile) {
                    ProfileView(onSignOut: {
                        showProfile = false
                        authState = .loggedOut
                    })
                    .frame(width: 300, height: 280)
                }
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
