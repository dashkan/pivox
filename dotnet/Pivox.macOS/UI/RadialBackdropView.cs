using CoreGraphics;
using ObjCRuntime;
using Pivox.Shared.UI;

namespace Pivox.UI;

/// <summary>
/// Window backdrop with two accent-tinted radial gradients — one
/// anchored top-leading, one bottom-trailing. Mirrors the SwiftUI
/// <c>authBackdrop</c> in <c>native/.../Auth/LoginView.swift</c>:
///
/// <code>
/// ZStack {
///   theme.background
///   RadialGradient(colors: [theme.accent.opacity(0.28), .clear],
///                  center: .topLeading, startRadius: 0, endRadius: 520)
///   RadialGradient(colors: [theme.accent.opacity(0.18), .clear],
///                  center: .bottomTrailing, startRadius: 0, endRadius: 620)
/// }
/// </code>
///
/// The accent-tinted light gives the floating <c>NSGlassEffectView</c>
/// card something visible to refract — without it, the card reads as
/// a barely-there outline in light mode (the system window
/// background is too uniform to interact with the glass material).
///
/// Coordinates: <see cref="IsFlipped"/> returns true so the origin is
/// at the top-leading corner, matching SwiftUI's coordinate system
/// (and Apple's HIG documents) — no Y-axis flip math at gradient
/// draw time.
/// </summary>
[Register("RadialBackdropView")]
public sealed class RadialBackdropView : NSView
{
    public override bool IsFlipped => true;

    public override void DrawRect(CGRect dirtyRect)
    {
        // Fill background with the system window-background color so
        // the gradient layers above it pick up the right base
        // appearance in light/dark.
        ThemeColors.NS(ThemeColor.Background).SetFill();
        NSBezierPath.FromRect(Bounds).Fill();

        var accent = ThemeColors.NS(ThemeColor.Accent);

        DrawRadial(
            from: new CGPoint(0, 0),                         // top-leading
            toCenter: new CGPoint(0, 0),
            endRadius: 520,
            accent: accent,
            alpha: 0.28f);

        DrawRadial(
            from: new CGPoint(Bounds.Width, Bounds.Height),  // bottom-trailing
            toCenter: new CGPoint(Bounds.Width, Bounds.Height),
            endRadius: 620,
            accent: accent,
            alpha: 0.18f);
    }

    /// <summary>
    /// A radial that starts at <paramref name="from"/> with radius 0
    /// (a point source) and fans out to <paramref name="endRadius"/>
    /// at the same center, fading from <paramref name="accent"/> at
    /// <paramref name="alpha"/> to fully transparent. Matches the
    /// SwiftUI RadialGradient(colors: [c, .clear], center, 0, end)
    /// shape.
    /// </summary>
    private static void DrawRadial(
        CGPoint from, CGPoint toCenter, double endRadius,
        NSColor accent, float alpha)
    {
        var gradient = new NSGradient(
            new[]
            {
                accent.ColorWithAlphaComponent(alpha),
                accent.ColorWithAlphaComponent(0f),
            },
            new[] { 0f, 1f });

        gradient.DrawFromCenterRadius(
            from, 0, toCenter, (System.Runtime.InteropServices.NFloat)endRadius,
            NSGradientDrawingOptions.None);
    }
}
