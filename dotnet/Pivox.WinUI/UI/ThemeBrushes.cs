using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Media;
using Pivox.Shared.UI;
using Windows.UI;

namespace Pivox.UI;

/// <summary>
/// Realizes <see cref="ThemeColor"/> tokens as WinUI brushes/colors.
/// Parallels macOS <c>ThemeColors.cs</c> (returns <c>NSColor</c>).
///
/// Tokens that map to system theme resources use
/// <c>{ThemeResource ...}</c> keys so light/dark/accent changes
/// propagate automatically. Custom tokens (Background, Surface) use
/// explicit RGBA pairs matching the macOS values.
/// </summary>
public static class ThemeBrushes
{
    public static Brush Brush(ThemeColor token) => token switch
    {
        ThemeColor.Accent              => AccentBrush(),
        ThemeColor.ProminentButtonText => new SolidColorBrush(Colors.White),
        ThemeColor.Foreground          => ResourceBrush("TextFillColorPrimaryBrush"),
        ThemeColor.SecondaryForeground => ResourceBrush("TextFillColorSecondaryBrush"),
        ThemeColor.Border              => ResourceBrush("ControlStrokeColorDefaultBrush"),
        ThemeColor.Destructive         => new SolidColorBrush(ColorFrom(0xE8, 0x3B, 0x3B)),
        ThemeColor.Background          => ResourceBrush("SolidBackgroundFillColorBaseBrush"),
        ThemeColor.Surface             => ResourceBrush("LayerFillColorDefaultBrush"),
        _ => throw new ArgumentOutOfRangeException(nameof(token), token, null),
    };

    /// <summary>The system accent color — tracks the user's Windows
    /// personalization setting live.</summary>
    public static Color AccentColor()
    {
        if (Application.Current.Resources.TryGetValue("SystemAccentColor", out var obj)
            && obj is Color c)
            return c;
        return Colors.SlateBlue; // fallback
    }

    public static Color AccentColorAt(float alpha)
    {
        var c = AccentColor();
        return Color.FromArgb((byte)(alpha * 255), c.R, c.G, c.B);
    }

    private static SolidColorBrush AccentBrush()
        => new(AccentColor());

    private static Brush ResourceBrush(string key)
    {
        if (Application.Current.Resources.TryGetValue(key, out var obj)
            && obj is Brush b)
            return b;
        return new SolidColorBrush(Colors.Magenta); // visible fallback
    }

    private static Color ColorFrom(int r, int g, int b)
        => Color.FromArgb(255, (byte)r, (byte)g, (byte)b);
}
