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

    // Text roles. Use these instead of literals like
    // `.title3.weight(.semibold)` so a single theme change
    // propagates everywhere.
    /// Brand/splash title (sign-in, register, launch screens).
    /// Prominent standalone type — not the same as a dialog's
    /// pageTitleFont (which is smaller and meant to sit next to an
    /// action button).
    public let brandTitleFont: Font
    /// Page-level title (dialog/page headers). 17pt semibold.
    public let pageTitleFont: Font
    /// Subsection heading (Profile, Email, Danger zone). 15pt semibold.
    public let sectionHeadingFont: Font
    /// Primary row title (e.g. "Delete account" row label). Body
    /// weight with semibold for emphasis.
    public let rowTitleFont: Font
    /// Small label above a field value (Display name, Email, etc.).
    public let fieldLabelFont: Font
    /// Status chip text (Verified / Unverified / etc.) — body-small
    /// at medium weight.
    public let statusBadgeFont: Font

    // MARK: Icon sizing
    //
    // Semantic icon fonts. Starting point is `iconToolbar` used
    // uniformly everywhere (even tight inline rows) — matches Apple's
    // toolbar-icon convention (Music/Finder/Mail: 17pt .medium) and
    // keeps visual language consistent. `iconInline` is kept as a
    // smaller variant we can opt into later for specific spots if a
    // real need surfaces. `iconEmptyState` sizes the large placeholder
    // glyphs in empty/error states.
    public let iconToolbar: Font
    public let iconInline: Font
    public let iconEmptyState: Font

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
    /// Subtle fill used on hover affordances (button hover backgrounds,
    /// click-to-edit hints). Adapts in light/dark mode via the
    /// secondary-label color at low opacity so the hint reads as
    /// chrome rather than a colored highlight.
    public let hoverFill: Color
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

    // MARK: Sidebar
    //
    // Selection-pill fill for sidebar rows. Music / Mail / Finder
    // use a neutral-gray pill (no accent tint — text/icon carry the
    // accent color instead). Stays the same color whether the window
    // is active or inactive; only the text+icon toggles between
    // accent and secondary via emphasis.
    public let sidebarSelectionFill: Color

    // MARK: Button
    //
    // Foreground color for `.borderedProminent` / tinted-glass buttons.
    // macOS 26 renders prominent buttons as a translucent tint
    // resolved from the accent color and picks its own "harmonizing"
    // foreground, which collapses to low-contrast (violet text on
    // violet fill) with brand accents. Forcing white across the app
    // guarantees contrast regardless of which system accent a user
    // has set. Changes here propagate to every prominent button via
    // `PivoxPrimaryButton` and any Label that reads the token.
    public let prominentButtonText: Color

    // MARK: Default

    public static let `default` = PivoxTheme(
        bodyFont: .body,
        bodySmallFont: .callout,
        captionFont: .caption,
        headingFont: .headline,
        codeFont: .system(.body, design: .monospaced),

        brandTitleFont: .largeTitle.weight(.bold),
        pageTitleFont: .title2.weight(.semibold),
        sectionHeadingFont: .title3.weight(.semibold),
        rowTitleFont: .body.weight(.semibold),
        fieldLabelFont: .subheadline,
        statusBadgeFont: .callout.weight(.medium),

        iconToolbar: .system(size: 17, weight: .medium),
        iconInline: .system(size: 14, weight: .medium),
        iconEmptyState: .largeTitle,

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
        hoverFill: Color(nsColor: .secondaryLabelColor).opacity(0.12),
        destructive: Color(nsColor: .systemRed),
        destructiveSubtle: Color(nsColor: .systemRed).opacity(0.15),
        success: Color(nsColor: .systemGreen),
        warning: Color(nsColor: .systemOrange),

        userBubble: Color.accentColor.opacity(0.12),
        assistantBubble: Color(nsColor: .controlBackgroundColor),
        codeSurface: Color(nsColor: .textBackgroundColor),
        codeText: Color(nsColor: .textColor),
        inlineCodeBackground: Color(nsColor: .quaternaryLabelColor).opacity(0.5),

        sidebarSelectionFill: Color(nsColor: .secondarySystemFill),

        prominentButtonText: .white
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

// MARK: - Convenience modifiers
//
// These read the theme from the environment so call sites don't need
// `@Environment(\.pivoxTheme)` plumbing for one-off icon sizing.
// Prefer these over hand-written `.font(.system(size: 17, weight: .medium))`.

extension View {
    /// Standard toolbar/action icon sizing. 17pt .medium by default;
    /// adapts if the theme is overridden via environment.
    public func pivoxIconToolbar() -> some View {
        modifier(PivoxIconToolbarModifier())
    }

    /// Compact inline icon sizing. 14pt .medium by default.
    public func pivoxIconInline() -> some View {
        modifier(PivoxIconInlineModifier())
    }

    /// Large empty/error state glyph sizing.
    public func pivoxIconEmptyState() -> some View {
        modifier(PivoxIconEmptyStateModifier())
    }
}

private struct PivoxIconToolbarModifier: ViewModifier {
    @Environment(\.pivoxTheme) private var theme
    func body(content: Content) -> some View {
        content.font(theme.iconToolbar)
    }
}

private struct PivoxIconInlineModifier: ViewModifier {
    @Environment(\.pivoxTheme) private var theme
    func body(content: Content) -> some View {
        content.font(theme.iconInline)
    }
}

private struct PivoxIconEmptyStateModifier: ViewModifier {
    @Environment(\.pivoxTheme) private var theme
    func body(content: Content) -> some View {
        content.font(theme.iconEmptyState)
    }
}

// MARK: - Label style
//
// `Label(text, systemImage:)` sizes both the title and the icon from
// the ambient font — which at default button sizing gives a small
// icon (~13pt). For buttons we want the "chat icon" feel (17pt medium)
// without upsizing the text, so this label style pins the icon to the
// toolbar-icon token and leaves the title at the ambient font.

public struct PivoxIconLabelStyle: LabelStyle {
    @Environment(\.pivoxTheme) private var theme

    public init() {}

    public func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 6) {
            configuration.icon.font(theme.iconToolbar)
            configuration.title
        }
    }
}

extension LabelStyle where Self == PivoxIconLabelStyle {
    /// Button-friendly label style that upsizes the icon to
    /// `theme.iconToolbar` while leaving the title at the button's
    /// ambient font. Matches the visual weight of our chat panel
    /// IconButtons on text+icon buttons elsewhere in the app.
    public static var pivoxIcon: PivoxIconLabelStyle { PivoxIconLabelStyle() }
}
