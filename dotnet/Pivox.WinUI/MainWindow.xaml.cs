using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Pivox.Api.V1;
using Pivox.Client;
using Pivox.Shared.Auth;
using Pivox.Shared.Organization;

namespace Pivox;

/// <summary>
/// Post-auth shell. Mirrors macOS <c>DetailViewController</c>:
/// welcome banner ("Signed in as X"), active-organization picker,
/// sign-out, and a status line for org-load failures. Real feature
/// content lands here as features ship; the chat panel will dock
/// alongside in Phase B step 2b.
///
/// The session is guaranteed live for this window's lifetime —
/// <c>App.OnAuthChanged</c> swaps to LoginWindow on sign-out.
/// Threading: <c>SelectionChanged</c> fires on the dispatcher thread
/// (UI event), so writes to <see cref="ActiveOrganization.Current"/>
/// take the fast synchronous path — no marshaling in this handler.
/// </summary>
public sealed partial class MainWindow : Window
{
    /// <summary>Upper bound on a single user's org-membership list
    /// for picker purposes. AIP-158 caps server-side page size at
    /// 1000; setting our request size at that ceiling means a user
    /// belonging to up to 1000 orgs gets a complete list in one
    /// round-trip without surfacing pagination plumbing in the
    /// picker. Real-world membership counts are O(1–10); the ceiling
    /// exists to defend against a future heavy-user scenario rather
    /// than to support it. If a user ever has &gt;1000 memberships,
    /// the truncation logging below makes the shortfall visible at
    /// runtime — at that point the picker itself needs a different
    /// affordance (search, virtualized list) before pagination is
    /// even useful UX. Mirrors macOS DetailViewController.</summary>
    private const int OrganizationListPageSize = 1000;

    private readonly IAuthService _auth;
    private readonly PivoxClient _pivox;
    private readonly ActiveOrganization _activeOrganization;
    // Mirrors the picker's items by index. Empty while loading or on
    // error; populated 1:1 with picker entries after a successful list
    // load. Indexed into by SelectionChanged to resolve the chosen
    // org's resource name.
    private readonly List<Organization> _organizations = [];
    // Suppress the SelectionChanged write-through during programmatic
    // selection in PopulateOrganizationsAsync. Without this guard,
    // selecting the persisted index would fire the handler and write
    // back the same org name — a same-value no-op today, but a
    // semantic muddle (the "first interaction commits" contract on
    // the visual-default branch wants no write at all).
    private bool _suppressSelectionWrite;

    public MainWindow(
        IAuthService auth,
        PivoxClient pivox,
        ActiveOrganization activeOrganization)
    {
        InitializeComponent();
        _auth = auth;
        _pivox = pivox;
        _activeOrganization = activeOrganization;

        ExtendsContentIntoTitleBar = true;
        SetTitleBar(AppTitleBar);

        var session = _auth.Current;
        var who = session is null
            ? "(no session)"
            : session.Email ?? session.PivoxUserId;
        WelcomeText.Text = $"Signed in as {who}";

        // Fire-and-forget org list load. Continuation lands on the
        // dispatcher thread because async/await captures the UI
        // SyncContext on the call site (this constructor runs on it).
        _ = PopulateOrganizationsAsync();
    }

    /// <summary>Load the user's organizations from pivox-cloud,
    /// populate the dropdown, and select the persisted
    /// ActiveOrganization (or default to the first if no persisted
    /// value matches). Disables the picker while loading; enables on
    /// success; surfaces a status message on failure. Mirrors macOS
    /// <c>DetailViewController.PopulateOrganizationsAsync</c>.</summary>
    private async Task PopulateOrganizationsAsync()
    {
        StatusText.Text = "";
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
                System.Diagnostics.Debug.WriteLine(
                    "[MainWindow] ListOrganizations returned "
                    + $"{response.Organizations.Count} orgs with a "
                    + "NextPageToken set; the picker is showing only "
                    + "the first page. Add pagination to surface the rest.");
            }

