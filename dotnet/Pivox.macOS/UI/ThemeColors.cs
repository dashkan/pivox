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
    /// instance.
    ///
    /// <para><b>Source of truth: SwiftUI.</b> Token-to-NSColor
    /// mappings mirror <c>PivoxTheme.default</c> in
    /// <c>native/.../Core/Foundation/Theme/Theme.swift</c>:
    /// <c>background = Color(nsColor: .windowBackgroundColor)</c>,
    /// <c>backgroundRaised = .controlBackgroundColor</c>, etc. The
    /// system semantic colors are appearance-aware (auto-flip
    /// light/dark + accessibility variants) without us inventing a
    /// dynamic provider, and they keep the dotnet shell visually
    /// identical to the SwiftUI shell on the same OS version.</para></summary>
    public static NSColor NS(ThemeColor token) => token switch
    {
        ThemeColor.Background          => NSColor.WindowBackground,
        ThemeColor.BackgroundRaised    => NSColor.ControlBackground,
        ThemeColor.Accent              => NSColor.ControlAccent,
        ThemeColor.ProminentButtonText => NSColor.White,
        ThemeColor.Foreground          => NSColor.Label,
        ThemeColor.SecondaryForeground => NSColor.SecondaryLabel,
        ThemeColor.TertiaryForeground  => NSColor.TertiaryLabel,
        ThemeColor.Border              => NSColor.Separator,
        ThemeColor.Destructive         => NSColor.SystemRed,
        // HoverFill: secondaryLabel.opacity(0.12) — matches the
        // SwiftUI theme's `hoverFill` exactly. NSColor's
        // ColorWithAlphaComponent preserves the appearance-aware
        // dynamic-color behavior; the alpha clamp is constant.
        ThemeColor.HoverFill           => NSColor.SecondaryLabel.ColorWithAlphaComponent(0.12f),
        // UserBubble: accent.opacity(0.12) — matches SwiftUI's
        // `userBubble`. Accent tint at 12% reads as "the user's
        // turn" without competing with the markdown-rendered
        // assistant content next to it.
        ThemeColor.UserBubble          => NSColor.ControlAccent.ColorWithAlphaComponent(0.12f),
        _ => throw new ArgumentOutOfRangeException(nameof(token), token, null),
    };
}
