using System.Net.Http;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Web;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Pivox.Shared;
using Pivox.Shared.Auth;

namespace Pivox.Auth;

/// <summary>
/// Windows implementation of <see cref="IAuthService"/>, wrapping the
/// Firebase C++ SDK via the <c>Pivox.Firebase.Native</c> C++/WinRT
/// component. Mirrors <c>MacOsAuthService</c> in shape: produces
/// <see cref="AuthSession"/> values carrying <see cref="FirebaseIdentity"/>,
/// fires <see cref="CurrentChanged"/> on every state transition.
///
/// OAuth flows (Google, GitHub, SSO) run in C# via WebView2 popup
/// windows, then hand credentials to the typed bridge methods.
///
/// One instance per process. Constructed by <see cref="App"/>.
/// </summary>
public sealed class WindowsAuthService : IAuthService
{
    // ── Google OAuth (direct PKCE — no broker) ───────────────────
    private const string GoogleClientID =
        "45920224787-gb662gbotfv763cqjis53748ctgigncl.apps.googleusercontent.com";
    private const string GoogleCallbackScheme =
        "com.googleusercontent.apps.45920224787-gb662gbotfv763cqjis53748ctgigncl";
    private const string GoogleRedirectUri =
        GoogleCallbackScheme + ":/oauth2callback";

    // ── Broker flows (GitHub, SSO) ───────────────────────────────
    private const string BrokerCallbackScheme = "pivox";
    private const string BrokerReturnUrl = "pivox://auth-complete";

    private static readonly HttpClient s_http = new();

    private readonly Firebase.Native.FirebaseAuthBridge _bridge;
    private AuthSession? _current;

