import SwiftUI

struct LoginView: View {
  var onSwitchToRegister: () -> Void
  // Injectable AuthService so delegated auth flows (AUTHN-07) can reuse the
  // same UI against a named Firebase backend. Defaults to the shared instance
  // for normal app launches.
  var auth: AuthService
  private let appState = AppStateBridge.shared()

  @State private var email = ""
  @State private var password = ""
  @State private var rememberMe: Bool
  @State private var isLoading = false
  @FocusState private var focusedField: Field?

  enum Field: Hashable {
    case email, password
  }

  init(auth: AuthService = .shared, onSwitchToRegister: @escaping () -> Void) {
    self.auth = auth
    self.onSwitchToRegister = onSwitchToRegister
    let state = AppStateBridge.shared()
    let savedEmail = state.loadString(forKey: "remembered_email") ?? ""
    _email = State(initialValue: savedEmail)
    _rememberMe = State(initialValue: !savedEmail.isEmpty)
  }

  var body: some View {
    VStack(spacing: 0) {
      Spacer()

      authCard
        .padding(.horizontal, 40)

      Spacer()
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity)
    .onAppear { focusedField = .email }
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
          .focused($focusedField, equals: .email)
          .onSubmit { focusedField = .password }
          .disabled(isLoading)
          .accessibilityLabel("Email address")
          .accessibilityIdentifier("login-email")

        SecureField("Password", text: $password)
          .textFieldStyle(.roundedBorder)
          .textContentType(.password)
          .focused($focusedField, equals: .password)
          .onSubmit { submitSignIn() }
          .disabled(isLoading)
          .accessibilityLabel("Password")
          .accessibilityIdentifier("login-password")

        HStack {
          Toggle("Remember me", isOn: $rememberMe)
            .toggleStyle(.checkbox)
            .font(.caption)
            .disabled(isLoading)
            .accessibilityIdentifier("login-remember-me")
          Spacer()
          Button("Forgot password?") {}
            .buttonStyle(.link)
            .font(.caption)
            .disabled(isLoading)
            .accessibilityIdentifier("login-forgot-password")
        }

        Button(action: submitSignIn) {
          if isLoading {
            ProgressView()
              .controlSize(.small)
              .frame(maxWidth: .infinity)
          } else {
            Text("Sign In")
              .frame(maxWidth: .infinity)
          }
        }
        .buttonStyle(.borderedProminent)
        .controlSize(.large)
        .disabled(email.isEmpty || password.isEmpty || isLoading)
        .accessibilityIdentifier("login-sign-in")

        // Error message — pre-allocated space to prevent layout shift.
        Text(auth.errorMessage ?? " ")
          .font(.caption)
          .foregroundStyle(.red)
          .multilineTextAlignment(.center)
          .opacity(auth.errorMessage != nil ? 1 : 0)
          .accessibilityIdentifier("login-error")
      }

      // Separator
      HStack {
        Rectangle().frame(height: 1).foregroundStyle(.separator)
        Text("or").font(.caption).foregroundStyle(.secondary)
        Rectangle().frame(height: 1).foregroundStyle(.separator)
      }

      // Social login
      VStack(spacing: 8) {
        Button(action: {
          appState.save("", forKey: "remembered_email")
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

        Button(action: {}) {
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
        Text("Don't have an account?")
          .font(.caption)
          .foregroundStyle(.secondary)
        Button("Create one", action: onSwitchToRegister)
          .buttonStyle(.link)
          .font(.caption)
          .disabled(isLoading)
          .accessibilityIdentifier("login-switch-register")
      }
    }
    .padding(32)
    .frame(maxWidth: 400)
    .glassCard()
  }

  private func submitSignIn() {
    guard !email.isEmpty, !password.isEmpty, !isLoading else { return }
    isLoading = true
    Task {
      await auth.signIn(email: email, password: password)
      isLoading = false
      // Only save email on successful sign-in.
      if auth.isSignedIn {
        appState.save(rememberMe ? email : "", forKey: "remembered_email")
      }
    }
  }
}
