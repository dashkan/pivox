namespace Pivox.UI;

/// <summary>
/// Full-width primary action button for auth flows (Sign In, Create
/// Account, Continue). Wraps an <see cref="NSButton"/> configured as
/// a tint-prominent push button at <see cref="NSControlSize.Large"/>,
/// so callers get the look right by construction.
///
/// Why a dedicated component (mirrors the SwiftUI rationale):
/// <list type="bullet">
/// <item>The loading treatment — swap label for a spinner centered in the
///   same control bounds — is the same everywhere we use this pattern.</item>
/// <item>Tint prominence + content tint settings stay encapsulated so
///   call sites can't drift.</item>
/// </list>
///
/// Sizing: the button itself has an intrinsic content size driven by its
/// title. To get "full-width", the parent applies leading/trailing
/// anchors. Mirrors the SwiftUI side, where <c>.frame(maxWidth: .infinity)</c>
/// lives on the parent container, not the button view itself.
///
/// Scope: auth-specific primary action. Destructive prominent actions
/// (Delete account) use a different tint and label pattern; keep those
/// separate.
/// </summary>
[Register("AuthPrimaryButton")]
public sealed class AuthPrimaryButton : NSButton
{
    private readonly NSProgressIndicator _spinner;
    private string _persistentTitle = "";
    private bool _isLoading;

    public AuthPrimaryButton()
    {
        // Configure the button shape.
        BezelStyle = NSBezelStyle.Push;
        ControlSize = NSControlSize.Large;
        // Primary prominence = the AppKit equivalent of SwiftUI's
        // .borderedProminent — accent-tinted fill with system-chosen
        // bright label. We deliberately do NOT set ContentTintColor:
        // letting AppKit pick the label color gives correct contrast
        // in both enabled (crisp white) and disabled (properly dimmed)
        // states. Setting it to a fixed color makes disabled-state
        // text vanish into the fill.
        TintProminence = NSTintProminence.Primary;
        Title = "";
        TranslatesAutoresizingMaskIntoConstraints = false;

        // Inline spinner used while loading. Centered in the button's
        // bounds via Auto Layout; toggled in IsLoading.
        _spinner = new NSProgressIndicator
        {
            Style = NSProgressIndicatorStyle.Spinning,
            ControlSize = NSControlSize.Small,
            Indeterminate = true,
            IsDisplayedWhenStopped = false,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        AddSubview(_spinner);
        NSLayoutConstraint.ActivateConstraints(new[]
        {
            _spinner.CenterXAnchor.ConstraintEqualTo(CenterXAnchor),
            _spinner.CenterYAnchor.ConstraintEqualTo(CenterYAnchor),
        });
    }

    /// <summary>Visible button label. While <see cref="IsLoading"/> is
    /// true the label is hidden and the spinner shows in its place;
    /// the title is restored when loading flips off, so callers don't
    /// need to remember it.
    ///
    /// Note: assigning <see cref="PrimaryTitle"/> *during* loading
    /// updates the persistent value but doesn't make the label visible
    /// until loading ends — by design, since the spinner owns the
    /// surface. If you need a "Sign In" → "Cancel" morph mid-flight
    /// (as in the SwiftUI <c>AuthPrimaryButton</c>'s OAuth-cancel
    /// state), flip <see cref="IsLoading"/> off first, then set the
    /// new <see cref="PrimaryTitle"/>.</summary>
    public string PrimaryTitle
    {
        get => _persistentTitle;
        set
        {
            _persistentTitle = value;
            if (!_isLoading) Title = value;
        }
    }

    /// <summary>True while a request is in flight. Disables the button
    /// (no click-through), hides the title, and starts the inline
    /// spinner. Idempotent.</summary>
    public bool IsLoading
    {
        get => _isLoading;
        set
        {
            if (_isLoading == value) return;
            _isLoading = value;
            if (value)
            {
                Title = "";
                _spinner.StartAnimation(null);
                Enabled = false;
            }
            else
            {
                _spinner.StopAnimation(null);
                Title = _persistentTitle;
                // IsLoading turning off does NOT re-enable the button —
                // the caller's CanSubmit gate may still want it disabled
                // (e.g., field validation failed). Caller assigns Enabled
                // separately.
            }
        }
    }
}
