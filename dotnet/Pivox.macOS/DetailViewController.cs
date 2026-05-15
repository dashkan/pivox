using AppKit;
using CoreGraphics;
using Foundation;
using ObjCRuntime;
using Pivox.Api.V1;
using Pivox.Client;
using Pivox.Shared.Auth;
using Pivox.Shared.UI;
using Pivox.UI;

namespace Pivox;

/// <summary>
/// Post-auth detail pane. Currently a thin placeholder + a ListOrgs
/// button as a smoke test that the signed-in session can talk to
/// pivox-cloud. Real content (dashboards, asset library, chat) lands
/// here as features ship.
///
/// Threading: `IAuthService.CurrentChanged` is observed at the
/// AppDelegate level (it drives window-swap routing). This VC is only
/// constructed when signed in, so it doesn't need to subscribe — the
/// session is guaranteed live for its lifetime.
/// </summary>
[Register("DetailViewController")]
public sealed class DetailViewController : NSViewController
{
    private readonly IAuthService _auth;
    private readonly PivoxClient _pivox;
    private NSButton _signOut = null!;
    private NSButton _listOrgs = null!;
    private NSTextField _status = null!;

    public DetailViewController(IAuthService auth, PivoxClient pivox)
        : base((string?)null, null)
    {
        _auth = auth;
        _pivox = pivox;
    }

    public override void LoadView()
    {
        View = new NSView(new CGRect(0, 0, 600, 460));
    }

    public override void ViewDidLoad()
    {
        base.ViewDidLoad();

        var session = _auth.Current;
        var who = session is null ? "(no session)" : session.Email ?? session.PivoxUserId;

        var welcome = NSTextField.CreateLabel($"Signed in as {who}");
        welcome.Font = ThemeFonts.NS(ThemeFont.Title);
        welcome.TranslatesAutoresizingMaskIntoConstraints = false;

        _listOrgs = new NSButton
        {
            Title = "List organizations",
            BezelStyle = NSBezelStyle.Push,
            ControlSize = NSControlSize.Regular,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        _listOrgs.Activated += async (_, _) => await ListOrgsAsync();

        _signOut = new NSButton
        {
            Title = "Sign out",
            BezelStyle = NSBezelStyle.Push,
            ControlSize = NSControlSize.Regular,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        _signOut.Activated += async (_, _) => await _auth.SignOutAsync();

        _status = NSTextField.CreateLabel(" ");
        _status.Font = ThemeFonts.NS(ThemeFont.BodySmall);
        _status.TextColor = ThemeColors.NS(ThemeColor.SecondaryForeground);
        _status.Alignment = NSTextAlignment.Left;
        _status.LineBreakMode = NSLineBreakMode.ByWordWrapping;
        _status.UsesSingleLineMode = false;
        _status.TranslatesAutoresizingMaskIntoConstraints = false;

        var stack = new NSStackView
        {
            Orientation = NSUserInterfaceLayoutOrientation.Vertical,
            Alignment = NSLayoutAttribute.Leading,
            Spacing = ThemeMetrics.SpaceMd,
            TranslatesAutoresizingMaskIntoConstraints = false,
            EdgeInsets = new NSEdgeInsets(
                ThemeMetrics.SpaceLg, ThemeMetrics.SpaceLg,
                ThemeMetrics.SpaceLg, ThemeMetrics.SpaceLg),
        };
        stack.AddArrangedSubview(welcome);
        stack.AddArrangedSubview(_listOrgs);
        stack.AddArrangedSubview(_signOut);
        stack.AddArrangedSubview(_status);

        View.AddSubview(stack);
        NSLayoutConstraint.ActivateConstraints(new[]
        {
            stack.TopAnchor.ConstraintEqualTo(View.TopAnchor),
            stack.LeadingAnchor.ConstraintEqualTo(View.LeadingAnchor),
            stack.TrailingAnchor.ConstraintEqualTo(View.TrailingAnchor),
            // Status fills the stack's width minus the horizontal edge
            // insets (Lg on each side) so wrapped text doesn't run under
            // the inset.
            _status.WidthAnchor.ConstraintEqualTo(
                stack.WidthAnchor, 1, -2 * ThemeMetrics.SpaceLg),
        });
    }

    private async Task ListOrgsAsync()
    {
        _listOrgs.Enabled = false;
        _status.StringValue = "Calling pivox-cloud → Organizations.ListOrganizations…";
        try
        {
            var response = await _pivox.Organizations.ListOrganizationsAsync(
                new ListOrganizationsRequest());
            var lines = response.Organizations.Count == 0
                ? "(no organizations)"
                : string.Join("\n", response.Organizations.Select(
                    o => $"  • {o.DisplayName}  [{o.Name}]"));
            _status.StringValue = $"{response.Organizations.Count} org(s):\n{lines}";
        }
        catch (Grpc.Core.RpcException ex)
        {
            _status.StringValue = $"gRPC {ex.StatusCode}: {ex.Status.Detail}";
        }
        catch (Exception ex)
        {
            _status.StringValue = ex.Message;
        }
        finally
        {
            _listOrgs.Enabled = true;
        }
    }
}
