import SwiftUI

struct RegisterView: View {
  var onSwitchToLogin: () -> Void

  private let auth = AuthService.shared

  @Environment(\.pivoxTheme) private var theme
  @State private var email = ""
  @State private var displayName = ""
  @State private var password = ""
  @State private var confirmPassword = ""
  @State private var isLoading = false
  /// Mirrors LoginView — shared toggle via AppStorage so ⌘⇧G
  /// flipping on either screen affects both.
  @AppStorage("debug.auth.glass_card") private var useGlassCard: Bool = true

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
    // Mirror LoginView: accent-tinted gradient backdrop gives the
    // glass card's Liquid Glass effect something to refract against.
    .background(authBackdrop.ignoresSafeArea())
    .background(glassToggleShortcut)
  }

  private var authBackdrop: some View {
    ZStack {
      theme.background
      RadialGradient(
        colors: [theme.accent.opacity(0.28), .clear],
        center: .topLeading,
        startRadius: 0,
        endRadius: 520)
      RadialGradient(
        colors: [theme.accent.opacity(0.18), .clear],
        center: .bottomTrailing,
        startRadius: 0,
        endRadius: 620)
    }
  }

  /// Hidden ⌘⇧G shortcut to toggle the glass card on/off — mirrors
  /// the same shortcut on LoginView so either screen can flip it.
  private var glassToggleShortcut: some View {
    Button {
      useGlassCard.toggle()
    } label: { EmptyView() }
      .keyboardShortcut("g", modifiers: [.command, .shift])
      .buttonStyle(.plain)
      .frame(width: 0, height: 0)
      .opacity(0)
      .accessibilityHidden(true)
      .focusable(false)
  }

  private var authCard: some View {
    VStack(spacing: 24) {
      // Upper section (header + form) is locked to a fixed height
      // matching LoginView so the separator and everything below
      // lands at identical Y in both screens — no 1pt drift from
      // Spacer rounding on a whole-card frame. See LoginView for the
      // rationale.
      VStack(spacing: 24) {
      // Header
      VStack(spacing: 8) {
        Text("Pivox")
          .font(theme.brandTitleFont)
        Text("Create your account")
          .font(theme.bodyFont)
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
          .font(theme.bodyFont)
          .foregroundStyle(theme.destructive)
          .multilineTextAlignment(.center)
          .opacity(auth.errorMessage != nil ? 1 : 0)
          .accessibilityIdentifier("register-error")
      }

      // Minimum gap between form and separator inside the fixed-
      // height upper section. On Register this collapses near the
      // minimum because the form fills most of the section; Login's
      // Spacer grows to match.
      Spacer(minLength: 12)
      }
      // Matches LoginView's upper-section height — see that file.
      .frame(height: 320)

      // Separator
      HStack {
        Rectangle().frame(height: 1).foregroundStyle(theme.border)
        Text("or").font(theme.bodySmallFont).foregroundStyle(.secondary)
        Rectangle().frame(height: 1).foregroundStyle(theme.border)
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
          .font(theme.bodyFont)
          .foregroundStyle(.secondary)
        Button("Sign in", action: onSwitchToLogin)
          .buttonStyle(.link)
          .font(theme.bodyFont)
          .accessibilityIdentifier("register-switch-login")
      }
    }
    .padding(32)
    .frame(maxWidth: 400)
    .glassCardIfEnabled(useGlassCard)
  }
}
