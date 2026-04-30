import SwiftUI

/// SSO sign-in screen. Reached from `LoginView`'s "Sign in with SSO"
/// button. Owns its own email field so the password sign-in screen
/// doesn't have to morph based on what the resolver says — keeping
/// SSO behind an explicit click avoids leaking which domains have
/// SSO configured (silent probing on every keystroke would
/// enumerate the directory).
struct SsoLoginView: View {
  var onSwitchToLogin: () -> Void
  var auth: AuthService
  private let appState = AppStateBridge.shared()

  @Environment(\.pivoxTheme) private var theme
  @State private var email = ""
  @State private var isLoading = false
  @FocusState private var focused: Bool
  @AppStorage("debug.auth.glass_card") private var useGlassCard: Bool = true

  init(auth: AuthService = .shared, onSwitchToLogin: @escaping () -> Void) {
    self.auth = auth
    self.onSwitchToLogin = onSwitchToLogin
    let saved = AppStateBridge.shared().loadString(forKey: "remembered_email") ?? ""
    _email = State(initialValue: saved)
  }

  var body: some View {
    VStack(spacing: 0) {
      Spacer()
      authCard
        .padding(.horizontal, 40)
      Spacer()
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity)
    .background(authBackdrop.ignoresSafeArea())
    .onAppear { focused = true }
  }

  /// Same accent-tinted backdrop pattern as LoginView so chrome
  /// doesn't flash when navigating between the two screens.
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

  private var authCard: some View {
    VStack(spacing: 24) {
      VStack(spacing: 8) {
        Image(systemName: "key.shield")
          .font(.system(size: 32, weight: .regular))
          .foregroundStyle(.secondary)
        Text("Sign in with SSO")
          .font(theme.brandTitleFont)
        Text("Enter your work email to continue.")
          .font(theme.bodyFont)
          .foregroundStyle(.secondary)
          .multilineTextAlignment(.center)
      }

      VStack(spacing: 16) {
        TextField("Work email", text: $email)
          .textFieldStyle(.roundedBorder)
          .textContentType(.username)
          .focused($focused)
          .onSubmit { submit() }
          .disabled(isLoading)
          .accessibilityLabel("Work email")
          .accessibilityIdentifier("sso-email")

        AuthPrimaryButton("Continue", isLoading: isLoading, action: submit)
          .disabled(email.isEmpty || isLoading)
          .accessibilityIdentifier("sso-continue")

        // Pre-allocated error row (matches LoginView's pattern so
        // navigating between the two doesn't shift layout).
        Text(auth.errorMessage ?? " ")
          .font(theme.bodyFont)
          .foregroundStyle(theme.destructive)
          .multilineTextAlignment(.center)
          .opacity(auth.errorMessage != nil ? 1 : 0)
          .accessibilityIdentifier("sso-error")
      }

      HStack(spacing: 4) {
        Text("Not using SSO?")
          .font(theme.bodyFont)
          .foregroundStyle(.secondary)
        Button("Back to sign in", action: onSwitchToLogin)
          .buttonStyle(.link)
          .font(theme.bodyFont)
          .disabled(isLoading)
          .accessibilityIdentifier("sso-back-to-login")
      }
    }
    .padding(32)
    .frame(maxWidth: 400)
    .glassCardIfEnabled(useGlassCard)
  }

  private func submit() {
    let trimmed = email.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !trimmed.isEmpty, !isLoading else { return }
    isLoading = true
    auth.errorMessage = nil
    Task {
      defer { isLoading = false }
      do {
        if let providerID = try await auth.resolveSSOProvider(email: trimmed) {
          await auth.signInWithSSO(providerID: providerID, loginHint: trimmed)
          if auth.isSignedIn {
            appState.save(trimmed, forKey: "remembered_email")
          }
        } else {
          // Generic message intentional — don't disclose whether the
          // domain is unknown vs. SSO-not-configured (matches the
          // server's existence-probe defense on resolveProvider).
          auth.errorMessage = "SSO is not available for this email"
        }
      } catch {
        auth.errorMessage = "Couldn't reach the SSO directory. Try again."
      }
    }
  }
}
