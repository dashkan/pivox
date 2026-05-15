using AppKit;
using CoreAnimation;
using CoreGraphics;
using Foundation;

namespace Pivox.Ai;

/// <summary>
/// Animated iridescent rainbow border — the AppKit translation of
/// the SwiftUI <c>AIShimmer</c> view modifier in
/// <c>native/.../Core/Foundation/Effects/AIShimmer.swift</c>.
///
/// <para><b>How it works.</b> A <see cref="CAGradientLayer"/> with
/// <c>type=Conic</c> renders the same pastel rainbow as the SwiftUI
/// reference (pink → coral → gold → sky → lavender). The gradient
/// is rotated continuously via a <see cref="CABasicAnimation"/>
/// against <c>transform.rotation.z</c>. To make the gradient appear
/// only along the rounded-rect border (not filling the interior),
/// the layer is masked with a <see cref="CAShapeLayer"/> whose path
/// is a stroked rounded rect — i.e. an outline at <c>lineWidth</c>
/// thickness. Only pixels under that stroke pass through the mask;
/// everything else is transparent. Net effect: a rotating
/// gradient stroke around the rounded rect, identical to the
/// SwiftUI <c>strokeBorder(AngularGradient(...))</c>.</para>
///
/// <para><b>Intensity.</b> Scales both the alpha and the stroke
/// width — ambient (0.30–0.40) when the input is unfocused,
/// prominent (0.95+) when focused. Driven by the consumer via
/// <see cref="SetIntensity"/>, which animates the change over
/// 250ms (matching the SwiftUI's
/// <c>.animation(.easeInOut(duration: 0.25), value: focused)</c>).</para>
///
/// <para><b>Lifecycle.</b> Attach to a host view's layer via
/// <see cref="Attach"/>; the shimmer layer is added as a sublayer
/// and tracks the host's bounds via the parent's auto-resize.
/// Continuous rotation runs as long as the layer is in a window;
/// CoreAnimation pauses it when offscreen, so the cost at rest is
/// negligible.</para>
///
/// <para><b>Why CALayer, not Core Image or Metal.</b> A pure CALayer
/// stack runs on the compositor without main-thread cost. The
/// SwiftUI reference uses <see cref="CFTimeInterval"/>-keyed
/// <c>TimelineView(.animation)</c> which is the SwiftUI-side
/// equivalent of a free-running compositor animation. Both
/// approaches give 60fps with zero <c>setNeedsDisplay</c> churn.</para>
/// </summary>
internal sealed class AIShimmerLayer
{
    /// <summary>Pastel rainbow palette. Identical RGB stops to
    /// <c>AIShimmerPalette.stops</c> in
    /// <c>native/.../Core/Foundation/Effects/AIShimmer.swift</c>.
    /// Last stop equals first for a seamless wrap.</summary>
    private static readonly NSColor[] Palette =
    [
        NSColor.FromRgba(1.00f, 0.50f, 0.70f, 1f),   // pink
        NSColor.FromRgba(1.00f, 0.68f, 0.50f, 1f),   // coral
        NSColor.FromRgba(1.00f, 0.85f, 0.45f, 1f),   // gold
        NSColor.FromRgba(0.58f, 0.88f, 1.00f, 1f),   // sky
        NSColor.FromRgba(0.72f, 0.60f, 0.96f, 1f),   // lavender
        NSColor.FromRgba(1.00f, 0.50f, 0.70f, 1f),   // pink (wrap)
    ];

    private readonly CAGradientLayer _gradientLayer;
    private readonly CAShapeLayer _maskLayer;
    private readonly float _cornerRadius;
    private float _lineWidth;
    private double _intensity;

