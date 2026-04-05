import SwiftUI

struct LoginView: View {
    var onSignIn: () -> Void
    var onSwitchToRegister: () -> Void

    private let appState = AppStateBridge.shared()!

    @State private var email = ""
    @State private var password = ""
    @State private var rememberMe: Bool

    init(onSignIn: @escaping () -> Void, onSwitchToRegister: @escaping () -> Void) {
        self.onSignIn = onSignIn
        self.onSwitchToRegister = onSwitchToRegister
        // Restore "Remember Me" from persisted state.
        let state = AppStateBridge.shared()!
        _rememberMe = State(initialValue: state.hasBool(forKey: "rememberMe") ? state.loadBool(forKey: "rememberMe") : false)
    }

    var body: some View {
        VStack(spacing: 0) {
            Spacer()

            authCard
                .padding(.horizontal, 40)

            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var authCard: some View {
        VStack(spacing: 24) {
            // Header
            VStack(spacing: 8) {
                Text("Pivox")
                    .font(.system(size: 32, weight: .bold))
                Text("Sign in to your account")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }

            // Form
            VStack(spacing: 16) {
                TextField("Email", text: $email)
                    .textFieldStyle(.roundedBorder)
                    .textContentType(.emailAddress)

                SecureField("Password", text: $password)
                    .textFieldStyle(.roundedBorder)
                    .textContentType(.password)

                HStack {
                    Toggle("Remember me", isOn: $rememberMe)
                        .toggleStyle(.checkbox)
                        .font(.caption)
                    Spacer()
                    Button("Forgot password?") { /* placeholder */ }
                        .buttonStyle(.link)
                        .font(.caption)
                }

                Button(action: {
                    appState.save(rememberMe, forKey: "rememberMe")
                    onSignIn()
                }) {
                    Text("Sign In")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
            }

            // Separator
            HStack {
                Rectangle().frame(height: 1).foregroundStyle(.separator)
                Text("or").font(.caption).foregroundStyle(.secondary)
                Rectangle().frame(height: 1).foregroundStyle(.separator)
            }

            // Social login
            VStack(spacing: 8) {
                Button(action: { /* placeholder */ }) {
                    HStack {
                        GoogleIcon(size: 16)
                        Text("Continue with Google")
                    }
                    .frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)
                .controlSize(.large)

                Button(action: { /* placeholder */ }) {
                    HStack {
                        Image("GitHubLogo")
                            .resizable()
                            .aspectRatio(contentMode: .fit)
                            .frame(width: 16, height: 16)
                        Text("Continue with GitHub")
                    }
                    .frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)
                .controlSize(.large)
            }

            // Footer
            HStack(spacing: 4) {
                Text("Don't have an account?")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Button("Create one", action: onSwitchToRegister)
                    .buttonStyle(.link)
                    .font(.caption)
            }
        }
        .padding(32)
        .frame(maxWidth: 400)
        .glassCard()
    }
}