    public WindowsAuthService()
    {
        _bridge = new Firebase.Native.FirebaseAuthBridge();

        // Subscribe BEFORE Initialize — Firebase's AddAuthStateListener
        // fires synchronously with the current state on registration.
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
                "Firebase C++ SDK initialization failed.");
        }
    }

    public AuthSession? Current => _current;
    public event EventHandler<AuthSession?>? CurrentChanged;

    // ── primary sign-in paths ────────────────────────────────────

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
        var jwt = await _bridge.SignInWithGoogleCredentialAsync(
                idToken, accessToken)
            .AsTask(ct);
        var session = BuildSession(jwt);
        SetCurrent(session);
        return session;
    }

    public async Task<AuthSession> SignInWithGitHubAsync(
        CancellationToken ct = default)
    {
        var accessToken = await PerformBrokerOAuthAsync(
            providerSlug: "github",
            expectedProvider: "github",
            expectedKind: "github_access_token",
            tokenFieldName: "token",
            loginHint: null,
            ct: ct);

        var jwt = await _bridge.SignInWithGitHubCredentialAsync(accessToken)
            .AsTask(ct);
        var session = BuildSession(jwt);
        SetCurrent(session);
        return session;
    }

    public async Task<AuthSession> SignInWithSsoAsync(
        string providerId, string loginHint, CancellationToken ct = default)
    {
        var (idToken, nonce) = await PerformSsoBrokerOAuthAsync(
            providerId, loginHint, ct);

        var jwt = await _bridge.SignInWithOidcCredentialAsync(
                providerId, idToken, nonce)
            .AsTask(ct);
        var session = BuildSession(jwt);
        SetCurrent(session);
        return session;
    }

    // ── account lifecycle ────────────────────────────────────────

    public async Task<AuthSession> CreateAccountAsync(
        string email, string password, string displayName,
        CancellationToken ct = default)
    {
        var jwt = await _bridge.CreateAccountAsync(
                email, password, displayName)
            .AsTask(ct);
        var session = BuildSession(jwt);
        SetCurrent(session);
        return session;
    }

    public Task SendPasswordResetAsync(
        string email, CancellationToken ct = default)
        => _bridge.SendPasswordResetAsync(email).AsTask(ct);

    public Task SignOutAsync(CancellationToken ct = default)
    {
        _bridge.SignOut();
        SetCurrent(null);
        return Task.CompletedTask;
    }

    // ── SSO discovery ────────────────────────────────────────────

    public async Task<string?> ResolveSsoProviderAsync(
        string email, CancellationToken ct = default)
    {
        var trimmed = email.Trim();
        if (string.IsNullOrEmpty(trimmed)) return null;

        // pivox-cloud's resolveProvider is a REST endpoint — no auth
        // header (pre-auth surface). AOT-clean: hand-built JSON body
        // to avoid IL2026/IL3050 from JsonSerializer on anonymous types.
        var url = $"{CloudConfig.BrokerBaseUrl}/internal/v1/auth:resolveProvider";
        var encoded = System.Text.Encodings.Web.JavaScriptEncoder.Default
            .Encode(trimmed);
        var body = $"{{\"email\":\"{encoded}\"}}";
        using var req = new HttpRequestMessage(HttpMethod.Post, url)
        {
            Content = new StringContent(body, Encoding.UTF8, "application/json"),
        };

        using var resp = await s_http.SendAsync(req, ct);
        switch ((int)resp.StatusCode)
        {
            case 200:
                var payload = await resp.Content.ReadAsStringAsync(ct);
                using (var doc = JsonDocument.Parse(payload))
                {
                    return doc.RootElement.TryGetProperty("provider_id", out var p)
                        ? p.GetString()
                        : null;
                }
            case 404:
                return null;
            default:
                throw new InvalidOperationException(
                    $"resolveProvider failed: HTTP {(int)resp.StatusCode}");
        }
    }

    // ── token access ─────────────────────────────────────────────

    public async Task<string> GetIdTokenAsync(CancellationToken ct = default)
    {
        if (!_bridge.IsSignedIn)
            throw new InvalidOperationException("Not signed in.");

        var jwt = await _bridge.GetIdTokenAsync(false).AsTask(ct);
        var session = BuildSession(jwt);
        SetCurrent(session);
        return session.IdToken;
    }

    // ── helpers ──────────────────────────────────────────────────

    private static AuthSession BuildSession(string jwt)
        => new(jwt, new FirebaseIdentity(jwt));

    private void SetCurrent(AuthSession? session)
    {
        // Dedupe: same JWT → no semantic change.
        if (_current is null && session is null) return;
        if (_current is not null && session is not null
            && _current.IdToken == session.IdToken)
            return;

        _current = session;
        CurrentChanged?.Invoke(this, session);
    }

    // ── Google OAuth (PKCE + WebView2 popup) ─────────────────────

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

        var authCode = await LaunchOAuthPopupAsync(
            authUrl, GoogleCallbackScheme, state, ct);

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

    // ── Broker OAuth (GitHub) ────────────────────────────────────

    private async Task<string> PerformBrokerOAuthAsync(
        string providerSlug,
        string expectedProvider,
        string expectedKind,
        string tokenFieldName,
        string? loginHint,
        CancellationToken ct)
    {
        var encodedReturn = Uri.EscapeDataString(BrokerReturnUrl);
        var startUrl =
            $"{CloudConfig.BrokerBaseUrl}/internal/v1/auth/{Uri.EscapeDataString(providerSlug)}/start"
            + $"?return={encodedReturn}";
        if (!string.IsNullOrWhiteSpace(loginHint))
            startUrl += $"&login_hint={Uri.EscapeDataString(loginHint.Trim())}";

        var callbackUrl = await LaunchBrokerPopupAsync(
            startUrl, BrokerCallbackScheme, ct);

        var items = ParseFragment(new Uri(callbackUrl));

        if (items.TryGetValue("error", out var errCode))
        {
            var desc = items.TryGetValue("error_description", out var d) ? d : errCode;
            throw new InvalidOperationException($"Broker sign-in failed: {desc}");
        }
        if (!items.TryGetValue("provider", out var provider) || provider != expectedProvider)
            throw new InvalidOperationException(
                $"Broker returned wrong provider (got '{provider}', expected '{expectedProvider}').");
        if (!items.TryGetValue("kind", out var kind) || kind != expectedKind)
            throw new InvalidOperationException(
                $"Broker returned unexpected credential kind '{kind}'.");
        if (!items.TryGetValue(tokenFieldName, out var token) || string.IsNullOrEmpty(token))
            throw new InvalidOperationException(
                $"Broker callback missing '{tokenFieldName}'.");
        return token;
    }

    // ── Broker SSO/OIDC ──────────────────────────────────────────

    private async Task<(string IdToken, string Nonce)>
        PerformSsoBrokerOAuthAsync(
            string providerId, string loginHint, CancellationToken ct)
    {
        var encodedReturn = Uri.EscapeDataString(BrokerReturnUrl);
        var startUrl =
            $"{CloudConfig.BrokerBaseUrl}/internal/v1/auth/{Uri.EscapeDataString(providerId)}/start"
            + $"?return={encodedReturn}";
        if (!string.IsNullOrWhiteSpace(loginHint))
            startUrl += $"&login_hint={Uri.EscapeDataString(loginHint.Trim())}";

        var callbackUrl = await LaunchBrokerPopupAsync(
            startUrl, BrokerCallbackScheme, ct);

        var items = ParseFragment(new Uri(callbackUrl));

        if (items.TryGetValue("error", out var errCode))
        {
            var desc = items.TryGetValue("error_description", out var d) ? d : errCode;
            throw new InvalidOperationException($"SSO sign-in failed: {desc}");
        }
        if (!items.TryGetValue("provider", out var provider) || provider != providerId)
            throw new InvalidOperationException(
                $"Broker returned wrong provider (got '{provider}', expected '{providerId}').");
        if (!items.TryGetValue("kind", out var kind) || kind != "oidc_id_token")
            throw new InvalidOperationException(
                $"Broker returned unexpected credential kind '{kind}'.");
        if (!items.TryGetValue("token", out var idToken) || string.IsNullOrEmpty(idToken))
            throw new InvalidOperationException("Broker callback missing id_token.");
        if (!items.TryGetValue("nonce", out var nonce) || string.IsNullOrEmpty(nonce))
            throw new InvalidOperationException("Broker callback missing nonce.");
        return (idToken, nonce);
    }

    // ── WebView2 popup helpers ───────────────────────────────────

    /// <summary>
    /// OAuth popup for direct flows (Google PKCE). Intercepts
    /// navigation to <paramref name="callbackScheme"/> in the query
    /// string, validates the state parameter, returns the auth code.
    /// </summary>
    private static Task<string> LaunchOAuthPopupAsync(
        string authUrl, string callbackScheme, string expectedState,
        CancellationToken ct)
    {
        var tcs = new TaskCompletionSource<string>();
        var window = new Window { Title = "Sign In — Pivox" };
        var webView = new WebView2();
        var callbackFired = false;

        ct.Register(() =>
        {
            if (!callbackFired)
                window.DispatcherQueue.TryEnqueue(() => window.Close());
        });

        webView.NavigationStarting += (_, args) =>
        {
            var uri = args.Uri;
            if (uri is null || !uri.StartsWith(callbackScheme,
                    StringComparison.OrdinalIgnoreCase))
                return;

            args.Cancel = true;
            if (callbackFired) return;
            callbackFired = true;

            var query = HttpUtility.ParseQueryString(new Uri(uri).Query);
            var returnedState = query["state"];

            if (returnedState != expectedState)
            {
                tcs.TrySetException(new InvalidOperationException(
                    "OAuth state mismatch — possible CSRF."));
            }
            else if (!string.IsNullOrEmpty(query["code"]))
            {
                tcs.TrySetResult(query["code"]!);
            }
            else if (!string.IsNullOrEmpty(query["error"]))
            {
                tcs.TrySetException(new InvalidOperationException(
                    $"Google OAuth error: {query["error"]}"));
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
            if (!callbackFired) { callbackFired = true; tcs.TrySetCanceled(); }
        };

        window.Content = webView;
        window.AppWindow.Resize(new Windows.Graphics.SizeInt32 { Width = 500, Height = 700 });
        window.Activate();
        webView.Source = new Uri(authUrl);

        return tcs.Task;
    }

    /// <summary>
    /// WebView2 popup for broker flows (GitHub, SSO). Intercepts
    /// navigation to <paramref name="callbackScheme"/> and returns
    /// the full callback URL (fragment-bearing).
    /// </summary>
    private static Task<string> LaunchBrokerPopupAsync(
        string startUrl, string callbackScheme, CancellationToken ct)
    {
        var tcs = new TaskCompletionSource<string>();
        var window = new Window { Title = "Sign In — Pivox" };
        var webView = new WebView2();
        var callbackFired = false;

        ct.Register(() =>
        {
            if (!callbackFired)
                window.DispatcherQueue.TryEnqueue(() => window.Close());
        });

        webView.NavigationStarting += (_, args) =>
        {
            var uri = args.Uri;
            if (uri is null || !uri.StartsWith(callbackScheme,
                    StringComparison.OrdinalIgnoreCase))
                return;

            args.Cancel = true;
            if (callbackFired) return;
            callbackFired = true;

            tcs.TrySetResult(uri);
            window.Close();
        };

        window.Closed += (_, _) =>
        {
            if (!callbackFired) { callbackFired = true; tcs.TrySetCanceled(); }
        };

        window.Content = webView;
        window.AppWindow.Resize(new Windows.Graphics.SizeInt32 { Width = 500, Height = 700 });
        window.Activate();
        webView.Source = new Uri(startUrl);

        return tcs.Task;
    }

    // ── Fragment parser ──────────────────────────────────────────

    private static Dictionary<string, string> ParseFragment(Uri callback)
    {
        var fragment = callback.Fragment.TrimStart('#');
        var nv = HttpUtility.ParseQueryString(fragment);
        var dict = new Dictionary<string, string>(nv.Count);
        foreach (string? key in nv)
        {
            if (key is null) continue;
            var val = nv[key];
            if (val is not null) dict[key] = val;
        }
        return dict;
    }

    // ── PKCE ─────────────────────────────────────────────────────

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