    public AIShimmerLayer(float cornerRadius, float lineWidth = 1.5f, double periodSeconds = 6.0)
    {
        _cornerRadius = cornerRadius;
        _lineWidth = lineWidth;
        _intensity = 0.35;

        // Conic gradient covers the full layer bounds. The mask
        // restricts visible output to the rounded-rect stroke.
        _gradientLayer = new CAGradientLayer
        {
            LayerType = CAGradientLayerType.Conic,
            StartPoint = new CGPoint(0.5, 0.5),
            EndPoint = new CGPoint(1.0, 0.5),
            Colors = Palette.Select(c => c.CGColor).ToArray(),
            Opacity = (float)_intensity,
        };

        _maskLayer = new CAShapeLayer
        {
            FillColor = NSColor.Clear.CGColor,
            StrokeColor = NSColor.Black.CGColor,
            LineWidth = _lineWidth,
        };
        _gradientLayer.Mask = _maskLayer;

        var rotation = CABasicAnimation.FromKeyPath("transform.rotation.z");
        rotation.From = NSNumber.FromDouble(0);
        rotation.To = NSNumber.FromDouble(2 * Math.PI);
        rotation.Duration = periodSeconds;
        rotation.RepeatCount = float.PositiveInfinity;
        // RemovedOnCompletion=false + AnimationKey lets the
        // animation persist across layoutSubviews / re-display
        // cycles. Otherwise CoreAnimation strips it after the first
        // implicit transaction.
        rotation.RemovedOnCompletion = false;
        _gradientLayer.AddAnimation(rotation, "shimmer.rotation");
    }

    /// <summary>Attach this shimmer to a host view's layer. The host
    /// MUST have <c>WantsLayer = true</c>; if it doesn't, we set it
    /// (and create a CALayer) defensively. The shimmer becomes a
    /// sublayer; <see cref="UpdateFrame"/> must be called whenever
    /// the host's bounds change (from the host's <c>Layout</c>
    /// override).</summary>
    public void Attach(NSView host)
    {
        if (!host.WantsLayer) host.WantsLayer = true;
        host.Layer ??= new CALayer();
        // The shimmer sits ABOVE the host's own background but
        // BELOW any subviews — same Z-order as the SwiftUI
        // .overlay { ... } modifier, which renders the modified
        // content first and the overlay on top. AppKit's sublayer
        // insertion at index 0 puts it at the bottom of the layer
        // stack; we want it at the top, so just AddSublayer.
        host.Layer.AddSublayer(_gradientLayer);
        UpdateFrame(host.Bounds);
    }

    /// <summary>Sync the shimmer's geometry to the host's current
    /// bounds. Call from the host's <c>Layout()</c> override so the
    /// gradient and the mask path track size changes.
    ///
    /// <para><b>Pre-layout guard.</b> When called before the host
    /// has been laid out (e.g. inside <c>Attach</c> right after
    /// construction), <c>bounds</c> is empty. <c>bounds.Inset</c>
    /// would produce a negative-dimension rect, and
    /// <see cref="CGPath.FromRoundedRect"/> throws
    /// <c>ArgumentException("cornerWidth")</c> when
    /// <c>cornerWidth &gt; rect.Width / 2</c> — which is trivially
    /// true for any negative-width rect. Skip the path build in
    /// that case; the next <c>Layout()</c> call will retry with
    /// real bounds.</para></summary>
    public void UpdateFrame(CGRect bounds)
    {
        _gradientLayer.Frame = bounds;
        // Stroke is centered on the rounded-rect edge; inset by
        // half the line width so the visible stroke sits flush with
        // the bounds rather than half-clipped.
        var inset = _lineWidth / 2;
        var rect = bounds.Inset(inset, inset);
        var minDimensionForCorner = 2 * (_cornerRadius - inset);
        if (rect.Width < minDimensionForCorner
            || rect.Height < minDimensionForCorner)
        {
            // Too small for the rounded path — skip until laid out.
            _maskLayer.Path = null;
            return;
        }
        using var path = CGPath.FromRoundedRect(
            rect, _cornerRadius - inset, _cornerRadius - inset);
        _maskLayer.Path = path;
        _maskLayer.Frame = bounds;
    }

    /// <summary>Set the shimmer intensity (0..1). Animates the
    /// change over 250ms so focus transitions feel smooth rather
    /// than a hard step. Below ~0.05 the shimmer is effectively
    /// invisible.</summary>
    public void SetIntensity(double intensity, bool animated = true)
    {
        intensity = Math.Clamp(intensity, 0, 1);
        if (Math.Abs(_intensity - intensity) < 0.005) return;
        _intensity = intensity;

        if (animated)
        {
            CATransaction.Begin();
            CATransaction.AnimationDuration = 0.25;
            _gradientLayer.Opacity = (float)intensity;
            _maskLayer.LineWidth = _lineWidth * Math.Max((float)intensity, 0.5f);
            CATransaction.Commit();
        }
        else
        {
            CATransaction.Begin();
            CATransaction.DisableActions = true;
            _gradientLayer.Opacity = (float)intensity;
            _maskLayer.LineWidth = _lineWidth * Math.Max((float)intensity, 0.5f);
            CATransaction.Commit();
        }
    }
}
