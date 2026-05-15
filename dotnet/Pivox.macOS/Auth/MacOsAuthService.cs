using System.Net.Http;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Web;
using AppKit;
using AuthenticationServices;
using FirebaseAuth;
using FirebaseCore;
using Foundation;
using ObjCRuntime;
using Pivox.Shared.Auth;

namespace Pivox.Auth;

/// <summary>
/// macOS implementation of <see cref="IAuthService"/>, wrapping the
/// Firebase Cocoa SDK binding plus AppKit's <c>ASWebAuthenticationSession</c>
/// for OAuth providers. State (current user, refresh) lives in
/// FIRAuth — we just publish snapshots as <see cref="AuthSession"/>
/// values via <see cref="CurrentChanged"/>.
///
/// One instance per process. Construct after Firebase is configured
/// (or it configures lazily on first use).
/// </summary>
public sealed class MacOsAuthService : IAuthService
{
    // Hardcoded macOS OAuth client ID for Google. Same value the
    // SwiftUI Pivox app uses (see native/.../AuthService.swift). This
    // is a Google Cloud OAuth 2.0 client ID for native macOS, distinct
    // from the CLIENT_ID in GoogleService-Info.plist (Firebase's own).
    private const string GoogleClientID =
        "45920224787-gb662gbotfv763cqjis53748ctgigncl.apps.googleusercontent.com";

    private const string GoogleCallbackScheme =
        "com.googleusercontent.apps.45920224787-gb662gbotfv763cqjis53748ctgigncl";

    private const string GoogleRedirectUri = GoogleCallbackScheme + ":/oauth2callback";

    private static readonly HttpClient s_http = new();

    // Strong refs so neither survives only on the stack.
    private ASWebAuthenticationSession? _activeWebAuthSession;
    private AuthSession? _current;

    public MacOsAuthService()
    {
        if (FIRApp.DefaultApp is null)
        {
            FIRApp.Configure();
        }

        // FIRAuth restores the persisted user from Keychain synchronously
        // during Configure. The listener below fires once on registration
        // with the current state (Firebase's documented behavior) so it
        // catches both the launch-time restored session AND subsequent
        // changes — no separate restore-on-launch code needed.
        FIRAuth.Auth.AddAuthStateDidChangeListener((_, user) =>
        {
            if (user is null)
            {
                Console.Error.WriteLine("[Auth] state change: signed out");
                SetCurrent(null);
                return;
            }
            Console.Error.WriteLine($"[Auth] state change: uid={user.Uid}");
            user.GetIDTokenWithCompletion((tok, err) =>
            {
                if (err is not null)
                {
                    Console.Error.WriteLine($"[Auth] token fetch failed: {err.LocalizedDescription}");
                    return;
                }
                if (tok is not null)
                {
                    SetCurrent(BuildSession(tok));
                }
            });
        });
    }

    public AuthSession? Current => _current;
    public event EventHandler<AuthSession?>? CurrentChanged;

    public async Task<AuthSession> SignInWithEmailAsync(
        string email, string password, CancellationToken ct = default)
    {
        var tcs = new TaskCompletionSource<(FIRAuthDataResult?, NSError?)>();
        FIRAuth.Auth.SignInWithEmail(email, password, (r, e) => tcs.SetResult((r, e)));
        var (result, error) = await tcs.Task.WaitAsync(ct);
        return await FinalizeAsync(result, error, ct);
    }

    public async Task<AuthSession> SignInWithGoogleAsync(CancellationToken ct = default)
    {
        var (idToken, accessToken) = await PerformGoogleOAuthAsync(ct);

        var credential = FIRGoogleAuthProvider.CredentialWithIDToken(idToken, accessToken);
        var tcs = new TaskCompletionSource<(FIRAuthDataResult?, NSError?)>();
        FIRAuth.Auth.SignInWithCredential(credential, (r, e) => tcs.SetResult((r, e)));
        var (result, error) = await tcs.Task.WaitAsync(ct);
        return await FinalizeAsync(result, error, ct);
    }

    public Task SignOutAsync(CancellationToken ct = default)
    {
        FIRAuth.Auth.SignOut(out var error);
        if (error is not null)
        {
            throw new InvalidOperationException(
                $"FIRAuth.signOut failed: {error.LocalizedDescription} (code {error.Code})");
        }
        SetCurrent(null);
        return Task.CompletedTask;
    }

    public async Task<string> GetIdTokenAsync(CancellationToken ct = default)
    {
        var user = FIRAuth.Auth.CurrentUser
            ?? throw new InvalidOperationException("Not signed in.");

        // FIRAuth's getIDToken: auto-refreshes if the token is close to
        // expiry (default ~5 min). No need to second-guess it.
        var tcs = new TaskCompletionSource<(NSString?, NSError?)>();
        user.GetIDTokenWithCompletion((tok, err) => tcs.SetResult((tok, err)));
        var (token, error) = await tcs.Task.WaitAsync(ct);

        if (error is not null)
        {
            throw new InvalidOperationException(
                $"Token fetch failed: {error.LocalizedDescription} (code {error.Code})");
        }
        if (token is null)
        {
            throw new InvalidOperationException("Token fetch returned no token.");
        }

        // Update the cached snapshot — Firebase may have rotated the
        // token transparently (claims could change, expiry advanced).
        var session = BuildSession(token);
        SetCurrent(session);
        return session.IdToken;
    }

