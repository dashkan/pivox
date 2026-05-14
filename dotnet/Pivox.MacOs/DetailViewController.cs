using AppKit;
using CoreGraphics;
using Foundation;
using ObjCRuntime;
using Pivox.Api.V1;
using Pivox.Client;
using Pivox.Shared.Auth;

namespace Pivox;

/// <summary>
/// Test harness for the cross-platform auth + gRPC stack:
///
///   - Sign in (email/password or Google) via <see cref="IAuthService"/>.
///   - Restore persisted Firebase session on launch (via FIRAuth's
///     Keychain persistence + the AddAuthStateDidChangeListener in
///     MacOsAuthService).
///   - Call pivox-cloud's Organizations.ListOrganizations via the
///     gRPC <see cref="PivoxClient"/> with the Bearer token attached
///     by AuthInterceptor.
///
/// Knows nothing about Firebase or platform-specific auth internals.
/// </summary>
[Register("DetailViewController")]
public sealed class DetailViewController : NSViewController
{
    private readonly IAuthService _auth;
    private readonly PivoxClient _pivox;
    private NSTextField _email = null!;
    private NSSecureTextField _password = null!;
    private NSButton _signIn = null!;
    private NSButton _googleSignIn = null!;
    private NSButton _signOut = null!;
    private NSButton _listOrgs = null!;
    private NSTextField _status = null!;

    public DetailViewController(IAuthService auth, PivoxClient pivox)
        : base((string?)null, null)
    {
        _auth = auth;
        _pivox = pivox;
        _auth.CurrentChanged += OnAuthChanged;
    }

    public override void LoadView()
    {
        View = new NSView(new CGRect(0, 0, 600, 460));
    }

    public override void ViewDidLoad()
    {
        base.ViewDidLoad();

        var title = Label("Pivox — .NET cross-platform spike", bold: true, size: 18);
        var subtitle = Label(
            "Auth via IAuthService, gRPC via PivoxClient, both in Pivox.Shared/Pivox.Client.",
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

        _signOut = new NSButton
        {
            Title = "Sign Out",
            BezelStyle = NSBezelStyle.Rounded,
            ControlSize = NSControlSize.Regular,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        _signOut.Activated += (_, _) => _ = SignOutAsync();

        _listOrgs = new NSButton
        {
            Title = "Call pivox-cloud → ListOrganizations",
            BezelStyle = NSBezelStyle.Rounded,
            ControlSize = NSControlSize.Regular,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        _listOrgs.Activated += (_, _) => _ = ListOrgsAsync();

        _status = Label(" ", size: 12);
        _status.LineBreakMode = NSLineBreakMode.ByWordWrapping;
        _status.PreferredMaxLayoutWidth = 360;

        var stack = new NSStackView
        {
            Orientation = NSUserInterfaceLayoutOrientation.Vertical,
            Spacing = 10,
            Alignment = NSLayoutAttribute.CenterX,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        stack.AddArrangedSubview(title);
        stack.AddArrangedSubview(subtitle);
        stack.AddArrangedSubview(_email);
        stack.AddArrangedSubview(_password);
        stack.AddArrangedSubview(_signIn);
        stack.AddArrangedSubview(_googleSignIn);
        stack.AddArrangedSubview(_listOrgs);
        stack.AddArrangedSubview(_signOut);
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
            _listOrgs.WidthAnchor.ConstraintEqualTo(stack.WidthAnchor),
            _signOut.WidthAnchor.ConstraintEqualTo(stack.WidthAnchor),
            _status.WidthAnchor.ConstraintEqualTo(stack.WidthAnchor),
        });

        // If Firebase already restored a persisted session in the
        // background (state listener may have fired before this VC was
        // constructed), reflect it now. After this, future state
        // changes flow through OnAuthChanged.
        RenderAuthState(_auth.Current);
    }

    // ───── auth ──────────────────────────────────────────────────

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
            await _auth.SignInWithEmailAsync(email, password);
            // OnAuthChanged renders the result.
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
            await _auth.SignInWithGoogleAsync();
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

    private async Task SignOutAsync()
    {
        try
        {
            await _auth.SignOutAsync();
            Status("Signed out.");
        }
        catch (Exception ex)
        {
            Status($"❌ Sign out failed: {ex.Message}", isError: true);
        }
    }

    private void OnAuthChanged(object? sender, AuthSession? session)
    {
        // FIRAuth fires the listener on whatever thread Firebase chose
        // internally; AppKit demands main-thread for any view mutation.
        NSApplication.SharedApplication.InvokeOnMainThread(() => RenderAuthState(session));
    }

    private void RenderAuthState(AuthSession? session)
    {
        if (session is null)
        {
            Status(" ");
            return;
        }

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

    // ───── gRPC test ─────────────────────────────────────────────

    private async Task ListOrgsAsync()
    {
        if (_auth.Current is null)
        {
            Status("Sign in first.", isError: true);
            return;
        }

        Status("Calling pivox-cloud → Organizations.ListOrganizations...");
        try
        {
            var response = await _pivox.Organizations
                .ListOrganizationsAsync(new ListOrganizationsRequest());

            var lines = response.Organizations
                .Select(o => $"  • {o.Name}  ({o.DisplayName})")
                .DefaultIfEmpty("  (no organizations)")
                .ToArray();
            Status(
                $"✅ ListOrganizations returned {response.Organizations.Count} org(s):\n" +
                string.Join("\n", lines));
        }
        catch (Grpc.Core.RpcException ex)
        {
            Console.Error.WriteLine($"[gRPC] RpcException: status={ex.StatusCode} detail={ex.Status.Detail}");
            Status($"❌ gRPC {ex.StatusCode}: {ex.Status.Detail}", isError: true);
        }
        catch (Exception ex)
        {
            Console.Error.WriteLine($"[gRPC] {ex.GetType().Name}: {ex.Message}");
            Status($"❌ {ex.Message}", isError: true);
        }
    }

    // ───── helpers ───────────────────────────────────────────────

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
        // NSFont.SystemFontOfSize is bound as nullable but Apple's
        // contract guarantees non-null. Forgive at the boundary.
        Font = (bold
            ? NSFont.SystemFontOfSize((nfloat)size, NSFontWeight.Bold)
            : NSFont.SystemFontOfSize((nfloat)size))!,
        TextColor = secondary ? NSColor.SecondaryLabel : NSColor.Label,
        TranslatesAutoresizingMaskIntoConstraints = false,
    };
}
