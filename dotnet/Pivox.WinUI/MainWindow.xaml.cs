using Microsoft.UI.Xaml;
using Pivox.Api.V1;
using Pivox.Client;
using Pivox.Shared.Auth;

namespace Pivox;

/// <summary>
/// Post-auth shell. Mirrors macOS <c>DetailViewController</c>:
/// shows who's signed in, a ListOrganizations smoke-test button,
/// and a sign-out button. Real content lands here as features ship.
///
/// The session is guaranteed live for this window's lifetime —
/// <c>App.OnAuthChanged</c> swaps to LoginWindow on sign-out.
/// </summary>
public sealed partial class MainWindow : Window
{
    private readonly IAuthService _auth;
    private readonly PivoxClient _pivox;

    public MainWindow(IAuthService auth, PivoxClient pivox)
    {
        InitializeComponent();
        _auth = auth;
        _pivox = pivox;

        ExtendsContentIntoTitleBar = true;
        SetTitleBar(AppTitleBar);

        var session = _auth.Current;
        var who = session is null
            ? "(no session)"
            : session.Email ?? session.PivoxUserId;
        WelcomeText.Text = $"Signed in as {who}";
    }

    private async void ListOrgsButton_Click(object sender, RoutedEventArgs e)
    {
        ListOrgsButton.IsEnabled = false;
        StatusText.Text = "Calling pivox-cloud \u2192 Organizations.ListOrganizations\u2026";
        try
        {
            var response = await _pivox.Organizations
                .ListOrganizationsAsync(new ListOrganizationsRequest());

            var lines = response.Organizations.Count == 0
                ? "(no organizations)"
                : string.Join("\n", response.Organizations.Select(
                    o => $"  \u2022 {o.DisplayName}  [{o.Name}]"));
            StatusText.Text = $"{response.Organizations.Count} org(s):\n{lines}";
        }
        catch (Grpc.Core.RpcException ex)
        {
            StatusText.Text = $"gRPC {ex.StatusCode}: {ex.Status.Detail}";
        }
        catch (Exception ex)
        {
            StatusText.Text = ex.Message;
        }
        finally
        {
            ListOrgsButton.IsEnabled = true;
        }
    }

    private async void SignOutButton_Click(object sender, RoutedEventArgs e)
        => await _auth.SignOutAsync();
}
