using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Media;
using Pivox.Api.V1;
using Pivox.Client;
using Pivox.Shared.Auth;

namespace Pivox;

/// <summary>
/// Test harness mirroring <c>DetailViewController</c> on macOS.
/// Knows nothing about Firebase — calls <see cref="IAuthService"/>
/// + <see cref="PivoxClient"/>.
/// </summary>
public sealed partial class MainWindow : Window
{
    private readonly IAuthService _auth;
    private readonly PivoxClient _pivox;
    private readonly DispatcherQueue _dispatcher;

    public MainWindow(IAuthService auth, PivoxClient pivox)
    {
        InitializeComponent();
        _auth = auth;
        _pivox = pivox;
        _dispatcher = DispatcherQueue.GetForCurrentThread();

        // Extend the backdrop material through the title bar.
        ExtendsContentIntoTitleBar = true;
        SetTitleBar(AppTitleBar);

        _auth.CurrentChanged += OnAuthChanged;

        // If a session was already restored before we subscribed,
        // render it now.
        RenderAuthState(_auth.Current);
    }

    // ───── auth ──────────────────────────────────────────────────

    private async void SignInButton_Click(object sender, RoutedEventArgs e)
    {
        var email = EmailBox.Text?.Trim() ?? "";
        var password = PasswordBox.Password ?? "";
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
            Status($"Sign-in failed: {ex.Message}", isError: true);
        }
        finally
        {
            SetLoading(false);
        }
    }

    private async void GoogleSignInButton_Click(object sender, RoutedEventArgs e)
    {
        SetLoading(true);
        Status("Opening Google sign-in...");
        try
        {
            await _auth.SignInWithGoogleAsync();
        }
        catch (Exception ex)
        {
            Status($"Google sign-in failed: {ex.Message}", isError: true);
        }
        finally
        {
            SetLoading(false);
        }
    }

    private async void SignOutButton_Click(object sender, RoutedEventArgs e)
    {
        try
        {
            await _auth.SignOutAsync();
            Status("Signed out.");
        }
        catch (Exception ex)
        {
            Status($"Sign out failed: {ex.Message}", isError: true);
        }
    }

    private void OnAuthChanged(object? sender, AuthSession? session)
    {
        // Firebase fires on its internal thread; marshal to UI.
        _dispatcher.TryEnqueue(() => RenderAuthState(session));
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
            $"Signed in.\n" +
            $"pivoxUserId={session.PivoxUserId}\n" +
            $"email={session.Email}\n" +
            $"token={preview}\n" +
            $"expires={session.ExpiresAt:HH:mm:ss}");
    }

    // ───── gRPC test ─────────────────────────────────────────────

    private async void ListOrgsButton_Click(object sender, RoutedEventArgs e)
    {
        if (_auth.Current is null)
        {
            Status("Sign in first.", isError: true);
            return;
        }

        Status("Calling pivox-cloud -> Organizations.ListOrganizations...");
        try
        {
            var response = await _pivox.Organizations
                .ListOrganizationsAsync(new ListOrganizationsRequest());

            var lines = response.Organizations
                .Select(o => $"  - {o.Name}  ({o.DisplayName})")
                .DefaultIfEmpty("  (no organizations)")
                .ToArray();
            Status(
                $"ListOrganizations returned {response.Organizations.Count} org(s):\n" +
                string.Join("\n", lines));
        }
        catch (Grpc.Core.RpcException ex)
        {
            Status($"gRPC {ex.StatusCode}: {ex.Status.Detail}", isError: true);
        }
        catch (Exception ex)
        {
            Status($"{ex.Message}", isError: true);
        }
    }

    // ───── helpers ───────────────────────────────────────────────

    private void SetLoading(bool loading)
    {
        SignInButton.IsEnabled = !loading;
        SignInButton.Content = loading ? "Signing in..." : "Sign In";
        EmailBox.IsEnabled = !loading;
        PasswordBox.IsEnabled = !loading;
        GoogleSignInButton.IsEnabled = !loading;
    }

    private void Status(string message, bool isError = false)
    {
        StatusText.Text = message;
        StatusText.Foreground = isError
            ? new SolidColorBrush(Microsoft.UI.Colors.Red)
            : (Brush)Application.Current.Resources["TextFillColorPrimaryBrush"];
    }
}