            _organizations.Clear();
            WithSuppressedSelectionWrite(() =>
            {
                OrganizationPicker.Items.Clear();

                if (response.Organizations.Count == 0)
                {
                    OrganizationPicker.PlaceholderText =
                        "No organizations available";
                    OrganizationPicker.IsEnabled = false;
                    StatusText.Text =
                        "You don't belong to any organizations yet. "
                        + "Ask an admin to invite you or create one.";
                    return;
                }

                foreach (var org in response.Organizations)
                {
                    _organizations.Add(org);
                    OrganizationPicker.Items.Add(org.DisplayName);
                }

                OrganizationPicker.IsEnabled = true;

                // Restore the persisted selection if it's still in
                // the membership list; otherwise default to first AS
                // A VISUAL DEFAULT ONLY — don't write through to
                // ActiveOrganization. Persisting an unsolicited
                // default pretends the user picked when they didn't,
                // which (a) removes the future ability to change the
                // default without disrupting users, and (b) makes the
                // picker's "remembered choice" semantics dishonest.
                // On the user's first real interaction with the
                // picker (OrganizationPicker_SelectionChanged), we
                // commit. Mirrors macOS rationale.
                var persisted = _activeOrganization.Current;
                var matchIndex = persisted is null
                    ? -1
                    : _organizations.FindIndex(o => o.Name == persisted);
                OrganizationPicker.SelectedIndex = matchIndex >= 0 ? matchIndex : 0;
                // ActiveOrganization.Current is unchanged either way:
                // matched ⇒ already equal; unmatched ⇒ visual-only
                // default. The suppression flag ensures the handler
                // doesn't write back during this programmatic set.
            });
        }
        catch (Grpc.Core.RpcException ex)
        {
            ShowOrganizationLoadFailure();
            StatusText.Text =
                $"Failed to load organizations: gRPC {ex.StatusCode} "
                + $"({ex.Status.Detail})";
        }
        catch (Exception ex)
        {
            ShowOrganizationLoadFailure();
            StatusText.Text = $"Failed to load organizations: {ex.Message}";
        }
    }

    /// <summary>Render the picker into its error state: surface the
    /// failure both as a visible (disabled) item and as a disabled
    /// control, mirroring macOS DetailViewController which uses
    /// <c>AddItem("Couldn't load organizations")</c>. PlaceholderText
    /// alone would only show on focus loss with no item selected —
    /// the explicit item makes the failure visible regardless of
    /// focus state. The status line carries the structured error
    /// detail.</summary>
    private void ShowOrganizationLoadFailure()
    {
        _organizations.Clear();
        WithSuppressedSelectionWrite(() =>
        {
            OrganizationPicker.Items.Clear();
            OrganizationPicker.Items.Add("Couldn't load organizations");
            OrganizationPicker.SelectedIndex = 0;
            OrganizationPicker.IsEnabled = false;
        });
    }

    /// <summary>Run <paramref name="body"/> with
    /// <see cref="_suppressSelectionWrite"/> raised, so any
    /// programmatic <c>SelectedIndex</c> assignment or
    /// <c>Items.Clear()</c> inside doesn't fire
    /// <see cref="OrganizationPicker_SelectionChanged"/> as if it
    /// were a user choice. Always restores the flag on the way out,
    /// including exception paths.</summary>
    private void WithSuppressedSelectionWrite(Action body)
    {
        _suppressSelectionWrite = true;
        try
        {
            body();
        }
        finally
        {
            _suppressSelectionWrite = false;
        }
    }

    /// <summary>User picked a different organization from the
    /// dropdown. Updates <see cref="ActiveOrganization.Current"/>,
    /// which fires PropertyChanged and wipes any per-org viewmodel
    /// state downstream (chat history, conversation cursor, etc.)
    /// when the chat panel lands in Phase B step 2b.</summary>
    private void OrganizationPicker_SelectionChanged(
        object sender, SelectionChangedEventArgs e)
    {
        if (_suppressSelectionWrite) return;
        var index = OrganizationPicker.SelectedIndex;
        if (index < 0 || index >= _organizations.Count) return;
        _activeOrganization.Current = _organizations[index].Name;
    }

    private async void SignOutButton_Click(object sender, RoutedEventArgs e)
        => await _auth.SignOutAsync();
}
