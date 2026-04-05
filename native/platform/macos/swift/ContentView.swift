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
        TabView {
            ForEach(AppSection.allCases) { section in
                Tab(section.rawValue, systemImage: section.icon) {
                    VStack {
                        Text(section.rawValue)
                            .font(.largeTitle)
                            .foregroundStyle(.secondary)
                        Text("Coming soon")
                            .foregroundStyle(.tertiary)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
        }
        .tabViewStyle(.sidebarAdaptable)
        .tabViewSidebarBottomBar {
            HStack {
                Image(systemName: "person.circle")
                Text("Profile")
                Spacer()
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 6)
            .contentShape(Rectangle())
            .onTapGesture {
                // TODO: navigate to profile
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