    // ───── helpers ───────────────────────────────────────────────

    private async Task<AuthSession> FinalizeAsync(
        FIRAuthDataResult? result, NSError? error, CancellationToken ct)
    {
        if (error is not null)
        {
            throw new InvalidOperationException(
                $"Firebase sign-in failed: {error.LocalizedDescription} (code {error.Code})");
        }
        if (result?.User is null)
        {
            throw new InvalidOperationException("Firebase returned no user.");
        }

        var tcs = new TaskCompletionSource<(NSString?, NSError?)>();
        result.User.GetIDTokenWithCompletion((tok, err) => tcs.SetResult((tok, err)));
        var (token, tokErr) = await tcs.Task.WaitAsync(ct);
        if (tokErr is not null)
        {
            throw new InvalidOperationException(
                $"Got user but ID token fetch failed: {tokErr.LocalizedDescription}");
        }
        if (token is null)
        {
            throw new InvalidOperationException("Got user but no ID token.");
        }

        var session = BuildSession(token);
        SetCurrent(session);
        return session;
    }

    private static AuthSession BuildSession(NSString idToken)
    {
        var jwt = idToken.ToString();
        return new AuthSession(jwt, new FirebaseIdentity(jwt));
    }

    private void SetCurrent(AuthSession? session)
    {
        // Dedupe: FIRAuth's AddAuthStateDidChangeListener fires for
        // every sign-in / sign-out / token rotation, AND we also call
        // SetCurrent explicitly from FinalizeAsync / SignOutAsync. The
        // listener path arrives slightly after the explicit path,
        // carrying an effectively-equal session (same JWT). Without
        // dedup, every sign-in/out fires CurrentChanged twice, which
        // makes the router rebuild windows twice.
        //
        // Equality: both null, or both non-null with identical IdToken
        // (FirebaseIdentity is derived from IdToken so JWT identity
        // implies semantic identity).
        if (_current is null && session is null) return;
        if (_current is not null && session is not null
            && _current.IdToken == session.IdToken)
        {
            // Same JWT → no semantic change. Suppress the duplicate.
            return;
        }

        _current = session;
        CurrentChanged?.Invoke(this, session);
    }

    // ───── Google OAuth (PKCE + ASWebAuthenticationSession) ─────

    private async Task<(string IdToken, string AccessToken)> PerformGoogleOAuthAsync(
        CancellationToken ct)
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

        var callbackUrl = await RunWebAuthSessionAsync(authUrl, GoogleCallbackScheme);

        var query = HttpUtility.ParseQueryString(new Uri(callbackUrl).Query);
        var code = query["code"]
            ?? throw new InvalidOperationException("No 'code' in Google OAuth callback URL.");

        var tokenResponse = await s_http.PostAsync(
            "https://oauth2.googleapis.com/token",
            new FormUrlEncodedContent(new Dictionary<string, string>
            {
                ["code"] = code,
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

    private Task<string> RunWebAuthSessionAsync(string authUrl, string callbackScheme)
    {
        var tcs = new TaskCompletionSource<string>();
        // The (NSUrl, string callbackUrlScheme, completionHandler)
        // overload is deprecated on macOS 14.4+; the replacement takes
        // an ASWebAuthenticationSessionCallback factory result.
        var schemeCallback = ASWebAuthenticationSessionCallback.Create(callbackScheme);
        _activeWebAuthSession = new ASWebAuthenticationSession(
            new NSUrl(authUrl),
            schemeCallback,
            (callbackUrl, error) =>
            {
                if (error is not null)
                {
                    tcs.SetException(new InvalidOperationException(
                        $"OAuth web session failed: {error.LocalizedDescription} (code {error.Code})"));
                }
                else if (callbackUrl is not null)
                {
                    tcs.SetResult(callbackUrl.AbsoluteString!);
                }
                else
                {
                    tcs.SetException(new InvalidOperationException("OAuth web session returned no URL."));
                }
                _activeWebAuthSession = null;
            });

        var anchor = NSApplication.SharedApplication.KeyWindow
            ?? NSApplication.SharedApplication.MainWindow
            ?? throw new InvalidOperationException("No window available to anchor the OAuth sheet.");
        _activeWebAuthSession.PresentationContextProvider = new PresentationContextProvider(anchor);
        _activeWebAuthSession.PrefersEphemeralWebBrowserSession = false;
        _activeWebAuthSession.Start();
        return tcs.Task;
    }

    private sealed class PresentationContextProvider
        : NSObject, IASWebAuthenticationPresentationContextProviding
    {
        private readonly NSWindow _window;
        public PresentationContextProvider(NSWindow window) { _window = window; }
        public NSWindow GetPresentationAnchor(ASWebAuthenticationSession session) => _window;
    }

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
        => Convert.ToBase64String(data).TrimEnd('=').Replace('+', '-').Replace('/', '_');
}
