using Microsoft.UI.Text;
using Microsoft.UI.Xaml.Media;
using Pivox.Shared.UI;
using Windows.UI.Text;

namespace Pivox.UI;

/// <summary>
/// Realizes <see cref="ThemeFont"/> tokens as WinUI font specs.
/// Parallels macOS <c>ThemeFonts.cs</c> (returns <c>NSFont</c>).
///
/// Uses Segoe UI Variable (Win 11+ default) via
/// <c>FontFamily("Segoe UI Variable")</c>. Falls back to Segoe UI
/// on Win 10.
/// </summary>
public static class ThemeFonts
{
    public static (FontFamily Family, double Size, FontWeight Weight) Get(ThemeFont token) => token switch
    {
        ThemeFont.BrandTitle => (s_family, 28, FontWeights.SemiBold),
        ThemeFont.Title      => (s_family, 17, FontWeights.SemiBold),
        ThemeFont.Body       => (s_family, 14, FontWeights.Normal),
        ThemeFont.BodySmall  => (s_family, 12, FontWeights.Normal),
        _ => throw new ArgumentOutOfRangeException(nameof(token), token, null),
    };

    private static readonly FontFamily s_family = new("Segoe UI Variable");
}
