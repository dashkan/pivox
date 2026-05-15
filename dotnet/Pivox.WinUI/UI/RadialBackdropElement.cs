using System.Numerics;
using Microsoft.UI.Composition;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Hosting;
using Windows.UI;
using Windows.UI.ViewManagement;

namespace Pivox.UI;

/// <summary>
/// Paints two accent-tinted radial gradients behind the auth card —
/// one top-leading (0.28 alpha, radius 520px), one bottom-trailing
/// (0.18 alpha, radius 620px). Mirrors macOS <c>RadialBackdropView</c>.
///
/// Uses <see cref="CompositionRadialGradientBrush"/> in absolute
/// mapping mode so radii are in pixels (matching the macOS values).
/// </summary>
public sealed partial class RadialBackdropElement : Grid
{
    private SpriteVisual? _topVisual;
    private SpriteVisual? _bottomVisual;
    private readonly UISettings _uiSettings = new();
    private DispatcherQueue? _dispatcher;

    public RadialBackdropElement()
    {
        Loaded += OnLoaded;
        Unloaded += OnUnloaded;
        SizeChanged += OnSizeChanged;
        _uiSettings.ColorValuesChanged += OnSystemColorsChanged;
    }

    private void OnLoaded(object sender, RoutedEventArgs e)
    {
        _dispatcher = DispatcherQueue.GetForCurrentThread();
        BuildVisuals();
        UpdateVisualLayout();
    }

    private void OnUnloaded(object sender, RoutedEventArgs e)
    {
        _uiSettings.ColorValuesChanged -= OnSystemColorsChanged;
    }

    private void OnSizeChanged(object sender, SizeChangedEventArgs e)
        => UpdateVisualLayout();

    private void OnSystemColorsChanged(UISettings sender, object args)
    {
        // Fires on a background thread — marshal to UI.
        _dispatcher?.TryEnqueue(() =>
        {
            BuildVisuals();
            UpdateVisualLayout();
        });
    }

    private void BuildVisuals()
    {
        var compositor = ElementCompositionPreview.GetElementVisual(this).Compositor;
        var accent = ThemeBrushes.AccentColor();

        _topVisual = CreateRadialVisual(
            compositor, accent, alpha: 0.28f, radius: 520,
            centerX: 0, centerY: 0);

        _bottomVisual = CreateRadialVisual(
            compositor, accent, alpha: 0.18f, radius: 620,
            centerX: 0, centerY: 0); // center set in UpdateVisualLayout

        var container = compositor.CreateContainerVisual();
        container.Children.InsertAtTop(_topVisual);
        container.Children.InsertAtTop(_bottomVisual);

        ElementCompositionPreview.SetElementChildVisual(this, container);
    }

    private static SpriteVisual CreateRadialVisual(
        Compositor compositor, Color accent, float alpha, float radius,
        float centerX, float centerY)
    {
        var brush = compositor.CreateRadialGradientBrush();

        // Absolute mode: center and radius in pixels, not 0-1 relative.
        brush.MappingMode = CompositionMappingMode.Absolute;
        brush.EllipseCenter = new Vector2(centerX, centerY);
        brush.EllipseRadius = new Vector2(radius, radius);

        var accentWithAlpha = Color.FromArgb(
            (byte)(alpha * 255), accent.R, accent.G, accent.B);
        var transparent = Color.FromArgb(0, accent.R, accent.G, accent.B);

        brush.ColorStops.Add(compositor.CreateColorGradientStop(0.0f, accentWithAlpha));
        brush.ColorStops.Add(compositor.CreateColorGradientStop(1.0f, transparent));

        var visual = compositor.CreateSpriteVisual();
        visual.Brush = brush;
        return visual;
    }

    private void UpdateVisualLayout()
    {
        if (_topVisual is null || _bottomVisual is null) return;

        var w = (float)ActualWidth;
        var h = (float)ActualHeight;
        if (w <= 0 || h <= 0) return;

        // Both visuals span the full element.
        _topVisual.Size = new Vector2(w, h);
        _bottomVisual.Size = new Vector2(w, h);

        // Top-leading: gradient center at pixel (0, 0).
        if (_topVisual.Brush is CompositionRadialGradientBrush topBrush)
        {
            topBrush.EllipseCenter = new Vector2(0, 0);
        }

        // Bottom-trailing: gradient center at pixel (w, h).
        if (_bottomVisual.Brush is CompositionRadialGradientBrush bottomBrush)
        {
            bottomBrush.EllipseCenter = new Vector2(w, h);
        }
    }
}
