import SwiftUI

struct RegisterView: View {
    var onSwitchToLogin: () -> Void

    private let auth = AuthService.shared

    @State private var email = ""
    @State private var displayName = ""
    @State private var password = ""
    @State private var confirmPassword = ""
    @State private var isLoading = false

    init(onSwitchToLogin: @escaping () -> Void) {
        self.onSwitchToLogin = onSwitchToLogin
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
                Text("Create your account")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }

            // Form
            VStack(spacing: 16) {
                TextField("Email", text: $email)
                    .textFieldStyle(.roundedBorder)
                    .textContentType(.emailAddress)

                TextField("Display name", text: $displayName)
                    .textFieldStyle(.roundedBorder)
                    .textContentType(.name)

                SecureField("Password", text: $password)
                    .textFieldStyle(.roundedBorder)
                    .textContentType(.newPassword)

                SecureField("Confirm password", text: $confirmPassword)
                    .textFieldStyle(.roundedBorder)
                    .textContentType(.newPassword)

                Button(action: {
                    guard password == confirmPassword else {
                        auth.errorMessage = "Passwords do not match."
                        return
                    }
                    isLoading = true
                    Task {
                        await auth.createAccount(email: email, password: password, displayName: displayName)
                        isLoading = false
                    }
                }) {
                    if isLoading {
                        ProgressView()
                            .controlSize(.small)
                            .frame(maxWidth: .infinity)
                    } else {
                        Text("Create Account")
                            .frame(maxWidth: .infinity)
                    }
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .disabled(email.isEmpty || password.isEmpty || confirmPassword.isEmpty || displayName.isEmpty || isLoading)

                if let error = auth.errorMessage {
                    Text(error)
                        .font(.caption)
                        .foregroundStyle(.red)
                        .multilineTextAlignment(.center)
                }
            }

            // Separator
            HStack {
                Rectangle().frame(height: 1).foregroundStyle(.separator)
                Text("or").font(.caption).foregroundStyle(.secondary)
                Rectangle().frame(height: 1).foregroundStyle(.separator)
            }

            // Social signup
            VStack(spacing: 8) {
                Button(action: {
                    Task { await auth.signInWithGoogle() }
                }) {
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
                Text("Already have an account?")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Button("Sign in", action: onSwitchToLogin)
                    .buttonStyle(.link)
                    .font(.caption)
            }
        }
        .padding(32)
        .frame(maxWidth: 400)
        .glassCard()
    }
}
