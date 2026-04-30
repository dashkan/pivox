import SwiftUI

/// Email-first sign-in. Single entry point that resolves SSO vs.
/// password from the email after the user submits step 1:
///
///   Step 1 — email only, "Continue" button.
///     ↳ resolveSSOProvider(email)
///         ↳ provider id    → kick off OIDC broker flow.
///         ↳ nil            → step 2 (password resubmit).
///         ↳ error          → generic message, stay on step 1.
///
///   Step 2 — password field revealed below the (now read-only)
///   email, "Sign In" button. Submits email+password.
///
/// Why this shape: pre-Phase-X the landing page had two buttons
/// ("Sign In" + "Sign in with SSO") and a separate `SsoLoginView`,
/// which forced users to know which mechanism their org used and
/// produced a dead-end "SSO is not available for this email" failure
/// when they guessed wrong. Email-first is the modern default
/// (Slack/Notion/Linear/Vercel) and lets an org enable SSO without
/// requiring users to switch buttons — the existing email path just
/// works.
///
/// Sign-up is intentionally unaware of SSO. SSO accounts come in via
/// invite or IdP auto-provisioning; the registration surface stays
/// password-only and routes here only for sign-in.
struct LoginView: View {
  var onSwitchToRegister: () -> Void
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
  /// Email→provider resolve completed and returned no SSO provider.
  /// Reveals the password field and switches the primary button to
  /// "Sign In". Only ever flips false→true within a session — once
  /// we've established the email is password-auth, we don't bounce
  /// the user back to step 1 mid-attempt.
  @State private var didResolveAsPassword = false
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
    onSwitchToRegister: @escaping () -> Void
  ) {
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
          .onSubmit { submit() }
          .disabled(isLoading)
          .accessibilityLabel("Email address")
          .accessibilityIdentifier("login-email")
          // Editing the email after step 1 invalidates the SSO/password
          // decision — they may be typing a different domain. Roll
          // back to step 1 so the next submit re-resolves.
          .onChange(of: email) { _, _ in
            if didResolveAsPassword {
              didResolveAsPassword = false
              password = ""
            }
          }

        // Password field is mounted only after step 1 completes with
        // "no SSO" — keeping it gated avoids autofill prompting
        // during the email-only state and preserves the email-first
        // visual model. macOS keychain autofill triggers on
        // SecureField focus regardless of mount-vs-hidden, so a
        // conditional mount works without losing fill.
        if didResolveAsPassword {
          SecureField("Password", text: $password)
            .textFieldStyle(.roundedBorder)
            .textContentType(.password)
            .focused($focusedField, equals: .password)
            .onSubmit { submit() }
            .disabled(isLoading)
            .accessibilityLabel("Password")
            .accessibilityIdentifier("login-password")
            .transition(.opacity.combined(with: .move(edge: .top)))
        }

        HStack {
          Toggle("Remember me", isOn: $rememberMe)
            .toggleStyle(.checkbox)
            .font(theme.bodyFont)
            .disabled(isLoading)
            .accessibilityIdentifier("login-remember-me")
          Spacer()
          if didResolveAsPassword {
            Button("Forgot password?") {}
              .buttonStyle(.link)
              .font(theme.bodyFont)
              .disabled(isLoading)
              .accessibilityIdentifier("login-forgot-password")
          }
        }

        // Primary button morphs through three states:
        //   - idle/step 1   → "Continue"  (resolve SSO vs password)
        //   - idle/step 2   → "Sign In"   (submit password)
        //   - OAuth in flight → "Cancel sign-in" (tear down session)
        //
        // Morphing in place rather than placing the cancel as a
        // secondary link below the form: the user's eye is already
        // on the primary button after they clicked it, and a
        // peripheral cancel link is hard to find when they want it.
        // ASWebAuthenticationSession's own sheet has a Close button,
        // but the sheet is often buried behind windows or off-focus
        // — the in-app primary-button cancel covers all those cases
        // without making the user hunt.
        //
        // Single AuthPrimaryButton with conditional props (rather
        // than an if/else swap) so the view identity stays stable
        // across the morph — SwiftUI's implicit transition would
        // otherwise fade the old button out and the new one in,
        // producing a visible flash right at the moment the user
        // expects the OAuth sheet to appear.
        AuthPrimaryButton(
          auth.isOAuthInProgress
            ? "Cancel sign-in"
            : (didResolveAsPassword ? "Sign In" : "Continue"),
          isLoading: !auth.isOAuthInProgress && isLoading,
          action: auth.isOAuthInProgress ? auth.cancelOAuth : submit
        )
        .disabled(!auth.isOAuthInProgress && primaryDisabled)
        .accessibilityIdentifier(
          auth.isOAuthInProgress ? "login-cancel-oauth" : "login-sign-in")

        // Error message — pre-allocated space to prevent layout shift.
        Text(auth.errorMessage ?? " ")
          .font(theme.bodyFont)
          .foregroundStyle(theme.destructive)
          .multilineTextAlignment(.center)
          .opacity(auth.errorMessage != nil ? 1 : 0)
          .accessibilityIdentifier("login-error")
      }
      .animation(.easeInOut(duration: 0.18), value: didResolveAsPassword)

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

  /// Primary button is disabled when the visible-field invariants
  /// aren't met for the current step. Step 1 needs an email; step 2
  /// needs both fields populated.
  private var primaryDisabled: Bool {
    if isLoading { return true }
    if didResolveAsPassword {
      return email.isEmpty || password.isEmpty
    }
    return email.isEmpty
  }

  /// Single submit handler. Step 1 resolves SSO-vs-password; step 2
  /// runs the password sign-in. Same handler so `onSubmit` on the
  /// email field, `onSubmit` on the password field, and the primary
  /// button all share one path.
  private func submit() {
    guard !isLoading else { return }
    if didResolveAsPassword {
      submitPassword()
    } else {
      submitEmail()
    }
  }

  private func submitEmail() {
    let trimmed = email.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !trimmed.isEmpty else { return }
    isLoading = true
    auth.errorMessage = nil
    Task {
      defer { isLoading = false }
      do {
        if let providerID = try await auth.resolveSSOProvider(email: trimmed) {
          // SSO domain — hand off to the OIDC broker flow.
          // signInWithSSO sets `auth.errorMessage` on failure so the
          // UI surfaces it the same way password auth does.
          await auth.signInWithSSO(providerID: providerID, loginHint: trimmed)
          if auth.isSignedIn {
            appState.save(rememberMe ? trimmed : "", forKey: "remembered_email")
          }
        } else {
          // No SSO provider for this email — reveal the password
          // field and let the user resubmit. The "no account exists"
          // case falls through to step 2 and surfaces from the
          // server's normal invalid-credentials response, matching
          // existing password-auth messaging (intentional —
          // `resolveProvider` is public, so we don't disclose
          // existence here).
          didResolveAsPassword = true
          focusedField = .password
        }
      } catch {
        auth.errorMessage = "Couldn't reach the sign-in service. Try again."
      }
    }
  }

  private func submitPassword() {
    guard !email.isEmpty, !password.isEmpty else { return }
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
