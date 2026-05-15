using System.ComponentModel;
using CoreGraphics;
using ObjCRuntime;
using Pivox.Shared.Auth;
using Pivox.Shared.Navigation;
using Pivox.Shared.UI;
using Pivox.UI;

namespace Pivox.Auth;

/// <summary>
/// Sign-in form. Drives the SwiftUI <c>LoginView</c> shape:
///
/// <list type="bullet">
/// <item>Step 1 — email only. Primary button reads "Continue".
///   Submit resolves SSO vs password via
///   <see cref="LoginViewModel.SubmitEmailStepAsync"/>.</item>
/// <item>Step 2 — password field revealed. Primary button reads
///   "Sign In". Submit calls
///   <see cref="LoginViewModel.SignInWithEmailAsync"/>.</item>
/// </list>
///
/// Also exposes:
/// <list type="bullet">
/// <item>Remember-me checkbox (persists email across launches via
///   <see cref="RememberedEmail"/>).</item>
/// <item>Forgot-password link (step 2 only).</item>
/// <item>Continue-with-Google + Continue-with-GitHub social
///   buttons.</item>
/// <item>Footer "Don't have an account? Create one" — pushes the
///   Register route onto the <see cref="AppRouter"/>.</item>
/// </list>
///
/// Bindings: text-field <c>Changed</c> events push into the view-model;
/// view-model <see cref="INotifyPropertyChanged.PropertyChanged"/>
/// triggers <see cref="ApplyState"/>, a coarse refresh that touches
/// every field. Subscribe in <see cref="ViewDidLoad"/>, unsubscribe in
/// <see cref="Dispose"/>.
/// </summary>
[Register("LoginViewController")]
public sealed class LoginViewController : NSViewController
{
    private readonly LoginViewModel _vm;
    private readonly AppRouter _router;
    private readonly RememberedEmail _rememberedEmail;

    private NSTextField _emailField = null!;
    private NSSecureTextField _passwordField = null!;
    // Tracks the previous DidResolveAsPassword value across ApplyState
    // calls so we can detect the false→true transition and focus the
    // password field exactly once — without stealing focus on every
    // refresh while in step 2.
    private bool _previousDidResolveAsPassword;
    private NSButton _rememberMeCheckbox = null!;
    private NSButton _forgotPasswordButton = null!;
    private NSView _optionsRow = null!;
    private AuthPrimaryButton _primaryButton = null!;
    private NSButton _googleButton = null!;
    private NSButton _githubButton = null!;
    private NSButton _createOneLinkButton = null!;
    private NSTextField _errorLabel = null!;

    public LoginViewController(
        LoginViewModel vm, AppRouter router, RememberedEmail rememberedEmail)
        : base((string?)null, null)
    {
        _vm = vm ?? throw new ArgumentNullException(nameof(vm));
        _router = router ?? throw new ArgumentNullException(nameof(router));
        _rememberedEmail = rememberedEmail
            ?? throw new ArgumentNullException(nameof(rememberedEmail));
    }

    public override void LoadView()
    {
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

        // Seed the email field + VM from persisted remember-me state.
        // If non-empty, also pre-check the remember-me box so the
        // user's preference round-trips.
        var remembered = _rememberedEmail.Get();
        if (!string.IsNullOrEmpty(remembered))
        {
            _emailField.StringValue = remembered;
            _vm.Email = remembered;
            _rememberMeCheckbox.State = NSCellStateValue.On;
        }

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
        var subtitle = LabelField("Sign in to your account",
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
            // Hidden until step 1's resolver confirms password auth.
            // NSStackView collapses around hidden arranged subviews,
            // so the layout closes up cleanly.
            Hidden = true,
        };

        _optionsRow = BuildOptionsRow();

        _primaryButton = new AuthPrimaryButton
        {
            PrimaryTitle = _vm.PrimaryButtonTitle,
            KeyEquivalent = "\r",
        };

        _errorLabel = LabelField(" ",
            ThemeFonts.NS(ThemeFont.Body),
            ThemeColors.NS(ThemeColor.Destructive));
        _errorLabel.Alignment = NSTextAlignment.Center;

        form.AddArrangedSubview(_emailField);
        form.AddArrangedSubview(_passwordField);
        form.AddArrangedSubview(_optionsRow);
        form.AddArrangedSubview(_primaryButton);
        form.AddArrangedSubview(_errorLabel);

        NSLayoutConstraint.ActivateConstraints(new[]
        {
            _emailField.WidthAnchor.ConstraintEqualTo(form.WidthAnchor),
            _passwordField.WidthAnchor.ConstraintEqualTo(form.WidthAnchor),
            _optionsRow.WidthAnchor.ConstraintEqualTo(form.WidthAnchor),
            _primaryButton.WidthAnchor.ConstraintEqualTo(form.WidthAnchor),
            _errorLabel.WidthAnchor.ConstraintEqualTo(form.WidthAnchor),
        });

        return form;
    }

