import SwiftUI

/// App-wide design tokens. Adaptive for light/dark mode via NSColor
/// semantic colors. Injected via `.environment(\.pivoxTheme, theme)`.
/// Every view — auth, profile, chat, image editor — reads from the
/// same `@Environment(\.pivoxTheme)` so visual language stays
/// consistent across the app.
public struct PivoxTheme: Sendable {

    // MARK: Typography

    public let bodyFont: Font
    public let bodySmallFont: Font
    public let captionFont: Font
    public let headingFont: Font
    public let codeFont: Font

    // MARK: Spacing

    public let spacingXS: CGFloat
    public let spacingSM: CGFloat
    public let spacingMD: CGFloat
    public let spacingLG: CGFloat
    public let spacingXL: CGFloat

    // MARK: Radii

    public let radiusSM: CGFloat
    public let radiusMD: CGFloat
    public let radiusLG: CGFloat

    // MARK: Colors — semantic

    public let textPrimary: Color
    public let textSecondary: Color
    public let textTertiary: Color
    public let background: Color
    public let backgroundRaised: Color
    public let backgroundElevated: Color
    public let border: Color
    public let borderSubtle: Color
    public let accent: Color
    public let accentSubtle: Color
    /// Destructive / danger actions (delete account, remove, etc).
    public let destructive: Color
    public let destructiveSubtle: Color
    public let success: Color
    public let warning: Color

    // MARK: Chat-specific

    public let userBubble: Color
    public let assistantBubble: Color
    public let codeSurface: Color
    public let codeText: Color
    public let inlineCodeBackground: Color

    // MARK: Default

    public static let `default` = PivoxTheme(
        bodyFont: .body,
        bodySmallFont: .callout,
        captionFont: .caption,
        headingFont: .headline,
        codeFont: .system(.body, design: .monospaced),

        spacingXS: 4,
        spacingSM: 8,
        spacingMD: 12,
        spacingLG: 16,
        spacingXL: 24,

        radiusSM: 4,
        radiusMD: 8,
        radiusLG: 12,

        textPrimary: Color(nsColor: .labelColor),
        textSecondary: Color(nsColor: .secondaryLabelColor),
        textTertiary: Color(nsColor: .tertiaryLabelColor),
        background: Color(nsColor: .windowBackgroundColor),
        backgroundRaised: Color(nsColor: .controlBackgroundColor),
        backgroundElevated: Color(nsColor: .underPageBackgroundColor),
        border: Color(nsColor: .separatorColor),
        borderSubtle: Color(nsColor: .quaternaryLabelColor),
        accent: Color.accentColor,
        accentSubtle: Color.accentColor.opacity(0.15),
        destructive: Color(nsColor: .systemRed),
        destructiveSubtle: Color(nsColor: .systemRed).opacity(0.15),
        success: Color(nsColor: .systemGreen),
        warning: Color(nsColor: .systemOrange),

        userBubble: Color.accentColor.opacity(0.12),
        assistantBubble: Color(nsColor: .controlBackgroundColor),
        codeSurface: Color(nsColor: .textBackgroundColor),
        codeText: Color(nsColor: .textColor),
        inlineCodeBackground: Color(nsColor: .quaternaryLabelColor).opacity(0.5)
    )
}

// MARK: - Environment Key

private struct PivoxThemeKey: EnvironmentKey {
    static let defaultValue = PivoxTheme.default
}

extension EnvironmentValues {
    public var pivoxTheme: PivoxTheme {
        get { self[PivoxThemeKey.self] }
        set { self[PivoxThemeKey.self] = newValue }
    }
}
