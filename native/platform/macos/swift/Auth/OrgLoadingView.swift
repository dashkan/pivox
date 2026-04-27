import SwiftUI

/// Splash shown while `OrgService.bootstrap()` is in flight after
/// sign-in. Uses the same accent backdrop + glass card as the auth
/// screens so the visual continuity from registration → loading →
/// (onboarding|app) holds.
struct OrgLoadingView: View {
  @Environment(\.pivoxTheme) private var theme
  @AppStorage("debug.auth.glass_card") private var useGlassCard: Bool = true

  var body: some View {
    VStack(spacing: 0) {
      Spacer()
      card.padding(.horizontal, 40)
      Spacer()
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity)
    .background(authBackdrop.ignoresSafeArea())
  }

  private var card: some View {
    VStack(spacing: 24) {
      Text("Pivox")
        .font(theme.brandTitleFont)
      ProgressView()
        .controlSize(.large)
      Text("Loading your organizations…")
        .font(theme.bodyFont)
        .foregroundStyle(.secondary)
    }
    .padding(48)
    .frame(maxWidth: 400)
    .glassCardIfEnabled(useGlassCard)
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
}

/// Shown when `ListOrganizations` fails. Same visual treatment as
/// the loading splash; offers a retry that calls `OrgService.reload()`.
struct OrgLoadErrorView: View {
  let message: String
  let retry: () -> Void

  private let auth = AuthService.shared

  @Environment(\.pivoxTheme) private var theme
  @AppStorage("debug.auth.glass_card") private var useGlassCard: Bool = true

  var body: some View {
    VStack(spacing: 0) {
      Spacer()
      card.padding(.horizontal, 40)
      Spacer()
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity)
    .background(authBackdrop.ignoresSafeArea())
  }

  private var card: some View {
    VStack(spacing: 24) {
      Text("Pivox")
        .font(theme.brandTitleFont)
      Text(message)
        .font(theme.bodyFont)
        .foregroundStyle(theme.destructive)
        .multilineTextAlignment(.center)

      AuthPrimaryButton("Try Again", action: retry)

      HStack(spacing: 4) {
        Text("Or")
          .font(theme.bodyFont)
          .foregroundStyle(.secondary)
        Button("sign out", action: { auth.signOut() })
          .buttonStyle(.link)
          .font(theme.bodyFont)
      }
    }
    .padding(32)
    .frame(maxWidth: 400)
    .glassCardIfEnabled(useGlassCard)
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
}
