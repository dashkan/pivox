import SwiftUI

// MARK: - Design Tokens

/// Core design tokens for AIElements. Adaptive for light/dark mode.
/// Injected via `.environment(\.aiElementsTheme, theme)`.
public struct AIElementsTheme: Sendable {

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
    public let destructive: Color
    public let success: Color
    public let warning: Color

    // MARK: Chat-specific

    public let userBubble: Color
    public let assistantBubble: Color
    public let codeSurface: Color
    public let codeText: Color
    public let inlineCodeBackground: Color

    // MARK: Default

    public static let `default` = AIElementsTheme(
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
        destructive: Color.red,
        success: Color.green,
        warning: Color.orange,

        userBubble: Color.accentColor.opacity(0.12),
        assistantBubble: Color(nsColor: .controlBackgroundColor),
        codeSurface: Color(nsColor: .textBackgroundColor),
        codeText: Color(nsColor: .textColor),
        inlineCodeBackground: Color(nsColor: .quaternaryLabelColor).opacity(0.5)
    )
}

// MARK: - Environment Key

private struct AIElementsThemeKey: EnvironmentKey {
    static let defaultValue = AIElementsTheme.default
}

extension EnvironmentValues {
    public var aiElementsTheme: AIElementsTheme {
        get { self[AIElementsThemeKey.self] }
        set { self[AIElementsThemeKey.self] = newValue }
    }
}
