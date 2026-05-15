using Pivox.Shared.UI;

namespace Pivox.UI;

/// <summary>
/// Realizes <see cref="ThemeColor"/> tokens as appearance-aware
/// <see cref="NSColor"/> values. Each custom color is built via
/// <see cref="NSColor.GetColor(string?, Func{NSAppearance, NSColor})"/> so it
/// auto-flips when the system switches between light, dark, and
/// increased-contrast appearances without any reload code on our side.
///
/// SwiftUI's <c>pivoxTheme</c> environment object gets this behavior for
/// free because <c>Color</c> is appearance-aware by construction. AppKit
/// needs the explicit dynamic-provider closure — once wired here, the rest
/// of the macOS app calls <see cref="NS"/> and never thinks about it again.
///
/// Token values mirror the SwiftUI Theme.swift palette
/// (Core/Foundation/Theme.swift). Where the SwiftUI side reads a
/// semantic AppKit color (<c>NSColor.controlAccentColor</c> etc.) we
/// forward to the same source; where it picks a custom hex, we inline
/// the hex with light/dark pairs.
/// </summary>
public static class ThemeColors
{
    /// <summary>Resolve a token to its <see cref="NSColor"/> realization.
    /// Backing fields are cached so repeated calls return the same
    /// instance.</summary>
    public static NSColor NS(ThemeColor token) => token switch
    {
        ThemeColor.Background          => s_background,
        ThemeColor.Surface             => s_surface,
        ThemeColor.Accent              => NSColor.ControlAccent,
        ThemeColor.ProminentButtonText => NSColor.White,
        ThemeColor.Foreground          => NSColor.Label,
        ThemeColor.SecondaryForeground => NSColor.SecondaryLabel,
        ThemeColor.Border              => NSColor.Separator,
        ThemeColor.Destructive         => NSColor.SystemRed,
        _ => throw new ArgumentOutOfRangeException(nameof(token), token, null),
    };

    // ───── private appearance-aware backings ──────────────────────────

    // Background: window canvas. The solid window-background surface
    // that the auth card's NSGlassEffectView refracts from. Per WWDC
    // 2025 session 310, the new design uses a solid window background
    // (not NSVisualEffectView) so the floating Liquid Glass card has
    // a definite surface to sample; see Rule 16 in dotnet/CLAUDE.md.
    private static readonly NSColor s_background
        = NSColor.GetColor("PivoxBackground", appearance =>
            IsDark(appearance)
                ? NSColor.FromRgba(0.07f, 0.07f, 0.09f, 1f)
                : NSColor.FromRgba(0.97f, 0.97f, 0.99f, 1f));

    // Surface: card / panel background. One step up from Background.
    // Used as the solid fill behind the auth card's inner glass material.
    private static readonly NSColor s_surface
        = NSColor.GetColor("PivoxSurface", appearance =>
            IsDark(appearance)
                ? NSColor.FromRgba(0.12f, 0.12f, 0.15f, 1f)
                : NSColor.FromRgba(1.00f, 1.00f, 1.00f, 1f));

    private static bool IsDark(NSAppearance appearance)
    {
        // FindBestMatch returns the canonical name even when the
        // appearance is an accessibility variant
        // (DarkAquaIncreaseContrast etc.) — it walks the same lookup
        // path AppKit uses internally to resolve dynamic colors.
        // NSAppearance.NameAqua / NameDarkAqua are NSString; cast to
        // string for the string[] overload.
        var match = appearance.FindBestMatch(new[]
        {
            (string)NSAppearance.NameAqua,
            (string)NSAppearance.NameDarkAqua,
        });
        return match == (string)NSAppearance.NameDarkAqua;
    }
}
