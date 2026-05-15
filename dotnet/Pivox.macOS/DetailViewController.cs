using AppKit;
using CoreGraphics;
using Foundation;
using ObjCRuntime;
using Pivox.Api.V1;
using Pivox.Client;
using Pivox.Shared.Auth;
using Pivox.Shared.Organization;
using Pivox.Shared.UI;
using Pivox.UI;

namespace Pivox;

/// <summary>
/// Post-auth detail pane. Hosts the active-organization picker
/// (NSPopUpButton populated from
/// <c>Organizations.ListOrganizations</c>) and — once chat lands
/// (Phase B step 2b) — the chat surface. Sign-out lives down here
/// too for now; will move into a profile/account menu in a later
/// pass.
///
/// Threading: <see cref="IAuthService.CurrentChanged"/> is observed
/// at the AppDelegate level (it drives window-swap routing). This
/// VC is only constructed when signed in, so it doesn't subscribe —
/// the session is guaranteed live for its lifetime. The org-list
/// load happens once in <see cref="ViewDidLoad"/>; future
/// org-membership changes will require a refresh trigger we don't
/// have yet.
/// </summary>
[Register("DetailViewController")]
public sealed class DetailViewController : NSViewController
{
    private readonly IAuthService _auth;
    private readonly PivoxClient _pivox;
    private readonly ActiveOrganization _activeOrganization;
    private NSPopUpButton _organizationPicker = null!;
    private NSButton _signOut = null!;
    private NSTextField _status = null!;
    // Mirrors the picker's menu items by index. Index 0 is the
    // "Select an organization" placeholder when no org is selected;
    // subsequent indices map 1:1 to entries here.
    private readonly List<Organization> _organizations = new();

    public DetailViewController(
        IAuthService auth,
        PivoxClient pivox,
        ActiveOrganization activeOrganization)
        : base((string?)null, null)
    {
        _auth = auth;
        _pivox = pivox;
        _activeOrganization = activeOrganization;
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

        var organizationLabel = NSTextField.CreateLabel("Organization");
        organizationLabel.Font = ThemeFonts.NS(ThemeFont.BodySmall);
        organizationLabel.TextColor = ThemeColors.NS(ThemeColor.SecondaryForeground);
        organizationLabel.TranslatesAutoresizingMaskIntoConstraints = false;

        _organizationPicker = new NSPopUpButton(CGRect.Empty, pullsDown: false)
        {
            ControlSize = NSControlSize.Regular,
            TranslatesAutoresizingMaskIntoConstraints = false,
            Enabled = false,  // Disabled until the list loads.
        };
        // Initial placeholder (overwritten by PopulateOrganizationsAsync).
        _organizationPicker.AddItem("Loading organizations…");
        _organizationPicker.Activated += OnOrganizationSelectionChanged;

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
        stack.AddArrangedSubview(organizationLabel);
        stack.AddArrangedSubview(_organizationPicker);
        stack.AddArrangedSubview(_signOut);
        stack.AddArrangedSubview(_status);

        View.AddSubview(stack);
        NSLayoutConstraint.ActivateConstraints(new[]
        {
            stack.TopAnchor.ConstraintEqualTo(View.TopAnchor),
            stack.LeadingAnchor.ConstraintEqualTo(View.LeadingAnchor),
            stack.TrailingAnchor.ConstraintEqualTo(View.TrailingAnchor),
            // Picker spans the stack's content width so its menu chrome
            // matches the rest of the rows visually.
            _organizationPicker.WidthAnchor.ConstraintEqualTo(
                stack.WidthAnchor, 1, -2 * ThemeMetrics.SpaceLg),
            // Status fills the stack's width minus the horizontal edge
            // insets (Lg on each side) so wrapped text doesn't run under
            // the inset.
            _status.WidthAnchor.ConstraintEqualTo(
                stack.WidthAnchor, 1, -2 * ThemeMetrics.SpaceLg),
        });

        // Kick off the async org list load. Continuation runs on the
        // main thread via captured SyncContext (AppKit's UI thread is
        // installed by .NET-for-macOS's run-loop integration).
        _ = PopulateOrganizationsAsync();
    }

    /// <summary>Load the user's organizations from pivox-cloud,
    /// populate the dropdown, and select the persisted ActiveOrganization
    /// (or default to the first if no persisted value matches).
    /// Disables the picker while loading; enables on success;
    /// surfaces a status message on failure.</summary>
    private async Task PopulateOrganizationsAsync()
    {
        _status.StringValue = "";
        try
        {
            var response = await _pivox.Organizations.ListOrganizationsAsync(
                new ListOrganizationsRequest());

            _organizations.Clear();
            _organizationPicker.RemoveAllItems();

            if (response.Organizations.Count == 0)
            {
                _organizationPicker.AddItem("No organizations available");
                _organizationPicker.Enabled = false;
                _status.StringValue =
                    "You don't belong to any organizations yet. Ask an "
                    + "admin to invite you or create one.";
                return;
            }

            foreach (var org in response.Organizations)
            {
                _organizations.Add(org);
                _organizationPicker.AddItem(org.DisplayName);
            }

            _organizationPicker.Enabled = true;

            // Restore the persisted selection if it's still in the
            // membership list; otherwise default to the first org.
            // Either way we WRITE through ActiveOrganization.Current so
            // a defaulted-to-first selection persists for next launch
            // (the user explicitly opting-into the default counts).
            var persisted = _activeOrganization.Current;
            var matchIndex = persisted is null
                ? -1
                : _organizations.FindIndex(o => o.Name == persisted);
            var selectedIndex = matchIndex >= 0 ? matchIndex : 0;

            _organizationPicker.SelectItem(selectedIndex);
            _activeOrganization.Current = _organizations[selectedIndex].Name;
        }
        catch (Grpc.Core.RpcException ex)
        {
            _organizationPicker.RemoveAllItems();
            _organizationPicker.AddItem("Couldn't load organizations");
            _organizationPicker.Enabled = false;
            _status.StringValue =
                $"Failed to load organizations: gRPC {ex.StatusCode} "
                + $"({ex.Status.Detail})";
        }
        catch (Exception ex)
        {
            _organizationPicker.RemoveAllItems();
            _organizationPicker.AddItem("Couldn't load organizations");
            _organizationPicker.Enabled = false;
            _status.StringValue = $"Failed to load organizations: {ex.Message}";
        }
    }

    /// <summary>User picked a different organization from the
    /// dropdown. Updates ActiveOrganization, which fires
    /// PropertyChanged and wipes any per-org viewmodel state
    /// downstream (chat history, conversation cursor, etc.).</summary>
    private void OnOrganizationSelectionChanged(object? sender, EventArgs e)
    {
        var index = (int)_organizationPicker.IndexOfSelectedItem;
        if (index < 0 || index >= _organizations.Count) return;
        _activeOrganization.Current = _organizations[index].Name;
    }
}