    private NSView BuildOptionsRow()
    {
        // Remember-me on the left, Forgot-password link on the right
        // (right side hidden in step 1, revealed alongside the password
        // field in step 2).
        var row = new NSStackView
        {
            Orientation = NSUserInterfaceLayoutOrientation.Horizontal,
            Alignment = NSLayoutAttribute.CenterY,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };

        _rememberMeCheckbox = new NSButton
        {
            Title = "Remember me",
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        _rememberMeCheckbox.SetButtonType(NSButtonType.Switch);
        _rememberMeCheckbox.Font = ThemeFonts.NS(ThemeFont.Body);

        _forgotPasswordButton = NSButton.CreateButton(
            "Forgot password?", () => { /* wired in WireBindings */ });
        _forgotPasswordButton.BezelStyle = NSBezelStyle.Inline;
        _forgotPasswordButton.Bordered = false;
        _forgotPasswordButton.Font = ThemeFonts.NS(ThemeFont.Body);
        _forgotPasswordButton.ContentTintColor = ThemeColors.NS(ThemeColor.Accent);
        _forgotPasswordButton.TranslatesAutoresizingMaskIntoConstraints = false;
        _forgotPasswordButton.Hidden = true;

        // Stack-gravity API: leading-anchored vs trailing-anchored. The
        // visual gap between the two grows/shrinks with the row width.
        row.AddView(_rememberMeCheckbox, NSStackViewGravity.Leading);
        row.AddView(_forgotPasswordButton, NSStackViewGravity.Trailing);

        return row;
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

        var prompt = LabelField("Don't have an account?",
            ThemeFonts.NS(ThemeFont.Body),
            ThemeColors.NS(ThemeColor.SecondaryForeground));

        _createOneLinkButton = NSButton.CreateButton(
            "Create one", () => _router.Push(new AppRoute.Register()));
        _createOneLinkButton.BezelStyle = NSBezelStyle.Inline;
        _createOneLinkButton.Bordered = false;
        _createOneLinkButton.Font = ThemeFonts.NS(ThemeFont.Body);
        _createOneLinkButton.ContentTintColor = ThemeColors.NS(ThemeColor.Accent);
        _createOneLinkButton.TranslatesAutoresizingMaskIntoConstraints = false;

        row.AddArrangedSubview(prompt);
        row.AddArrangedSubview(_createOneLinkButton);
        return row;
    }

    // ───── bindings ──────────────────────────────────────────────

    private void WireBindings()
    {
        _emailField.Changed += OnEmailChanged;
        _passwordField.Changed += OnPasswordChanged;
        _primaryButton.Activated += OnPrimaryActivated;
        _googleButton.Activated += OnGoogleActivated;
        _githubButton.Activated += OnGitHubActivated;
        _forgotPasswordButton.Activated += OnForgotPasswordActivated;
        _vm.PropertyChanged += OnViewModelChanged;
    }

    private void UnwireBindings()
    {
        _emailField.Changed -= OnEmailChanged;
        _passwordField.Changed -= OnPasswordChanged;
        _primaryButton.Activated -= OnPrimaryActivated;
        _googleButton.Activated -= OnGoogleActivated;
        _githubButton.Activated -= OnGitHubActivated;
        _forgotPasswordButton.Activated -= OnForgotPasswordActivated;
        _vm.PropertyChanged -= OnViewModelChanged;
    }

    private void OnEmailChanged(object? s, EventArgs e)
        => _vm.Email = _emailField.StringValue;

    private void OnPasswordChanged(object? s, EventArgs e)
        => _vm.Password = _passwordField.StringValue;

    private async void OnPrimaryActivated(object? s, EventArgs e)
    {
        // Dispatch: step 1 (resolve SSO vs password) vs step 2
        // (password sign-in). The shared VM knows the state.
        bool success;
        if (_vm.DidResolveAsPassword)
        {
            success = await _vm.SignInWithEmailAsync();
        }
        else
        {
            success = await _vm.SubmitEmailStepAsync();
            // success=true means SSO flow signed in inline — auth
            // listener will route to Shell. success=false either
            // means "no SSO, password field revealed" (continue
            // in step 2) or "request failed, error displayed".
        }

        if (success)
        {
            // Persist or clear the remembered email based on the
            // checkbox. Mirrors the SwiftUI conditional save.
            PersistRememberedEmail();
        }
    }

    private async void OnGoogleActivated(object? s, EventArgs e)
    {
        if (await _vm.SignInWithGoogleAsync()) PersistRememberedEmail();
    }

    private async void OnGitHubActivated(object? s, EventArgs e)
    {
        if (await _vm.SignInWithGitHubAsync()) PersistRememberedEmail();
    }

    private async void OnForgotPasswordActivated(object? s, EventArgs e)
    {
        // Capture the email the reset went to, BEFORE awaiting — the
        // user might edit the email field between clicking and the
        // alert appearing (the await is non-zero, slow networks more
        // so), and the alert should reference what was actually sent.
        var requestedEmail = _vm.Email;
        var sent = await _vm.SendPasswordResetAsync();
        if (sent)
        {
            var alert = new NSAlert
            {
                MessageText = "Check your email",
                InformativeText =
                    $"If an account exists for {requestedEmail}, you'll receive a "
                    + "password reset link shortly.",
                AlertStyle = NSAlertStyle.Informational,
            };
            alert.AddButton("OK");
            alert.RunModal();
        }
    }

    private void OnViewModelChanged(object? sender, PropertyChangedEventArgs e)
        => ApplyState();

    private void ApplyState()
    {
        var loading = _vm.IsLoading;
        var revealed = _vm.DidResolveAsPassword;

        _primaryButton.IsLoading = loading;
        _primaryButton.PrimaryTitle = _vm.PrimaryButtonTitle;
        _primaryButton.Enabled = _vm.CanSubmit;

        _passwordField.Hidden = !revealed;
        _forgotPasswordButton.Hidden = !revealed;
        // Reflect VM-cleared password (edit-email-after-step-2 resets it).
        if (!revealed && _passwordField.StringValue != _vm.Password)
        {
            _passwordField.StringValue = _vm.Password;
        }
        // Focus the password field exactly once when step 1 resolves
        // as password (false→true transition). Don't refocus on every
        // ApplyState while in step 2 — that would steal focus from
        // the user mid-edit.
        if (revealed && !_previousDidResolveAsPassword)
        {
            View.Window?.MakeFirstResponder(_passwordField);
        }
        _previousDidResolveAsPassword = revealed;

        _googleButton.Enabled = !loading;
        _githubButton.Enabled = !loading;
        _emailField.Enabled = !loading;
        _passwordField.Enabled = !loading;
        _rememberMeCheckbox.Enabled = !loading;
        _forgotPasswordButton.Enabled = !loading;
        _createOneLinkButton.Enabled = !loading;

        _errorLabel.StringValue = _vm.ErrorMessage ?? " ";
    }

    private void PersistRememberedEmail()
    {
        // Capture the checkbox state at the moment of successful
        // sign-in (mirrors SwiftUI's
        // `appState.save(rememberMe ? email : "", ...)` shape).
        if (_rememberMeCheckbox.State == NSCellStateValue.On)
        {
            _rememberedEmail.Set(_vm.Email.Trim());
        }
        else
        {
            _rememberedEmail.Set(null);
        }
    }

    // ───── helpers ───────────────────────────────────────────────

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
            // SVG-backed NSImages render at their intrinsic viewBox
            // size unless clamped. GoogleLogo is 24×24 so it happens
            // to look fine; GitHubLogo is 1024×1024 and would
            // explode the button. Pin to 16×16 — mirrors SwiftUI's
            // .frame(width: 16, height: 16) on the social icons.
            icon.Size = new CoreGraphics.CGSize(16, 16);
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
