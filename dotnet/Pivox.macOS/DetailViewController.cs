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
    /// <summary>Upper bound on a single user's org-membership list
    /// for picker purposes. AIP-158 caps server-side page size at
    /// 1000; setting our request size at that ceiling means a user
    /// belonging to up to 1000 orgs gets a complete list in one
    /// round-trip without surfacing pagination plumbing in the
    /// picker. Real-world membership counts are O(1–10); the
    /// ceiling exists to defend against a future heavy-user
    /// scenario rather than to support it. If a user ever has
    /// &gt;1000 memberships, the truncation logging below makes the
    /// shortfall visible at runtime — at that point the picker
    /// itself needs a different affordance (search, virtualized
    /// list) before pagination is even useful UX.</summary>
    private const int OrganizationListPageSize = 1000;

    private async Task PopulateOrganizationsAsync()
    {
        _status.StringValue = "";
        try
        {
            var response = await _pivox.Organizations.ListOrganizationsAsync(
                new ListOrganizationsRequest
                {
                    PageSize = OrganizationListPageSize,
                });

            // If the server filled the page AND signaled more, log
            // it. Defaulting to a single page is wrong for a >1k
            // membership user; we'd silently drop the tail of the
            // list. The picker doesn't paginate today — this surfaces
            // the shortfall so a future Phase D affordance has a
            // breadcrumb to follow rather than a silent regression.
            if (!string.IsNullOrEmpty(response.NextPageToken))
            {
                Console.Error.WriteLine(
                    "[DetailViewController] ListOrganizations returned "
                    + $"{response.Organizations.Count} orgs with a "
                    + "NextPageToken set; the picker is showing only "
                    + "the first page. Add pagination to surface the rest.");
            }

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
            // membership list; otherwise default to first AS A
            // VISUAL DEFAULT ONLY — don't write through to
            // ActiveOrganization. Persisting an unsolicited default
            // pretends the user picked when they didn't, which
            // (a) removes the future ability to change the default
            // without disrupting users, and (b) makes the picker's
            // "remembered choice" semantics dishonest. On the user's
            // first real interaction with the picker
            // (OnOrganizationSelectionChanged), we'll persist.
            var persisted = _activeOrganization.Current;
            var matchIndex = persisted is null
                ? -1
                : _organizations.FindIndex(o => o.Name == persisted);
            if (matchIndex >= 0)
            {
                _organizationPicker.SelectItem(matchIndex);
                // _activeOrganization.Current already matches; no
                // re-write needed (same-value sets are suppressed
                // anyway but skipping the round-trip is cleaner).
            }
            else
            {
                // Visual default to first; ActiveOrganization stays
                // at whatever it was (null on first launch, or some
                // stale value the user is no longer a member of).
                // Downstream consumers (chat) see Current as null
                // and gate their UI on it. The next picker
                // interaction commits a real choice.
                _organizationPicker.SelectItem(0);
            }
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
