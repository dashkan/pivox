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
          .font(.largeTitle.weight(.bold))
        Text("Create your account")
          .font(.subheadline)
          .foregroundStyle(.secondary)
      }

      // Form
      VStack(spacing: 16) {
        TextField("Email", text: $email)
          .textFieldStyle(.roundedBorder)
          .textContentType(.emailAddress)
          .disabled(isLoading)
          .accessibilityIdentifier("register-email")

        TextField("Display name", text: $displayName)
          .textFieldStyle(.roundedBorder)
          .textContentType(.name)
          .disabled(isLoading)
          .accessibilityIdentifier("register-display-name")

        SecureField("Password", text: $password)
          .textFieldStyle(.roundedBorder)
          .textContentType(.newPassword)
          .disabled(isLoading)
          .accessibilityIdentifier("register-password")

        SecureField("Confirm password", text: $confirmPassword)
          .textFieldStyle(.roundedBorder)
          .textContentType(.newPassword)
          .disabled(isLoading)
          .accessibilityIdentifier("register-confirm-password")

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
        .disabled(
          email.isEmpty || password.isEmpty || confirmPassword.isEmpty || displayName.isEmpty
            || isLoading
        )
        .accessibilityIdentifier("register-create-account")

        // Error message — pre-allocated space to prevent layout shift.
        Text(auth.errorMessage ?? " ")
          .font(.caption)
          .foregroundStyle(.red)
          .multilineTextAlignment(.center)
          .opacity(auth.errorMessage != nil ? 1 : 0)
          .accessibilityIdentifier("register-error")
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
          isLoading = true
          Task {
            await auth.signInWithGoogle()
            isLoading = false
          }
        }) {
          HStack {
            GoogleIcon(size: 16)
            Text("Continue with Google")
          }
          .frame(maxWidth: .infinity)
        }
        .buttonStyle(.bordered)
        .controlSize(.large)
        .disabled(isLoading)

        Button(action: {
          isLoading = true
          Task {
            await auth.signInWithGitHub()
            isLoading = false
          }
        }) {
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
        .disabled(isLoading)
      }

      // Footer
      HStack(spacing: 4) {
        Text("Already have an account?")
          .font(.caption)
          .foregroundStyle(.secondary)
        Button("Sign in", action: onSwitchToLogin)
          .buttonStyle(.link)
          .font(.caption)
          .accessibilityIdentifier("register-switch-login")
      }
    }
    .padding(32)
    .frame(maxWidth: 400)
    .glassCard()
  }
}
