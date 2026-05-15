using Pivox.Shared.UI;

namespace Pivox.UI;

/// <summary>
/// Realizes <see cref="ThemeFont"/> tokens as <see cref="NSFont"/>
/// instances. Cached per-role so repeated calls return the same
/// instance — fonts are immutable, no reason to allocate fresh ones.
///
/// SwiftUI's <c>pivoxTheme</c> typography (<c>brandTitleFont</c> /
/// <c>bodyFont</c> / <c>bodySmallFont</c>) maps 1:1 to these roles.
///
/// <see cref="NSFont.SystemFontOfSize"/> is bound as nullable but
/// Apple's contract guarantees non-null for valid sizes — null-forgiving
/// here is documented and correct.
/// </summary>
public static class ThemeFonts
{
    /// <summary>Resolve a token to its <see cref="NSFont"/> realization.</summary>
    public static NSFont NS(ThemeFont token) => token switch
    {
        ThemeFont.BrandTitle     => s_brandTitle,
        ThemeFont.Title          => s_title,
        ThemeFont.SectionHeading => s_sectionHeading,
        ThemeFont.Body           => s_body,
        ThemeFont.RowTitle       => s_rowTitle,
        ThemeFont.BodySmall      => s_bodySmall,
        _ => throw new ArgumentOutOfRangeException(nameof(token), token, null),
    };

    // ───── cached realizations ─────────────────────────────────

    // BrandTitle: app-name-level (28pt, Semibold). The "Pivox" header
    // on the login card and any future hero callouts.
    private static readonly NSFont s_brandTitle
        = NSFont.SystemFontOfSize(28, NSFontWeight.Semibold)!;

    // Title: section heading (system + 2pt, Semibold). "Signed in as
    // …", future panel headers.
    private static readonly NSFont s_title
        = NSFont.SystemFontOfSize(NSFont.SystemFontSize + 2, NSFontWeight.Semibold)!;

    // SectionHeading: empty-state + section title weight. SwiftUI's
    // `.title3.weight(.semibold)` is ~17pt semibold on macOS — one
    // size up from body to read as a heading without competing with
    // the brand title.
    private static readonly NSFont s_sectionHeading
        = NSFont.SystemFontOfSize(17, NSFontWeight.Semibold)!;

    // Body: default text (system font size). Field labels, button
    // captions when not provided by the control.
    private static readonly NSFont s_body
        = NSFont.SystemFontOfSize(NSFont.SystemFontSize)!;

    // RowTitle: body-size at semibold weight. Chat title strip,
    // table-row primary labels. SwiftUI:
    // `.body.weight(.semibold)`.
    private static readonly NSFont s_rowTitle
        = NSFont.SystemFontOfSize(NSFont.SystemFontSize, NSFontWeight.Semibold)!;

    // BodySmall: captions, dividers, status lines. One step below body.
    private static readonly NSFont s_bodySmall
        = NSFont.SystemFontOfSize(NSFont.SmallSystemFontSize)!;
}
