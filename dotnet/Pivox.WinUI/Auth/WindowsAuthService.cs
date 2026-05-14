using System.Net.Http;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Web;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.Web.WebView2.Core;
using Pivox.Shared.Auth;

namespace Pivox.Auth;

/// <summary>
/// Windows implementation of <see cref="IAuthService"/>, wrapping the
/// Firebase C++ SDK via the <c>Pivox.Firebase.Native</c> C++/WinRT
/// component. Mirrors <c>MacOsAuthService</c> in shape: produces
/// <see cref="AuthSession"/> values carrying <see cref="FirebaseIdentity"/>,
/// fires <see cref="CurrentChanged"/> on every state transition.
///
/// Google OAuth runs entirely in C# (PKCE + WebView2 popup + token
/// exchange), then hands the Google credential to the C++ bridge's
/// <c>SignInWithCredentialAsync</c>.
///
/// One instance per process.  Constructed by <see cref="App"/>.
/// </summary>
public sealed class WindowsAuthService : IAuthService
{
    // Google OAuth client ID — same as the macOS app.
    // See MacOsAuthService.GoogleClientID.
    private const string GoogleClientID =
        "45920224787-gb662gbotfv763cqjis53748ctgigncl.apps.googleusercontent.com";

    private const string GoogleCallbackScheme =
        "com.googleusercontent.apps.45920224787-gb662gbotfv763cqjis53748ctgigncl";

    private const string GoogleRedirectUri =
        GoogleCallbackScheme + ":/oauth2callback";

    private static readonly HttpClient s_http = new();

    private readonly Firebase.Native.FirebaseAuthBridge _bridge;
    private AuthSession? _current;

    public WindowsAuthService()
    {
        _bridge = new Firebase.Native.FirebaseAuthBridge();

        // Subscribe BEFORE Initialize.  Firebase's AddAuthStateListener
        // fires synchronously with the current state on registration
        // (restores persisted session from Windows credential storage).
        // If we subscribed after Initialize, that initial fire would
        // go to zero handlers and the restored session would be lost.
        _bridge.AuthStateChanged += async (_, signedIn) =>
        {
            if (!signedIn)
            {
                SetCurrent(null);
                return;
            }

            try
            {
                var jwt = await _bridge.GetIdTokenAsync(false);
                SetCurrent(BuildSession(jwt));
            }
            catch (Exception ex)
            {
                Console.Error.WriteLine(
                    $"[Auth] token fetch on state change failed: {ex.Message}");
            }
        };

        if (!_bridge.Initialize())
        {
            throw new InvalidOperationException(
                "Firebase C++ SDK initialization failed.  " +
                "Verify firebase_config.h values and that the " +
                "Firebase C++ SDK libs are present.");
        }
    }

    public AuthSession? Current => _current;
    public event EventHandler<AuthSession?>? CurrentChanged;

    public async Task<AuthSession> SignInWithEmailAsync(
        string email, string password, CancellationToken ct = default)
    {
        var jwt = await _bridge.SignInWithEmailAsync(email, password)
            .AsTask(ct);

        var session = BuildSession(jwt);
        SetCurrent(session);
        return session;
    }

    public async Task<AuthSession> SignInWithGoogleAsync(
        CancellationToken ct = default)
    {
        var (idToken, accessToken) = await PerformGoogleOAuthAsync(ct);

        var jwt = await _bridge.SignInWithCredentialAsync(
                "google.com", idToken, accessToken)
            .AsTask(ct);

        var session = BuildSession(jwt);
        SetCurrent(session);
        return session;
    }

    public Task SignOutAsync(CancellationToken ct = default)
    {
        _bridge.SignOut();
        SetCurrent(null);
        return Task.CompletedTask;
    }

    public async Task<string> GetIdTokenAsync(CancellationToken ct = default)
    {
        if (!_bridge.IsSignedIn)
        {
            throw new InvalidOperationException("Not signed in.");
        }

        var jwt = await _bridge.GetIdTokenAsync(false).AsTask(ct);

        // Update the cached snapshot — Firebase may have rotated
        // the token transparently.
        var session = BuildSession(jwt);
        SetCurrent(session);
        return session.IdToken;
    }

    // ───── helpers ───────────────────────────────────────────────

    private static AuthSession BuildSession(string jwt)
        => new(jwt, new FirebaseIdentity(jwt));

    private void SetCurrent(AuthSession? session)
    {
        _current = session;
        CurrentChanged?.Invoke(this, session);
    }

    // ───── Google OAuth (PKCE + WebView2 popup) ──────────────────

