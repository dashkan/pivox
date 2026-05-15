using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Web;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Pivox.Shared;
using Pivox.Shared.Auth;
using Pivox.Shared.Http;

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

    private readonly Firebase.Native.FirebaseAuthBridge _bridge;
    private readonly Microsoft.UI.Dispatching.DispatcherQueue _dispatcher;
    private AuthSession? _current;

    public WindowsAuthService()
    {
        _dispatcher = Microsoft.UI.Dispatching.DispatcherQueue.GetForCurrentThread();
        _bridge = new Firebase.Native.FirebaseAuthBridge();

        // Subscribe BEFORE Initialize — Firebase's AddAuthStateListener
        // fires synchronously with the current state on registration.
        // The event fires on Firebase's internal thread (CLAUDE.md
        // Rule 12) — marshal to UI thread before calling SetCurrent.
        _bridge.AuthStateChanged += (_, signedIn) =>
        {
            _dispatcher.TryEnqueue(async () =>
            {
                if (!signedIn)
                {
                    SetCurrent(null);
                    return;
                }
                try
                {
                    // Force refresh on state restore so Firebase
                    // validates the session against the server.
                    // A cached token looks valid locally even if
                    // the account was disabled/deleted — only a
                    // server round-trip catches that.
                    var jwt = await _bridge.GetIdTokenAsync(true);
                    SetCurrent(BuildSession(jwt));
                }
                catch (Exception ex)
                {
                    // Token fetch failed — account deleted, token
                    // revoked, or network error during refresh.
                    // Treat as signed out so the router shows Login
                    // rather than leaving the app in limbo.
                    Console.Error.WriteLine(
                        $"[Auth] token fetch on state change failed: {ex.Message}");
                    SetCurrent(null);
                }
            });
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
        try
        {
            var jwt = await _bridge.SignInWithEmailAsync(email, password)
                .AsTask(ct);
            var session = BuildSession(jwt);
            SetCurrent(session);
            return session;
        }
        catch (Exception ex) when (ex is not OperationCanceledException)
        {
            throw ToAuthException(ex, "email-sign-in");
        }
    }

    public async Task<AuthSession> SignInWithGoogleAsync(
        CancellationToken ct = default)
    {
        var (idToken, accessToken) = await PerformGoogleOAuthAsync(ct);
        try
        {
            var jwt = await _bridge.SignInWithGoogleCredentialAsync(
                    idToken, accessToken)
                .AsTask(ct);
            var session = BuildSession(jwt);
            SetCurrent(session);
            return session;
        }
        catch (Exception ex) when (ex is not OperationCanceledException)
        {
            throw ToAuthException(ex, "google-credential");
        }
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

        try
        {
            var jwt = await _bridge.SignInWithGitHubCredentialAsync(accessToken)
                .AsTask(ct);
            var session = BuildSession(jwt);
            SetCurrent(session);
            return session;
        }
        catch (Exception ex) when (ex is not OperationCanceledException)
        {
            throw ToAuthException(ex, "github-credential");
        }
    }

    public async Task<AuthSession> SignInWithSsoAsync(
        string providerId, string loginHint, CancellationToken ct = default)
    {
        var (idToken, nonce) = await PerformSsoBrokerOAuthAsync(
            providerId, loginHint, ct);

        try
        {
            var jwt = await _bridge.SignInWithOidcCredentialAsync(
                    providerId, idToken, nonce)
                .AsTask(ct);
            var session = BuildSession(jwt);
            SetCurrent(session);
            return session;
        }
        catch (Exception ex) when (ex is not OperationCanceledException)
        {
            throw ToAuthException(ex, "oidc-credential");
        }
    }

    // ── account lifecycle ────────────────────────────────────────

    public async Task<AuthSession> CreateAccountAsync(
        string email, string password, string displayName,
        CancellationToken ct = default)
    {
        try
        {
            var jwt = await _bridge.CreateAccountAsync(
                    email, password, displayName)
                .AsTask(ct);
            var session = BuildSession(jwt);
            SetCurrent(session);
            return session;
        }
        catch (Exception ex) when (ex is not OperationCanceledException)
        {
            throw ToAuthException(ex, "create-account");
        }
    }

    public async Task SendPasswordResetAsync(
        string email, CancellationToken ct = default)
    {
        try
        {
            await _bridge.SendPasswordResetAsync(email).AsTask(ct);
        }
        catch (Exception ex) when (ex is not OperationCanceledException)
        {
            throw ToAuthException(ex, "password-reset");
        }
    }

    public Task SignOutAsync(CancellationToken ct = default)
    {
        // SignOut triggers AuthStateChanged → listener dispatches
        // SetCurrent(null) to UI thread. No explicit SetCurrent
        // here — Rule 14: pick one path.
        _bridge.SignOut();
        return Task.CompletedTask;
    }

    // ── SSO discovery ────────────────────────────────────────────

    public Task<string?> ResolveSsoProviderAsync(
        string email, CancellationToken ct = default)
        => SsoProviderResolver.ResolveAsync(email, ct);

    // ── token access ─────────────────────────────────────────────

    public async Task<string> GetIdTokenAsync(CancellationToken ct = default)
    {
        if (!_bridge.IsSignedIn)
            throw InternalAuthError("GetIdTokenAsync called while not signed in");

        return await _bridge.GetIdTokenAsync(false).AsTask(ct);
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

        var tokenResponse = await SharedHttp.Instance.PostAsync(
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
        var callbackFired = 0; // interlocked — ct callback is threadpool

        ct.Register(() =>
        {
            if (Interlocked.CompareExchange(ref callbackFired, 0, 0) == 0)
                window.DispatcherQueue.TryEnqueue(() => window.Close());
        });

        webView.NavigationStarting += (_, args) =>
        {
            var uri = args.Uri;
            if (uri is null || !uri.StartsWith(callbackScheme,
                    StringComparison.OrdinalIgnoreCase))
                return;

            args.Cancel = true;
            if (Interlocked.Exchange(ref callbackFired, 1) != 0) return;

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
            if (Interlocked.Exchange(ref callbackFired, 1) == 0)
                tcs.TrySetCanceled();
        };

        window.Content = webView;
        window.AppWindow.Resize(new Windows.Graphics.SizeInt32 { Width = 500, Height = 700 });
        window.Activate();
        SetPopupOwner(window);
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
        var callbackFired = 0;

        ct.Register(() =>
        {
            if (Interlocked.CompareExchange(ref callbackFired, 0, 0) == 0)
                window.DispatcherQueue.TryEnqueue(() => window.Close());
        });

        webView.NavigationStarting += (_, args) =>
        {
            var uri = args.Uri;
            if (uri is null || !uri.StartsWith(callbackScheme,
                    StringComparison.OrdinalIgnoreCase))
                return;

            args.Cancel = true;
            if (Interlocked.Exchange(ref callbackFired, 1) != 0) return;

            tcs.TrySetResult(uri);
            window.Close();
        };

        window.Closed += (_, _) =>
        {
            if (Interlocked.Exchange(ref callbackFired, 1) == 0)
                tcs.TrySetCanceled();
        };

        window.Content = webView;
        window.AppWindow.Resize(new Windows.Graphics.SizeInt32 { Width = 500, Height = 700 });
        window.Activate();
        SetPopupOwner(window);
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

    // ── popup window ownership ─────────────────────────────────

    private const int GWLP_HWNDPARENT = -8;

    [DllImport("user32.dll")]
    private static extern IntPtr GetForegroundWindow();

    [DllImport("user32.dll", EntryPoint = "SetWindowLongPtrW")]
    private static extern IntPtr SetWindowLongPtr(IntPtr hWnd, int nIndex, IntPtr dwNewLong);

    /// <summary>
    /// Sets <paramref name="popup"/> as owned by the current foreground
    /// window so it minimizes together, shares the taskbar group, and
    /// doesn't appear as a separate alt-tab entry.
    /// </summary>
    private static void SetPopupOwner(Window popup)
    {
        var popupHwnd = WinRT.Interop.WindowNative.GetWindowHandle(popup);
        var ownerHwnd = GetForegroundWindow();
        if (ownerHwnd != IntPtr.Zero && ownerHwnd != popupHwnd)
        {
            SetWindowLongPtr(popupHwnd, GWLP_HWNDPARENT, ownerHwnd);
        }
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

    // ── auth error translation ───────────────────────────────────
    // Two-tier: Firebase C++ AuthError (encoded in HRESULT by the
    // bridge) → canonical AuthErrorCode → user-facing string via
    // AuthErrorMessages.Get. Mirrors MacOsAuthService.ToAuthException.

    // FACILITY_ITF HRESULT range: 0x80040000–0x8004FFFF.
    private const int FacilityItfBase = unchecked((int)0x80040000);

    private static AuthException ToAuthException(Exception ex, string contextLabel)
    {
        var firebaseCode = ExtractFirebaseCode(ex);
        var code = MapToAuthCode(firebaseCode);

        var message = code == AuthErrorCode.Unknown
            ? (ex.Message ?? AuthErrorMessages.Get(AuthErrorCode.Unknown))
            : AuthErrorMessages.Get(code);

        Console.Error.WriteLine(
            $"[Auth] {contextLabel} failed: firebase={firebaseCode} → {code}, "
            + $"raw='{ex.Message}'");

        return new AuthException(code, message, ex);
    }

    private static AuthException InternalAuthError(string context)
    {
        Console.Error.WriteLine($"[Auth] internal: {context}");
        return new AuthException(
            AuthErrorCode.Unknown,
            AuthErrorMessages.Get(AuthErrorCode.Unknown));
    }

    private static int ExtractFirebaseCode(Exception ex)
    {
        var hr = ex.HResult;
        // Bridge encodes Firebase AuthError as FACILITY_ITF HRESULT:
        // 0x80040000 | (authError & 0xFFFF).
        if ((hr & unchecked((int)0xFFFF0000)) == FacilityItfBase)
            return hr & 0xFFFF;
        return -1; // unknown
    }

    // Firebase C++ SDK enum values from firebase/auth/types.h.
    private static AuthErrorCode MapToAuthCode(int firebaseCode) => firebaseCode switch
    {
        11 => AuthErrorCode.InvalidEmail,         // kAuthErrorInvalidEmail
        12 or 14 or 4                             // kAuthErrorWrongPassword, UserNotFound, InvalidCredential
            => AuthErrorCode.WrongPassword,
        8 => AuthErrorCode.EmailAlreadyInUse,     // kAuthErrorEmailAlreadyInUse
        6 or 10                                   // kAuthErrorAccountExistsWithDifferentCredentials, CredentialAlreadyInUse
            => AuthErrorCode.AccountExistsWithDifferentCredential,
        23 => AuthErrorCode.WeakPassword,         // kAuthErrorWeakPassword
        19 => AuthErrorCode.NetworkError,         // kAuthErrorNetworkRequestFailed
        13 => AuthErrorCode.TooManyRequests,      // kAuthErrorTooManyRequests
        7 => AuthErrorCode.OperationNotAllowed,   // kAuthErrorOperationNotAllowed
        5 => AuthErrorCode.UserDisabled,          // kAuthErrorUserDisabled
        _ => AuthErrorCode.Unknown,
    };
}
