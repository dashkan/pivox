using System.ComponentModel;
using CoreGraphics;
using ObjCRuntime;
using Pivox.Shared.Auth;
using Pivox.Shared.UI;
using Pivox.UI;

namespace Pivox.Auth;

/// <summary>
/// Login form. Email + password + Google sign-in, with a shared
/// <see cref="LoginViewModel"/> driving state. The view-model lives in
/// Pivox.Shared so the WinUI port consumes the same state machine.
///
/// Layout mirrors the SwiftUI <c>LoginView.authCard</c> at the easier-views
/// scope: header / email / password / primary button / error / "or"
/// divider / Google button. Two-step SSO, MFA challenge, GitHub OAuth,
/// remember-me, and the Create-account footer are deferred — see
/// <see cref="LoginViewModel"/> doc comment for the deferred list.
///
/// Bindings: text-field <c>Changed</c> events push into the view-model;
/// view-model <see cref="INotifyPropertyChanged.PropertyChanged"/>
/// updates field state, button state, and the error label. Subscribe in
/// <see cref="ViewDidLoad"/>, unsubscribe in <see cref="Dispose"/>.
/// </summary>
[Register("LoginViewController")]
public sealed class LoginViewController : NSViewController
{
    private readonly LoginViewModel _vm;

    // Form controls. Constructed in LoadView; bound in ViewDidLoad.
    private NSTextField _emailField = null!;
    private NSSecureTextField _passwordField = null!;
    private AuthPrimaryButton _signInButton = null!;
    private NSButton _googleButton = null!;
    private NSTextField _errorLabel = null!;

    public LoginViewController(LoginViewModel vm) : base((string?)null, null)
    {
        _vm = vm ?? throw new ArgumentNullException(nameof(vm));
    }

    public override void LoadView()
    {
        // Container = a plain NSView that fills whatever rect the
        // window's content area gives us. The auth card is pinned to
        // the geometric center via Auto Layout, so the container's
        // own frame is irrelevant — let AppKit (via the window
        // controller's ContentViewController wiring) drive sizing.
        // Mirrors the SwiftUI VStack { Spacer; Group { card }; Spacer }
        // shape, where the outer frame is .infinity in both axes.
        var container = new NSView();

        var card = BuildCard();
        container.AddSubview(card);
        NSLayoutConstraint.ActivateConstraints(new[]
        {
            card.CenterXAnchor.ConstraintEqualTo(container.CenterXAnchor),
            card.CenterYAnchor.ConstraintEqualTo(container.CenterYAnchor),
            card.WidthAnchor.ConstraintEqualTo(ThemeMetrics.AuthCardWidth),
        });

        View = container;
    }

    public override void ViewDidLoad()
    {
        base.ViewDidLoad();
        WireBindings();
        // Render the initial view-model state — covers the "restored from
        // a previous session might still be signing in" case.
        ApplyState();
    }

    public override void ViewDidAppear()
    {
        base.ViewDidAppear();
        // Focus the email field on appear so keyboard input flows
        // immediately. Equivalent to SwiftUI's onAppear focusedField = .email.
        View.Window?.MakeFirstResponder(_emailField);
    }

    protected override void Dispose(bool disposing)
    {
        if (disposing)
        {
            UnwireBindings();
        }
        base.Dispose(disposing);
    }

    // ───── view tree ─────────────────────────────────────────────

