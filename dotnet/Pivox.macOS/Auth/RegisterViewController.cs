using System.ComponentModel;
using CoreGraphics;
using ObjCRuntime;
using Pivox.Shared.Auth;
using Pivox.Shared.Navigation;
using Pivox.Shared.UI;
using Pivox.UI;

namespace Pivox.Auth;

/// <summary>
/// Sign-up form. Mirrors <see cref="LoginViewController"/>'s shape
/// (glass card on solid window background) with the four-field form
/// SwiftUI <c>RegisterView</c> uses: Email, Display name, Password,
/// Confirm password.
///
/// Footer "Already have an account? Sign in" pops the router back to
/// Login.
/// </summary>
[Register("RegisterViewController")]
public sealed class RegisterViewController : NSViewController
{
    private readonly RegisterViewModel _vm;
    private readonly AppRouter _router;

    private NSTextField _emailField = null!;
    private NSTextField _displayNameField = null!;
    private NSSecureTextField _passwordField = null!;
    private NSSecureTextField _confirmPasswordField = null!;
    private AuthPrimaryButton _createAccountButton = null!;
    private NSButton _googleButton = null!;
    private NSButton _githubButton = null!;
    private NSButton _signInLinkButton = null!;
    private NSTextField _errorLabel = null!;

    public RegisterViewController(RegisterViewModel vm, AppRouter router)
        : base((string?)null, null)
    {
        _vm = vm ?? throw new ArgumentNullException(nameof(vm));
        _router = router ?? throw new ArgumentNullException(nameof(router));
    }

