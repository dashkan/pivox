using AppKit;
using CoreGraphics;
using Foundation;
using ObjCRuntime;
using Pivox.Shared.Auth;

namespace PivoxApp;

/// <summary>
/// UI consumer of <see cref="IAuthService"/>. Knows nothing about
/// Firebase, ASWebAuthenticationSession, or PKCE — those live in the
/// macOS-side service implementation. This view just wires buttons
/// to service calls and renders the returned <see cref="AuthSession"/>.
/// </summary>
[Register("DetailViewController")]
public sealed class DetailViewController : NSViewController
{
    private readonly IAuthService _auth;
    private NSTextField _email = null!;
    private NSSecureTextField _password = null!;
    private NSButton _signIn = null!;
    private NSButton _googleSignIn = null!;
    private NSTextField _status = null!;

    public DetailViewController(IAuthService auth) : base((string?)null, null)
    {
        _auth = auth;
        _auth.CurrentChanged += OnAuthChanged;
    }

    public override void LoadView()
    {
        View = new NSView(new CGRect(0, 0, 600, 400));
    }

    public override void ViewDidLoad()
    {
        base.ViewDidLoad();

        var title = Label("Auth — Cross-Platform Service Test", bold: true, size: 18);
        var subtitle = Label(
            "Calls IAuthService (Pivox.Shared) — no Firebase types in this view.",
            secondary: true);

        _email = new NSTextField
        {
            PlaceholderString = "Email",
            BezelStyle = NSTextFieldBezelStyle.Rounded,
            Bezeled = true,
            Editable = true,
            Selectable = true,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };

        _password = new NSSecureTextField
        {
            PlaceholderString = "Password",
            BezelStyle = NSTextFieldBezelStyle.Rounded,
            Bezeled = true,
            Editable = true,
            Selectable = true,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };

        _signIn = new NSButton
        {
            Title = "Sign In",
            BezelStyle = NSBezelStyle.Rounded,
            ControlSize = NSControlSize.Large,
            KeyEquivalent = "\r",
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        _signIn.Activated += (_, _) => _ = SignInEmailAsync();

        _googleSignIn = new NSButton
        {
            Title = "Continue with Google",
            BezelStyle = NSBezelStyle.Rounded,
            ControlSize = NSControlSize.Large,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        _googleSignIn.Activated += (_, _) => _ = SignInGoogleAsync();

        _status = Label(" ", size: 12);
        _status.LineBreakMode = NSLineBreakMode.ByWordWrapping;
        _status.PreferredMaxLayoutWidth = 360;

        var stack = new NSStackView
        {
            Orientation = NSUserInterfaceLayoutOrientation.Vertical,
            Spacing = 12,
            Alignment = NSLayoutAttribute.CenterX,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        stack.AddArrangedSubview(title);
        stack.AddArrangedSubview(subtitle);
        stack.AddArrangedSubview(_email);
        stack.AddArrangedSubview(_password);
        stack.AddArrangedSubview(_signIn);
        stack.AddArrangedSubview(_googleSignIn);
        stack.AddArrangedSubview(_status);

        View.AddSubview(stack);

        NSLayoutConstraint.ActivateConstraints(new[]
        {
            stack.CenterXAnchor.ConstraintEqualTo(View.CenterXAnchor),
            stack.CenterYAnchor.ConstraintEqualTo(View.CenterYAnchor),
            stack.WidthAnchor.ConstraintEqualTo(400f),
            _email.WidthAnchor.ConstraintEqualTo(stack.WidthAnchor),
            _password.WidthAnchor.ConstraintEqualTo(stack.WidthAnchor),
            _signIn.WidthAnchor.ConstraintEqualTo(stack.WidthAnchor),
            _googleSignIn.WidthAnchor.ConstraintEqualTo(stack.WidthAnchor),
            _status.WidthAnchor.ConstraintEqualTo(stack.WidthAnchor),
        });
    }

    private async Task SignInEmailAsync()
    {
        var email = _email.StringValue.Trim();
        var password = _password.StringValue;
        if (string.IsNullOrEmpty(email)) { Status("Email is required.", isError: true); return; }
        if (string.IsNullOrEmpty(password)) { Status("Password is required.", isError: true); return; }

        SetLoading(true);
        Status("Signing in...");
        try
        {
            var session = await _auth.SignInWithEmailAsync(email, password);
            RenderSession(session);
        }
        catch (Exception ex)
        {
            Status($"❌ {ex.Message}", isError: true);
        }
        finally
        {
            SetLoading(false);
        }
    }

    private async Task SignInGoogleAsync()
    {
        SetLoading(true);
        Status("Opening Google sign-in...");
        try
        {
            var session = await _auth.SignInWithGoogleAsync();
            RenderSession(session);
        }
        catch (Exception ex)
        {
            Status($"❌ {ex.Message}", isError: true);
        }
        finally
        {
            SetLoading(false);
        }
    }

    private void OnAuthChanged(object? sender, AuthSession? session)
    {
        // Sign-out from elsewhere (or any other state change Firebase
        // notices internally) reflects here. Doesn't need to be on the
        // main thread for this minimal view; if we end up doing real
        // UI updates from here, dispatch with NSRunLoop.Main.
    }

    private void RenderSession(AuthSession session)
    {
        var preview = session.IdToken.Length > 40
            ? session.IdToken[..40] + "..."
            : session.IdToken;
        Status(
            $"✅ Signed in.\n" +
            $"pivoxUserId={session.PivoxUserId}\n" +
            $"email={session.Email}\n" +
            $"token={preview}\n" +
            $"expires={session.ExpiresAt:HH:mm:ss}");
    }

    private void SetLoading(bool loading)
    {
        _signIn.Enabled = !loading;
        _signIn.Title = loading ? "Signing in..." : "Sign In";
        _email.Enabled = !loading;
        _password.Enabled = !loading;
        _googleSignIn.Enabled = !loading;
    }

    private void Status(string message, bool isError = false)
    {
        _status.StringValue = message;
        _status.TextColor = isError ? NSColor.SystemRed : NSColor.Label;
    }

    private static NSTextField Label(
        string text, bool bold = false, double size = 13, bool secondary = false) => new()
    {
        StringValue = text,
        Editable = false,
        Bordered = false,
        Bezeled = false,
        DrawsBackground = false,
        Selectable = false,
        Alignment = NSTextAlignment.Center,
        Font = bold
            ? NSFont.SystemFontOfSize((nfloat)size, NSFontWeight.Bold)
            : NSFont.SystemFontOfSize((nfloat)size),
        TextColor = secondary ? NSColor.SecondaryLabel : NSColor.Label,
        TranslatesAutoresizingMaskIntoConstraints = false,
    };
}
