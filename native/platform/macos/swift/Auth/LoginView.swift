import SwiftUI

struct LoginView: View {
  var onSwitchToRegister: () -> Void
  var onSwitchToSSO: () -> Void
  // Injectable AuthService so delegated auth flows (AUTHN-07) can reuse the
  // same UI against a named Firebase backend. Defaults to the shared instance
  // for normal app launches.
  var auth: AuthService
  private let appState = AppStateBridge.shared()

  @Environment(\.pivoxTheme) private var theme
  @State private var email = ""
  @State private var password = ""
  @State private var rememberMe: Bool
  @State private var isLoading = false
  @FocusState private var focusedField: Field?
  /// DEBUG-only toggle (⌘⇧G) so we can A/B the glass card
  /// treatment live without rebuilding. Persists across launches so
  /// the preference survives reopening the app.
  @AppStorage("debug.auth.glass_card") private var useGlassCard: Bool = true

  enum Field: Hashable {
    case email, password
  }

  init(
    auth: AuthService = .shared,
    onSwitchToRegister: @escaping () -> Void,
    onSwitchToSSO: @escaping () -> Void = {}
  ) {
    self.auth = auth
    self.onSwitchToRegister = onSwitchToRegister
    self.onSwitchToSSO = onSwitchToSSO
    let state = AppStateBridge.shared()
    let savedEmail = state.loadString(forKey: "remembered_email") ?? ""
    _email = State(initialValue: savedEmail)
    _rememberMe = State(initialValue: !savedEmail.isEmpty)
  }

  var body: some View {
    VStack(spacing: 0) {
      Spacer()

      Group {
        if auth.pendingMFAResolver != nil {
          MFAChallengeView(auth: auth)
        } else {
          authCard
        }
      }
      .padding(.horizontal, 40)

      Spacer()
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity)
    // Liquid Glass refracts and distorts content behind it. An
    // opaque uniform background gives the glass nothing to react to,
    // which is why `.glassCard()` looks identical to the fallback
    // material on a plain window. Providing a subtle colored
    // gradient behind the auth area gives the glass visible
    // refraction — accent-tinted light that the card distorts and
    // bends as it floats over. This is the macOS 26 pattern Apple
    // uses for login/onboarding moments.
    .background(authBackdrop.ignoresSafeArea())
    .onAppear { focusedField = .email }
    .background(glassToggleShortcut)
  }

  /// Soft accent-tinted backdrop behind the auth card. Uses theme
  /// colors so it adapts to dark mode. Two overlapping radial
  /// gradients create gentle color variation that Liquid Glass
  /// refracts visibly without being a loud, branded hero image.
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

  /// Hidden button wired as a ⌘⇧G shortcut so we can A/B the glass
  /// card treatment live. Zero-sized, invisible, outside tab order.
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
      // Upper section (header + form) is locked to a fixed height.
      // This is what keeps the separator and everything below it at
      // identical Y-coordinates between Login (shorter form) and
      // Register (taller form) — earlier the total card height was
      // fixed but a Spacer inside distributed residual height, and
      // sub-pixel rounding drifted the separator by 1pt. Pinning the
      // upper section eliminates that.
      VStack(spacing: 24) {
      // Header
      VStack(spacing: 8) {
        Text("Pivox")
          .font(theme.brandTitleFont)
        Text("Sign in to your account")
          .font(theme.bodyFont)
          .foregroundStyle(.secondary)
      }

      // Form
      VStack(spacing: 16) {
        TextField("Email", text: $email)
          .textFieldStyle(.roundedBorder)
          // `.username`, not `.emailAddress`. Password managers
          // (1Password, iCloud Keychain, etc.) key on
          // `.username` to offer login fill; `.emailAddress` is a
          // Contacts-suggestion hint and doesn't trigger autofill.
          .textContentType(.username)
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
            .font(theme.bodyFont)
            .disabled(isLoading)
            .accessibilityIdentifier("login-remember-me")
          Spacer()
          Button("Forgot password?") {}
            .buttonStyle(.link)
            .font(theme.bodyFont)
            .disabled(isLoading)
            .accessibilityIdentifier("login-forgot-password")
        }

        AuthPrimaryButton("Sign In", isLoading: isLoading, action: submitSignIn)
          .disabled(email.isEmpty || password.isEmpty || isLoading)
          .accessibilityIdentifier("login-sign-in")

        // Error message — pre-allocated space to prevent layout shift.
        Text(auth.errorMessage ?? " ")
          .font(theme.bodyFont)
          .foregroundStyle(theme.destructive)
          .multilineTextAlignment(.center)
          .opacity(auth.errorMessage != nil ? 1 : 0)
          .accessibilityIdentifier("login-error")
      }

