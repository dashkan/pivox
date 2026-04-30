import SwiftUI

/// Full-width secondary auth action button (Sign in with SSO,
/// future "Use a different sign-in method" affordances). Visual
/// peer of `AuthPrimaryButton` — same width, same control size,
/// same corner shape — but rendered as an accent-tinted outline
/// instead of a filled prominent button.
///
/// Why a dedicated component (vs reusing `.bordered`):
///   - The default `.bordered` style on macOS 26 inherits a neutral
///     gray fill that visually equates to the social-provider
///     buttons. Pinning `.tint(theme.accent)` lifts the SSO action
///     up next to the primary Sign In so users see two equally-
///     prominent paths into the app, with the filled vs outlined
///     contrast carrying the "default action" signal.
///   - Title-only construction matches `AuthPrimaryButton` so callers
///     don't end up with two slightly different button structures
///     for two equally-weighted actions.
///
/// Scope: auth flows (login screen). For tertiary actions like
/// "Forgot password?" use `.buttonStyle(.link)` instead.
struct AuthSecondaryButton: View {
  private let title: String
  private let systemImage: String?
  private let isLoading: Bool
  private let action: () -> Void

  @Environment(\.pivoxTheme) private var theme

  init(
    _ title: String,
    systemImage: String? = nil,
    isLoading: Bool = false,
    action: @escaping () -> Void
  ) {
    self.title = title
    self.systemImage = systemImage
    self.isLoading = isLoading
    self.action = action
  }

  var body: some View {
    Button(action: action) {
      Group {
        if isLoading {
          ProgressView()
            .controlSize(.small)
            .tint(theme.accent)
        } else if let systemImage {
          Label(title, systemImage: systemImage)
            .foregroundStyle(theme.accent)
        } else {
          Text(title)
            .foregroundStyle(theme.accent)
        }
      }
      .frame(maxWidth: .infinity)
    }
    .buttonStyle(.bordered)
    .tint(theme.accent)
    .controlSize(.large)
  }
}
