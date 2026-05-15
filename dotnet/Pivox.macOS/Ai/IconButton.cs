using AppKit;
using CoreGraphics;
using Foundation;
using ObjCRuntime;
using Pivox.Shared.UI;
using Pivox.UI;

namespace Pivox.Ai;

/// <summary>
/// AppKit translation of the SwiftUI <c>IconButton</c> in
/// <c>native/.../Core/Foundation/Buttons/IconButton.swift</c>. Same
/// visual contract: square hit target around a centered SF Symbol,
/// optional hover background tint, pointing-hand cursor on hover,
/// optional "latched" state that re-tints the glyph with the accent
/// color.
///
/// <list type="bullet">
/// <item><b>Hit target</b>: 32×32 pt (matches the SwiftUI metric).</item>
/// <item><b>Glyph color</b>: secondary (resting) → accent (latched
///   via <see cref="IsOn"/>) → destructive (when
///   <see cref="IsDestructive"/>).</item>
/// <item><b>Hover background</b>: subtle rounded-rect fill, opt-in
///   via the constructor's <c>showsHoverBackground</c> flag (same as
///   SwiftUI). Off for tight rows that already have their own
///   background; on for chrome icons where the affordance cue helps.</item>
/// <item><b>Cursor</b>: pointing-hand on hover (HIG-correct
///   "this clicks" cue).</item>
/// <item><b>Tooltip</b>: routed through <see cref="NSView.ToolTip"/>.
///   AppKit's tooltip is reliable on borderless buttons, no shim
///   needed like the SwiftUI side.</item>
/// </list>
/// </summary>
internal sealed class IconButton : NSButton
{
    private const float CornerRadius = 6;

    private readonly string _accessibilityLabel;
    private readonly bool _showsHoverBackground;
    private bool _isOn;
    private bool _isDestructive;
    private bool _isHovered;
    private NSTrackingArea? _trackingArea;

    public IconButton(
        string systemSymbolName,
        string accessibilityLabel,
        string? toolTip = null,
        bool isOn = false,
        bool isDestructive = false,
        bool showsHoverBackground = true)
    {
        ArgumentNullException.ThrowIfNull(systemSymbolName);
        ArgumentNullException.ThrowIfNull(accessibilityLabel);

        _accessibilityLabel = accessibilityLabel;
        _showsHoverBackground = showsHoverBackground;
        _isOn = isOn;
        _isDestructive = isDestructive;

        Title = "";
        Bordered = false;
        BezelStyle = NSBezelStyle.SmallSquare;
        ImagePosition = NSCellImagePosition.ImageOnly;
        ImageScaling = NSImageScale.ProportionallyDown;
        TranslatesAutoresizingMaskIntoConstraints = false;
        WantsLayer = true;
        Layer!.CornerRadius = CornerRadius;

        // SF Symbol with accessibility description so VoiceOver reads
        // a sensible name when the user lands on the button.
        Image = NSImage.GetSystemSymbol(systemSymbolName, accessibilityLabel);
        // Glyph size = ThemeMetrics.IconToolbarSize (17pt Medium),
        // matching SwiftUI's `iconToolbar: .system(size: 17, weight: .medium)`
        // — the macOS toolbar-button convention.
        SymbolConfiguration = NSImageSymbolConfiguration.Create(
            ThemeMetrics.IconToolbarSize,
            (double)NSFontWeight.Medium,
            NSImageSymbolScale.Medium);

        ContentTintColor = ResolveForegroundColor();
        ToolTip = toolTip ?? accessibilityLabel;
        // NSButton's AccessibilityLabel binding is read-only (the
        // setter exists on NSView but is shadowed by NSButton's
        // override). VoiceOver still gets a sensible name from the
        // SF Symbol's accessibility description (passed to
        // GetSystemSymbol above), plus the tooltip. If we later need
        // an explicit override, route through the NSAccessibility
        // protocol's SetAccessibilityLabel selector via PerformSelector.

        NSLayoutConstraint.ActivateConstraints(new[]
        {
            WidthAnchor.ConstraintEqualTo(ThemeMetrics.IconButtonHitTarget),
            HeightAnchor.ConstraintEqualTo(ThemeMetrics.IconButtonHitTarget),
        });
    }

    /// <summary>Latched state — when true, the glyph is tinted with
    /// <see cref="ThemeColor.Accent"/>. Used for "primary action
    /// currently enabled" (composer send glyph while text is
    /// non-empty) and for toggle feedback (future
    /// thumbs-up-after-voting). Suppresses
    /// <see cref="IsDestructive"/> if both are set — destructive is
    /// the louder signal; the latched/destructive combination isn't
    /// a real state.</summary>
    public bool IsOn
    {
        get => _isOn;
        set
        {
            if (_isOn == value) return;
            _isOn = value;
            ContentTintColor = ResolveForegroundColor();
        }
    }

    /// <summary>Destructive state — when true, the glyph is tinted
    /// with <see cref="ThemeColor.Destructive"/>. Used for the
    /// composer's stop button while a stream is in flight.</summary>
    public bool IsDestructive
    {
        get => _isDestructive;
        set
        {
            if (_isDestructive == value) return;
            _isDestructive = value;
            ContentTintColor = ResolveForegroundColor();
        }
    }

    public override void UpdateTrackingAreas()
    {
        base.UpdateTrackingAreas();
        if (_trackingArea is not null)
        {
            RemoveTrackingArea(_trackingArea);
        }
        _trackingArea = new NSTrackingArea(
            Bounds,
            NSTrackingAreaOptions.MouseEnteredAndExited
                | NSTrackingAreaOptions.ActiveInKeyWindow
                | NSTrackingAreaOptions.InVisibleRect,
            this,
            null);
        AddTrackingArea(_trackingArea);
    }

    public override void MouseEntered(NSEvent theEvent)
    {
        base.MouseEntered(theEvent);
        if (!Enabled) return;
        _isHovered = true;
        // Pointing-hand cursor — "this clicks" affordance, matches
        // the SwiftUI .pointingHandCursor() modifier. Push/pop
        // semantics are safe for nested hover regions.
        NSCursor.PointingHandCursor.Push();
        if (_showsHoverBackground)
        {
            Layer!.BackgroundColor = ResolveHoverBackground().CGColor;
        }
    }

    public override void MouseExited(NSEvent theEvent)
    {
        base.MouseExited(theEvent);
        if (!_isHovered) return;
        _isHovered = false;
        // NSCursor.Pop is an instance method in the binding (maps to
        // `-[NSCursor pop]`), even though the documented ObjC class
        // method `+[NSCursor pop]` is also valid. The instance form
        // pops the top of the cursor stack regardless of receiver
        // identity, so any NSCursor instance works.
        NSCursor.PointingHandCursor.Pop();
        if (_showsHoverBackground)
        {
            Layer!.BackgroundColor = NSColor.Clear.CGColor;
        }
    }

    private NSColor ResolveForegroundColor()
    {
        if (_isDestructive) return ThemeColors.NS(ThemeColor.Destructive);
        if (_isOn) return ThemeColors.NS(ThemeColor.Accent);
        return ThemeColors.NS(ThemeColor.SecondaryForeground);
    }

    private static NSColor ResolveHoverBackground()
    {
        // Route through the theme's HoverFill token — matches
        // SwiftUI's `theme.hoverFill = secondaryLabel.opacity(0.12)`.
        return ThemeColors.NS(ThemeColor.HoverFill);
    }
}