      // Flexible gap inside the upper (fixed-height) section. Grows
      // on Login (shorter form), collapses on Register (taller).
      Spacer(minLength: 12)
      }
      // Exact height of the upper section. Sized just above
      // Register's natural upper content so Register's Spacer
      // collapses to near minLength while Login's grows to match.
      .frame(height: 320)

      // Separator
      HStack {
        Rectangle().frame(height: 1).foregroundStyle(theme.border)
        Text("or").font(theme.bodySmallFont).foregroundStyle(.secondary)
        Rectangle().frame(height: 1).foregroundStyle(theme.border)
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

        Button(action: {
          appState.save("", forKey: "remembered_email")
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

        // SSO is intentionally a separate explicit screen — auto-
        // probing the resolver as the user types would enumerate
        // which domains have SSO configured. The button hands off
        // to a dedicated SSO view that owns its own email field
        // and provider-resolution flow.
        Button(action: onSwitchToSSO) {
          HStack {
            Image(systemName: "key.shield")
              .resizable()
              .aspectRatio(contentMode: .fit)
              .frame(width: 16, height: 16)
            Text("Sign in with SSO")
          }
          .frame(maxWidth: .infinity)
        }
        .buttonStyle(.bordered)
        .controlSize(.large)
        .disabled(isLoading)
        .accessibilityIdentifier("login-sso")
      }

      // Footer
      HStack(spacing: 4) {
        Text("Don't have an account?")
          .font(theme.bodyFont)
          .foregroundStyle(.secondary)
        Button("Create one", action: onSwitchToRegister)
          .buttonStyle(.link)
          .font(theme.bodyFont)
          .disabled(isLoading)
          .accessibilityIdentifier("login-switch-register")
      }
    }
    .padding(32)
    .frame(maxWidth: 400)
    .glassCardIfEnabled(useGlassCard)
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

/// Second-factor challenge shown on the login card when the first
/// factor (email/password or OAuth) succeeded but the account has
/// TOTP enrolled. Styled to match `LoginView.authCard` so the
/// window chrome doesn't shift when we swap between the sign-in
/// form and this challenge.
private struct MFAChallengeView: View {
  let auth: AuthService

  @Environment(\.pivoxTheme) private var theme
  @AppStorage("debug.auth.glass_card") private var useGlassCard: Bool = true

  @State private var code: String = ""
  @State private var isVerifying = false
  @State private var errorMessage: String?

  var body: some View {
    VStack(spacing: 24) {
      VStack(spacing: 8) {
        Image(systemName: "lock.shield")
          .font(.system(size: 32, weight: .regular))
          .foregroundStyle(.secondary)
        Text("Two-factor authentication")
          .font(theme.brandTitleFont)
        Text("Enter the 6-digit code from your authenticator app.")
          .font(theme.bodyFont)
          .foregroundStyle(.secondary)
          .multilineTextAlignment(.center)
      }

      OTPSegmentedField(value: $code, length: 6, onComplete: verify)

      // No Verify button: the OTP field auto-submits on 6 digits
      // via `onComplete`. A button would be disabled until after
      // auto-submit already fired. Inline spinner below gives the
      // user the loading signal instead.
      if isVerifying {
        ProgressView().controlSize(.small)
      }

      Text(errorMessage ?? " ")
        .font(theme.bodyFont)
        .foregroundStyle(theme.destructive)
        .multilineTextAlignment(.center)
        .opacity(errorMessage != nil ? 1 : 0)

      Button("Use a different account", action: cancel)
        .buttonStyle(.link)
        .font(theme.bodyFont)
        .disabled(isVerifying)
        .accessibilityIdentifier("mfa-cancel")
    }
    .padding(32)
    .frame(maxWidth: 400)
    .glassCardIfEnabled(useGlassCard)
  }

  private func verify() {
    guard code.count == 6 else { return }
    isVerifying = true
    errorMessage = nil
    Task {
      defer { isVerifying = false }
      do {
        try await auth.completeMFASignIn(code: code)
      } catch let error as ProfileError {
        errorMessage = error.userMessage
        code = ""
      } catch {
        errorMessage = error.localizedDescription
        code = ""
      }
    }
  }

  private func cancel() {
    auth.cancelMFASignIn()
  }
}
