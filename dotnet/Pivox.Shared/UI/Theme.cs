// Cross-platform theme token catalog. Each platform layer realizes
// the tokens into its native color/font types:
//   - macOS: dynamic-provider NSColor values that auto-flip on
//     light/dark appearance change.
//   - WinUI: SolidColorBrush resolved from ThemeResource at render
//     time.
//
// Token names mirror the SwiftUI pivoxTheme environment in
// native/platform/macos/swift/Core/Foundation/Theme.swift so a
// designer working from the SwiftUI app maps 1:1.
//
// Why an enum and not a string: the closed set is a forcing function.
// Adding a token = adding an enum case = compile error everywhere
// it's not realized. Stringly-typed tokens drift silently.
//
// Platform layers consume tokens through their own resolver (e.g.
// ThemeColors.NS(ThemeColor.Accent)), not a shared dictionary — the
// resolver knows the native color type and how to make it
// appearance-aware.

namespace Pivox.Shared.UI;

/// <summary>
/// Cross-platform font tokens. Each platform realizes them into its
/// native font type (<c>NSFont</c> on macOS, <c>FontFamily</c>+size on
/// WinUI). Same closed-set rule as <see cref="ThemeColor"/>: enum case
/// per role, not stringly-typed.
///
/// Roles mirror the SwiftUI <c>pivoxTheme</c> typography scale in
/// <c>native/.../Core/Foundation/Theme.swift</c> (<c>brandTitleFont</c>,
/// <c>bodyFont</c>, <c>bodySmallFont</c>) so designers reading either
/// codebase map roles 1:1.
/// </summary>
public enum ThemeFont
{
    /// <summary>App-name-level heading. "Pivox" on the login card.
    /// SwiftUI: <c>brandTitleFont = .largeTitle.weight(.bold)</c>.</summary>
    BrandTitle,

    /// <summary>Section heading. One step above body weight + size.
    /// SwiftUI: <c>pageTitleFont = .title2.weight(.semibold)</c>.</summary>
    Title,

    /// <summary>Empty-state and section heading (one step below
    /// <see cref="Title"/>). SwiftUI:
    /// <c>sectionHeadingFont = .title3.weight(.semibold)</c>.</summary>
    SectionHeading,

    /// <summary>Default body text. Buttons, fields, paragraph copy.
    /// SwiftUI: <c>bodyFont = .body</c>.</summary>
    Body,

    /// <summary>Row-title weight: body size at semibold. Chat title
    /// strip, table-row primary labels. SwiftUI:
    /// <c>rowTitleFont = .body.weight(.semibold)</c>.</summary>
    RowTitle,

    /// <summary>Captions, dividers, secondary affordances. One step
    /// below <see cref="Body"/>. SwiftUI:
    /// <c>bodySmallFont = .callout</c>.</summary>
    BodySmall,
}

public enum ThemeColor
{
    /// <summary>Window/canvas background. Behind everything.
    /// SwiftUI realization: <c>NSColor.windowBackgroundColor</c>.</summary>
    Background,

    /// <summary>One level up from <see cref="Background"/> — control
    /// containers, composer chrome, chat-bubble fills. SwiftUI:
    /// <c>NSColor.controlBackgroundColor</c> (the SwiftUI theme's
    /// <c>backgroundRaised</c> role).</summary>
    BackgroundRaised,

    /// <summary>Primary tint (the "Pivox blue"). Accent buttons, links,
    /// focused control rings.</summary>
    Accent,

    /// <summary>Foreground that pairs with <see cref="Accent"/>-tinted
    /// surfaces (prominent button text, accent-on-accent labels).</summary>
    ProminentButtonText,

    /// <summary>Primary text/foreground. SwiftUI: <c>labelColor</c>.</summary>
    Foreground,

    /// <summary>De-emphasized text — captions, helper copy.
    /// SwiftUI: <c>secondaryLabelColor</c>.</summary>
    SecondaryForeground,

    /// <summary>Tertiary text — placeholders, empty-state hints, very
    /// low-emphasis labels. SwiftUI: <c>tertiaryLabelColor</c>.</summary>
    TertiaryForeground,

    /// <summary>Subtle separators, control outlines, low-emphasis
    /// dividers. SwiftUI: <c>separatorColor</c>.</summary>
    Border,

    /// <summary>Error / destructive states — invalid-credential
    /// messaging, delete affordances, stop-streaming glyph.
    /// SwiftUI: <c>systemRed</c>.</summary>
    Destructive,

    /// <summary>Subtle hover background — icon-button hover state,
    /// click-to-edit hint backgrounds. SwiftUI:
    /// <c>secondaryLabelColor.opacity(0.12)</c>.</summary>
    HoverFill,