    private NSView BuildCard()
    {
        // Card stack — vertical, Lg spacing. Alignment = CenterX lets
        // the header (which has intrinsic-width text labels) center
        // naturally. Children that should fill the card width (form,
        // separator, social button) get explicit width constraints
        // below — NSStackView has no built-in cross-axis "Fill"
        // alignment.
        var formStack = new NSStackView
        {
            Orientation = NSUserInterfaceLayoutOrientation.Vertical,
            Alignment = NSLayoutAttribute.CenterX,
            Spacing = ThemeMetrics.SpaceLg,
            TranslatesAutoresizingMaskIntoConstraints = false,
            EdgeInsets = new NSEdgeInsets(
                ThemeMetrics.SpaceXl, ThemeMetrics.SpaceXl,
                ThemeMetrics.SpaceXl, ThemeMetrics.SpaceXl),
        };

        var header = BuildHeader();
        var form = BuildForm();
        var separator = BuildSeparator();
        var google = BuildGoogleButton();

        formStack.AddArrangedSubview(header);
        formStack.AddArrangedSubview(form);
        formStack.AddArrangedSubview(separator);
        formStack.AddArrangedSubview(google);

        // Make the non-header children stretch to the stack's content
        // width (Auth card width minus EdgeInsets). The card itself is
        // pinned to AuthCardWidth in LoadView; the stack's EdgeInsets
        // (SpaceXl on each side) inset arranged subviews' layout area,
        // and these width constraints tie children to the inset
        // content rect via the same SpaceXl reference — single source
        // of truth, no double-bookkeeping.
        //
        // NSStackView has no built-in cross-axis "Fill" alignment, so
        // explicit width constraints are the canonical pattern here.
        var inset = -2 * (float)formStack.EdgeInsets.Left;
        NSLayoutConstraint.ActivateConstraints(new[]
        {
            form.WidthAnchor.ConstraintEqualTo(formStack.WidthAnchor, 1, inset),
            separator.WidthAnchor.ConstraintEqualTo(formStack.WidthAnchor, 1, inset),
            google.WidthAnchor.ConstraintEqualTo(formStack.WidthAnchor, 1, inset),
        });

        // Wrap in Liquid Glass (macOS 26 / Tahoe). NSGlassEffectView
        // is the new, proper way to put a UI element on glass — it
        // adapts to surrounding content, refracts light, and handles
        // legibility treatments AppKit-side. See WWDC 2025 session 310
        // ("Build an AppKit app with the new design"). The glass view
        // ties its geometry to ContentView automatically via Auto
        // Layout, so we don't need to anchor formStack inside it.
        var glass = new NSGlassEffectView
        {
            CornerRadius = ThemeMetrics.CardCornerRadius,
            TranslatesAutoresizingMaskIntoConstraints = false,
            ContentView = formStack,
        };

        return glass;
    }

    private NSView BuildHeader()
    {
        var header = new NSStackView
        {
            Orientation = NSUserInterfaceLayoutOrientation.Vertical,
            Alignment = NSLayoutAttribute.CenterX,
            Spacing = ThemeMetrics.SpaceSm,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };

        var brand = LabelField(text: "Pivox",
            font: ThemeFonts.NS(ThemeFont.BrandTitle),
            color: ThemeColors.NS(ThemeColor.Foreground));
        var subtitle = LabelField(text: "Sign in to your account",
            font: ThemeFonts.NS(ThemeFont.Body),
            color: ThemeColors.NS(ThemeColor.SecondaryForeground));

        header.AddArrangedSubview(brand);
        header.AddArrangedSubview(subtitle);
        return header;
    }

    private NSView BuildForm()
    {
        var form = new NSStackView
        {
            Orientation = NSUserInterfaceLayoutOrientation.Vertical,
            Alignment = NSLayoutAttribute.Leading,
            Spacing = ThemeMetrics.SpaceMd,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };

        _emailField = new NSTextField
        {
            PlaceholderString = "Email",
            ControlSize = NSControlSize.Large,
            Bezeled = true,
            BezelStyle = NSTextFieldBezelStyle.Rounded,
            Editable = true,
            Selectable = true,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };

        _passwordField = new NSSecureTextField
        {
            PlaceholderString = "Password",
            ControlSize = NSControlSize.Large,
            Bezeled = true,
            BezelStyle = NSTextFieldBezelStyle.Rounded,
            Editable = true,
            Selectable = true,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };

        _signInButton = new AuthPrimaryButton
        {
            PrimaryTitle = "Sign In",
            // Default button: Return/Enter in any field activates it.
            // AppKit also gives the default button the distinctive
            // pulsing-blue treatment when the window is key.
            KeyEquivalent = "\r",
        };

        // Pre-allocated error space: a single-line label that stays in
        // the layout even when empty, so showing/hiding the message
        // doesn't shift everything below.
        _errorLabel = LabelField(text: " ",
            font: ThemeFonts.NS(ThemeFont.Body),
            color: ThemeColors.NS(ThemeColor.Destructive));
        _errorLabel.Alignment = NSTextAlignment.Center;

        form.AddArrangedSubview(_emailField);
        form.AddArrangedSubview(_passwordField);
        form.AddArrangedSubview(_signInButton);
        form.AddArrangedSubview(_errorLabel);

        // Form children all stretch to the form's full width.
        NSLayoutConstraint.ActivateConstraints(new[]
        {
            _emailField.WidthAnchor.ConstraintEqualTo(form.WidthAnchor),
            _passwordField.WidthAnchor.ConstraintEqualTo(form.WidthAnchor),
            _signInButton.WidthAnchor.ConstraintEqualTo(form.WidthAnchor),
            _errorLabel.WidthAnchor.ConstraintEqualTo(form.WidthAnchor),
        });

        return form;
    }

