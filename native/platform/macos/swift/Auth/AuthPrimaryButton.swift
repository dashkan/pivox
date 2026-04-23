import SwiftUI

/// Full-width primary action button for auth flows (Sign In, Create
/// Account). Wraps `.borderedProminent` + `.controlSize(.large)`
/// with the correct foreground-color placement so callers get the
/// look right by construction.
///
/// Why a dedicated component (vs a theme modifier):
///   - `.foregroundStyle` must live INSIDE the button's label to
///     survive macOS 26's `.borderedProminent` re-resolution. A
///     `ViewModifier` or a pure theme token can't enforce that
///     placement from outside; this component does, so callers
///     can't misplace it.
///   - Loading treatment (swap label for `ProgressView` in matching
///     tint) is the same everywhere we use this pattern.
///   - Foreground color reads from `theme.prominentButtonText` so
///     the palette stays centralized — component encapsulates the
///     structure, theme owns the color.
///
/// Scope: auth-specific primary action. Destructive prominent
/// actions (Delete account) use a different icon+label pattern and
/// a different tint; keep those separate.
struct AuthPrimaryButton: View {
    private let title: String
    private let isLoading: Bool
    private let action: () -> Void

    @Environment(\.pivoxTheme) private var theme

    init(_ title: String, isLoading: Bool = false, action: @escaping () -> Void) {
        self.title = title
        self.isLoading = isLoading
        self.action = action
    }

    var body: some View {
        Button(action: action) {
            Group {
                if isLoading {
                    ProgressView()
                        .controlSize(.small)
                        .tint(theme.prominentButtonText)
                } else {
                    Text(title)
                        .foregroundStyle(theme.prominentButtonText)
                }
            }
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .controlSize(.large)
    }
}