    private async Task<(string IdToken, string AccessToken)>
        PerformGoogleOAuthAsync(CancellationToken ct)
    {
        var codeVerifier = GeneratePkceVerifier();
        var codeChallenge = GeneratePkceChallenge(codeVerifier);

        var state = Guid.NewGuid().ToString("N");
        var authUrl =
            "https://accounts.google.com/o/oauth2/v2/auth"
            + "?client_id=" + Uri.EscapeDataString(GoogleClientID)
            + "&redirect_uri=" + Uri.EscapeDataString(GoogleRedirectUri)
            + "&response_type=code"
            + "&scope=" + Uri.EscapeDataString("openid email profile")
            + "&code_challenge=" + Uri.EscapeDataString(codeChallenge)
            + "&code_challenge_method=S256"
            + "&state=" + Uri.EscapeDataString(state);

        var authCode = await LaunchOAuthPopupAsync(authUrl, state, ct);

        // Exchange the auth code for tokens.
        var tokenResponse = await s_http.PostAsync(
            "https://oauth2.googleapis.com/token",
            new FormUrlEncodedContent(new Dictionary<string, string>
            {
                ["code"] = authCode,
                ["client_id"] = GoogleClientID,
                ["redirect_uri"] = GoogleRedirectUri,
                ["grant_type"] = "authorization_code",
                ["code_verifier"] = codeVerifier,
            }),
            ct);
        var responseBody = await tokenResponse.Content.ReadAsStringAsync(ct);
        if (!tokenResponse.IsSuccessStatusCode)
        {
            throw new InvalidOperationException(
                $"Google token exchange failed: HTTP {(int)tokenResponse.StatusCode} {responseBody}");
        }

        using var doc = JsonDocument.Parse(responseBody);
        var idToken = doc.RootElement.GetProperty("id_token").GetString()
            ?? throw new InvalidOperationException("Google response missing id_token.");
        var accessToken = doc.RootElement.GetProperty("access_token").GetString()
            ?? throw new InvalidOperationException("Google response missing access_token.");

        return (idToken, accessToken);
    }

    /// <summary>
    /// Opens a WinUI 3 window with a WebView2 control, navigates to
    /// the auth URL, intercepts the redirect to the callback scheme,
    /// and returns the authorization code.  Equivalent of macOS's
    /// <c>ASWebAuthenticationSession</c> and native's <c>OAuthPopup</c>.
    /// </summary>
    private static Task<string> LaunchOAuthPopupAsync(
        string authUrl, string expectedState, CancellationToken ct)
    {
        var tcs = new TaskCompletionSource<string>();

        var window = new Window { Title = "Sign In — Pivox" };
        var webView = new WebView2();
        var callbackFired = false;

        // Cancellation closes the popup window, which triggers the
        // Closed handler below → TrySetCanceled on the TCS.
        ct.Register(() =>
        {
            if (!callbackFired)
                window.DispatcherQueue.TryEnqueue(() => window.Close());
        });

        webView.NavigationStarting += (_, args) =>
        {
            var uri = args.Uri;
            if (uri is null || !uri.StartsWith(GoogleCallbackScheme,
                    StringComparison.OrdinalIgnoreCase))
                return;

            args.Cancel = true;

            if (callbackFired) return;
            callbackFired = true;

            var query = HttpUtility.ParseQueryString(new Uri(uri).Query);
            var code = query["code"];
            var error = query["error"];
            var returnedState = query["state"];

            // Validate the state parameter to prevent CSRF.
            if (returnedState != expectedState)
            {
                tcs.TrySetException(new InvalidOperationException(
                    "OAuth state mismatch — possible CSRF."));
            }
            else if (!string.IsNullOrEmpty(code))
            {
                tcs.TrySetResult(code);
            }
            else if (!string.IsNullOrEmpty(error))
            {
                tcs.TrySetException(new InvalidOperationException(
                    $"Google OAuth error: {error}"));
            }
            else
            {
                tcs.TrySetException(new InvalidOperationException(
                    "No auth code in Google OAuth callback."));
            }

            window.Close();
        };

        window.Closed += (_, _) =>
        {
            if (!callbackFired)
            {
                callbackFired = true;
                tcs.TrySetCanceled();
            }
        };

        window.Content = webView;
        window.AppWindow.Resize(new Windows.Graphics.SizeInt32 { Width = 500, Height = 700 });
        window.Activate();

        webView.Source = new Uri(authUrl);

        return tcs.Task;
    }

    // ───── PKCE ──────────────────────────────────────────────────

    private static string GeneratePkceVerifier()
    {
        Span<byte> bytes = stackalloc byte[32];
        RandomNumberGenerator.Fill(bytes);
        return Base64UrlEncode(bytes);
    }

    private static string GeneratePkceChallenge(string verifier)
    {
        Span<byte> hash = stackalloc byte[32];
        SHA256.HashData(Encoding.ASCII.GetBytes(verifier), hash);
        return Base64UrlEncode(hash);
    }

    private static string Base64UrlEncode(ReadOnlySpan<byte> data)
        => Convert.ToBase64String(data)
            .TrimEnd('=')
            .Replace('+', '-')
            .Replace('/', '_');
}