    private NSView BuildSeparator()
    {
        var row = new NSStackView
        {
            Orientation = NSUserInterfaceLayoutOrientation.Horizontal,
            Alignment = NSLayoutAttribute.CenterY,
            Spacing = ThemeMetrics.SpaceSm,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };

        var left = HairlineRule();
        var or = LabelField(text: "or",
            font: ThemeFonts.NS(ThemeFont.BodySmall),
            color: ThemeColors.NS(ThemeColor.SecondaryForeground));
        var right = HairlineRule();

        row.AddArrangedSubview(left);
        row.AddArrangedSubview(or);
        row.AddArrangedSubview(right);

        // Both rules grow equally to fill the row.
        NSLayoutConstraint.ActivateConstraints(new[]
        {
            left.HeightAnchor.ConstraintEqualTo(ThemeMetrics.HairlineThickness),
            right.HeightAnchor.ConstraintEqualTo(ThemeMetrics.HairlineThickness),
            left.WidthAnchor.ConstraintEqualTo(right.WidthAnchor),
        });

        return row;
    }

    private NSView BuildGoogleButton()
    {
        _googleButton = new NSButton
        {
            Title = "Continue with Google",
            BezelStyle = NSBezelStyle.Push,
            ControlSize = NSControlSize.Large,
            ImagePosition = NSCellImagePosition.ImageLeft,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        // Official Google "G" logo. The asset (google-g.svg) lives in
        // Pivox.macOS/Assets.xcassets/GoogleLogo.imageset/, copied from
        // the SwiftUI Pivox app's asset catalog. SVG with
        // preserves-vector-representation so it renders crisp at any
        // button size.
        var icon = NSImage.ImageNamed("GoogleLogo");
        if (icon is not null) _googleButton.Image = icon;
        return _googleButton;
    }

    // ───── bindings ──────────────────────────────────────────────

    // Named handlers so Dispose can unsubscribe symmetrically. Stored
    // lambdas would close over `this` and `_vm`, making the
    // subscription a strong reference that keeps the VC alive until
    // the AppKit control's invocation list releases — fragile under
    // future capture changes. Methods don't have that hazard.
    private void WireBindings()
    {
        _emailField.Changed += OnEmailChanged;
        _passwordField.Changed += OnPasswordChanged;
        _signInButton.Activated += OnSignInActivated;
        _googleButton.Activated += OnGoogleActivated;
        _vm.PropertyChanged += OnViewModelChanged;
    }

    private void UnwireBindings()
    {
        _emailField.Changed -= OnEmailChanged;
        _passwordField.Changed -= OnPasswordChanged;
        _signInButton.Activated -= OnSignInActivated;
        _googleButton.Activated -= OnGoogleActivated;
        _vm.PropertyChanged -= OnViewModelChanged;
    }

    private void OnEmailChanged(object? sender, EventArgs e)
        => _vm.Email = _emailField.StringValue;

    private void OnPasswordChanged(object? sender, EventArgs e)
        => _vm.Password = _passwordField.StringValue;

    private async void OnSignInActivated(object? sender, EventArgs e)
        => await _vm.SignInWithEmailAsync();

    private async void OnGoogleActivated(object? sender, EventArgs e)
        => await _vm.SignInWithGoogleAsync();

    private void OnViewModelChanged(object? sender, PropertyChangedEventArgs e)
    {
        // Coarse-grained refresh — touched-property switch would micro-
        // optimize but the form has 4 controls and the work per refresh
        // is trivial. Keep it simple.
        ApplyState();
    }

    private void ApplyState()
    {
        // Mirror only the fields where the source-of-truth is the view-
        // model. The text fields' StringValue is owned by the user's
        // typing; we don't re-push it from the VM (would trigger
        // recursive Changed events and cursor flicker).
        var loading = _vm.IsLoading;
        _signInButton.IsLoading = loading;
        _signInButton.Enabled = _vm.CanSubmit;
        _googleButton.Enabled = !loading;
        _emailField.Enabled = !loading;
        _passwordField.Enabled = !loading;

        // Pre-allocated space: keep one non-empty char (" ") when no
        // error, so the label's height doesn't collapse and shift the
        // layout above when an error arrives.
        _errorLabel.StringValue = _vm.ErrorMessage ?? " ";
    }

    // ───── helpers ───────────────────────────────────────────────

    private static NSTextField LabelField(string text, NSFont font, NSColor color)
    {
        var label = NSTextField.CreateLabel(text);
        label.Font = font;
        label.TextColor = color;
        label.TranslatesAutoresizingMaskIntoConstraints = false;
        return label;
    }

    private static NSView HairlineRule()
    {
        var line = new NSView { TranslatesAutoresizingMaskIntoConstraints = false };
        line.WantsLayer = true;
        line.Layer!.BackgroundColor = ThemeColors.NS(ThemeColor.Border).CGColor;
        return line;
    }
}