    public override void LoadView()
    {
        var container = new NSView();

        var backdrop = new RadialBackdropView
        {
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        container.AddSubview(backdrop);

        var card = BuildCard();
        container.AddSubview(card);

        NSLayoutConstraint.ActivateConstraints(new[]
        {
            backdrop.TopAnchor.ConstraintEqualTo(container.TopAnchor),
            backdrop.BottomAnchor.ConstraintEqualTo(container.BottomAnchor),
            backdrop.LeadingAnchor.ConstraintEqualTo(container.LeadingAnchor),
            backdrop.TrailingAnchor.ConstraintEqualTo(container.TrailingAnchor),

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
        ApplyState();
    }

    public override void ViewDidAppear()
    {
        base.ViewDidAppear();
        View.Window?.MakeFirstResponder(_emailField);
    }

    protected override void Dispose(bool disposing)
    {
        if (disposing) UnwireBindings();
        base.Dispose(disposing);
    }

    // ───── view tree ─────────────────────────────────────────────

    private NSView BuildCard()
    {
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
        var social = BuildSocialButtons();
        var footer = BuildFooter();

        formStack.AddArrangedSubview(header);
        formStack.AddArrangedSubview(form);
        formStack.AddArrangedSubview(separator);
        formStack.AddArrangedSubview(social);
        formStack.AddArrangedSubview(footer);

        var inset = -2 * (float)formStack.EdgeInsets.Left;
        NSLayoutConstraint.ActivateConstraints(new[]
        {
            form.WidthAnchor.ConstraintEqualTo(formStack.WidthAnchor, 1, inset),
            separator.WidthAnchor.ConstraintEqualTo(formStack.WidthAnchor, 1, inset),
            social.WidthAnchor.ConstraintEqualTo(formStack.WidthAnchor, 1, inset),
        });

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

        var brand = LabelField("Pivox",
            ThemeFonts.NS(ThemeFont.BrandTitle),
            ThemeColors.NS(ThemeColor.Foreground));
        var subtitle = LabelField("Create your account",
            ThemeFonts.NS(ThemeFont.Body),
            ThemeColors.NS(ThemeColor.SecondaryForeground));

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

        _emailField = MakeTextField("Email");
        _displayNameField = MakeTextField("Display name");
        _passwordField = MakeSecureField("Password");
        _confirmPasswordField = MakeSecureField("Confirm password");

        _createAccountButton = new AuthPrimaryButton
        {
            PrimaryTitle = "Create Account",
            // Default button — Return/Enter activates from any field.
            KeyEquivalent = "\r",
        };

        _errorLabel = LabelField(" ",
            ThemeFonts.NS(ThemeFont.Body),
            ThemeColors.NS(ThemeColor.Destructive));
        _errorLabel.Alignment = NSTextAlignment.Center;

        form.AddArrangedSubview(_emailField);
        form.AddArrangedSubview(_displayNameField);
        form.AddArrangedSubview(_passwordField);
        form.AddArrangedSubview(_confirmPasswordField);
        form.AddArrangedSubview(_createAccountButton);
        form.AddArrangedSubview(_errorLabel);

        NSLayoutConstraint.ActivateConstraints(new[]
        {
            _emailField.WidthAnchor.ConstraintEqualTo(form.WidthAnchor),
            _displayNameField.WidthAnchor.ConstraintEqualTo(form.WidthAnchor),
            _passwordField.WidthAnchor.ConstraintEqualTo(form.WidthAnchor),
            _confirmPasswordField.WidthAnchor.ConstraintEqualTo(form.WidthAnchor),
            _createAccountButton.WidthAnchor.ConstraintEqualTo(form.WidthAnchor),
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
        var or = LabelField("or",
            ThemeFonts.NS(ThemeFont.BodySmall),
            ThemeColors.NS(ThemeColor.SecondaryForeground));
        var right = HairlineRule();
        row.AddArrangedSubview(left);
        row.AddArrangedSubview(or);
        row.AddArrangedSubview(right);
        NSLayoutConstraint.ActivateConstraints(new[]
        {
            left.HeightAnchor.ConstraintEqualTo(ThemeMetrics.HairlineThickness),
            right.HeightAnchor.ConstraintEqualTo(ThemeMetrics.HairlineThickness),
            left.WidthAnchor.ConstraintEqualTo(right.WidthAnchor),
        });
        return row;
    }

    private NSView BuildSocialButtons()
    {
        var stack = new NSStackView
        {
            Orientation = NSUserInterfaceLayoutOrientation.Vertical,
            Alignment = NSLayoutAttribute.Leading,
            Spacing = ThemeMetrics.SpaceSm,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };

        _googleButton = MakeSocialButton("Continue with Google", "GoogleLogo");
        _githubButton = MakeSocialButton("Continue with GitHub", "GitHubLogo");

        stack.AddArrangedSubview(_googleButton);
        stack.AddArrangedSubview(_githubButton);
        NSLayoutConstraint.ActivateConstraints(new[]
        {
            _googleButton.WidthAnchor.ConstraintEqualTo(stack.WidthAnchor),
            _githubButton.WidthAnchor.ConstraintEqualTo(stack.WidthAnchor),
        });
        return stack;
    }

    private NSView BuildFooter()
    {
        var row = new NSStackView
        {
            Orientation = NSUserInterfaceLayoutOrientation.Horizontal,
            Alignment = NSLayoutAttribute.CenterY,
            Spacing = ThemeMetrics.SpaceXs,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };

        var prompt = LabelField("Already have an account?",
            ThemeFonts.NS(ThemeFont.Body),
            ThemeColors.NS(ThemeColor.SecondaryForeground));

        _signInLinkButton = NSButton.CreateButton("Sign in", () => _router.Pop());
        _signInLinkButton.BezelStyle = NSBezelStyle.Inline;
        _signInLinkButton.SetButtonType(NSButtonType.MomentaryChange);
        _signInLinkButton.Bordered = false;
        _signInLinkButton.Font = ThemeFonts.NS(ThemeFont.Body);
        _signInLinkButton.ContentTintColor = ThemeColors.NS(ThemeColor.Accent);
        _signInLinkButton.TranslatesAutoresizingMaskIntoConstraints = false;

        row.AddArrangedSubview(prompt);
        row.AddArrangedSubview(_signInLinkButton);
        return row;
    }

    // ───── bindings ──────────────────────────────────────────────

    private void WireBindings()
    {
        _emailField.Changed += OnEmailChanged;
        _displayNameField.Changed += OnDisplayNameChanged;
        _passwordField.Changed += OnPasswordChanged;
        _confirmPasswordField.Changed += OnConfirmPasswordChanged;
        _createAccountButton.Activated += OnCreateAccountActivated;
        _googleButton.Activated += OnGoogleActivated;
        _githubButton.Activated += OnGitHubActivated;
        _vm.PropertyChanged += OnViewModelChanged;
    }

    private void UnwireBindings()
    {
        _emailField.Changed -= OnEmailChanged;
        _displayNameField.Changed -= OnDisplayNameChanged;
        _passwordField.Changed -= OnPasswordChanged;
        _confirmPasswordField.Changed -= OnConfirmPasswordChanged;
        _createAccountButton.Activated -= OnCreateAccountActivated;
        _googleButton.Activated -= OnGoogleActivated;
        _githubButton.Activated -= OnGitHubActivated;
        _vm.PropertyChanged -= OnViewModelChanged;
    }

    private void OnEmailChanged(object? s, EventArgs e) => _vm.Email = _emailField.StringValue;
    private void OnDisplayNameChanged(object? s, EventArgs e) => _vm.DisplayName = _displayNameField.StringValue;
    private void OnPasswordChanged(object? s, EventArgs e) => _vm.Password = _passwordField.StringValue;
    private void OnConfirmPasswordChanged(object? s, EventArgs e) => _vm.ConfirmPassword = _confirmPasswordField.StringValue;

    private async void OnCreateAccountActivated(object? s, EventArgs e)
        => await _vm.CreateAccountAsync();

    private async void OnGoogleActivated(object? s, EventArgs e)
        => await _vm.SignInWithGoogleAsync();

    private async void OnGitHubActivated(object? s, EventArgs e)
        => await _vm.SignInWithGitHubAsync();

    private void OnViewModelChanged(object? sender, PropertyChangedEventArgs e) => ApplyState();

    private void ApplyState()
    {
        var loading = _vm.IsLoading;
        _createAccountButton.IsLoading = loading;
        _createAccountButton.Enabled = _vm.CanSubmit;
        _googleButton.Enabled = !loading;
        _githubButton.Enabled = !loading;
        _emailField.Enabled = !loading;
        _displayNameField.Enabled = !loading;
        _passwordField.Enabled = !loading;
        _confirmPasswordField.Enabled = !loading;
        _signInLinkButton.Enabled = !loading;
        _errorLabel.StringValue = _vm.ErrorMessage ?? " ";
    }

    // ───── helpers ───────────────────────────────────────────────

    private static NSTextField MakeTextField(string placeholder) => new NSTextField
    {
        PlaceholderString = placeholder,
        ControlSize = NSControlSize.Large,
        Bezeled = true,
        BezelStyle = NSTextFieldBezelStyle.Rounded,
        Editable = true,
        Selectable = true,
        TranslatesAutoresizingMaskIntoConstraints = false,
    };

    private static NSSecureTextField MakeSecureField(string placeholder) => new NSSecureTextField
    {
        PlaceholderString = placeholder,
        ControlSize = NSControlSize.Large,
        Bezeled = true,
        BezelStyle = NSTextFieldBezelStyle.Rounded,
        Editable = true,
        Selectable = true,
        TranslatesAutoresizingMaskIntoConstraints = false,
    };

    private static NSButton MakeSocialButton(string title, string imageName)
    {
        var button = new NSButton
        {
            Title = title,
            BezelStyle = NSBezelStyle.Push,
            ControlSize = NSControlSize.Large,
            ImagePosition = NSCellImagePosition.ImageLeft,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        var icon = NSImage.ImageNamed(imageName);
        if (icon is not null)
        {
            // Pin SVG-backed NSImages to 16×16 (mirrors SwiftUI's
            // .frame on the social icons). GitHubLogo's 1024×1024
            // intrinsic size would otherwise blow up the button.
            icon.Size = new CGSize(16, 16);
            button.Image = icon;
        }
        return button;
    }

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