    /// <summary>User-message bubble fill. SwiftUI:
    /// <c>accent.opacity(0.12)</c>. Distinct from secondary-label
    /// tint: user turns are accent-flavored to read as the user's
    /// own contribution.</summary>
    UserBubble,
}

/// <summary>
/// Cross-platform numeric design tokens — spacing, corner radii,
/// hairline thickness, fixed widths. Unlike <see cref="ThemeColor"/>
/// and <see cref="ThemeFont"/>, metrics don't need a platform
/// realizer: AppKit takes <c>NFloat</c> (implicit from <c>float</c>),
/// WinUI takes <c>double</c> (implicit widen from <c>float</c>).
/// Same number, both sides.
///
/// Note on float vs double: WinUI's <c>Thickness</c>, <c>CornerRadius</c>,
/// and Margin take <c>double</c>; AppKit's geometry uses <c>NFloat</c>
/// which has implicit conversion from <c>float</c> but NOT from
/// <c>double</c>. <c>float</c> is the cross-platform sweet spot
/// — plenty of precision for layout, no cast required on either side.
///
/// Scale is the standard 4-unit design grid. New values are added as
/// roles emerge, not as arbitrary one-off numbers in views — if you
/// find yourself reaching for an inline literal, add a constant here
/// instead.
/// </summary>
public static class ThemeMetrics
{
    // ───── 4-unit spacing scale ────────────────────────────────
    public const float SpaceXs = 4;
    public const float SpaceSm = 8;
    public const float SpaceMd = 16;
    public const float SpaceLg = 24;
    public const float SpaceXl = 32;

    // ───── layout primitives ───────────────────────────────────

    /// <summary>Hairline divider thickness (the "or" separator and any
    /// other 1pt rules). 1pt on standard DPI, scales on retina.</summary>
    public const float HairlineThickness = 1;

    /// <summary>Corner radius for a "card" — any floating surface that
    /// reads as a discrete UI element (auth card, dashboard tile,
    /// settings panel, etc.). Matches the macOS 26 design-system
    /// curvature for floating elements.</summary>
    public const float CardCornerRadius = 20;

    // ───── feature-scoped tokens (Auth, etc.) ──────────────────
    // Naming convention: feature-scoped tokens are prefixed with the
    // feature name so the scope is visible at the call site. A token
    // is general (no prefix) when ≥2 features use it; promote
    // prefixed tokens by dropping the prefix when a second consumer
    // shows up.

    /// <summary>Fixed width of the centered auth card. Wider makes
    /// the form feel too horizontal at desktop sizes; narrower
    /// cramps the fields and primary button.</summary>
    public const float AuthCardWidth = 360;

    /// <summary>Corner radius for the user-turn chat bubble. Matches
    /// the SwiftUI reference (<c>RoundedRectangle(cornerRadius: 14)</c>
    /// in <c>AIElements/Components/Message/Message.swift</c>). Bubbles
    /// are the visible "user said this" affordance — softer curvature
    /// than a card, but enough to read as a discrete pill rather than
    /// a rectangular text block.</summary>
    public const float ChatBubbleCornerRadius = 14;

    /// <summary>Square hit-target size for icon buttons. Matches
    /// SwiftUI's IconButton metric (32pt = 17pt glyph + 15pt padding,
    /// the standard macOS toolbar-button hit area).</summary>
    public const float IconButtonHitTarget = 32;

    /// <summary>Toolbar-icon glyph size. SwiftUI:
    /// <c>iconToolbar: .system(size: 17, weight: .medium)</c>.</summary>
    public const float IconToolbarSize = 17;

    /// <summary>Corner radius for inline chat controls that aren't
    /// the user bubble — composer text-area outline, future
    /// inspector chips. Smaller curvature than
    /// <see cref="ChatBubbleCornerRadius"/> for visual hierarchy.</summary>
    public const float ChatMessageCornerRadius = 8;

    /// <summary>Vertical spacing between rows in the chat transcript
    /// — fed into <c>NSTableView.IntercellSpacing</c>. Matches the
    /// SwiftUI reference (<c>intercellSpacing = NSSize(width: 0,
    /// height: 8)</c> in
    /// <c>native/.../AIChat/Transcript/ConversationTranscriptView.swift</c>).
    /// Same magnitude as <see cref="SpaceSm"/> today but feature-
    /// scoped so the two can diverge if the transcript layout ever
    /// wants tighter or looser breathing room than generic 8pt
    /// spacing.</summary>
    public const float TranscriptIntercellSpacingY = 8;
}
