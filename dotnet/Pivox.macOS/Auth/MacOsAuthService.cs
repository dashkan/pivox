using System.Net.Http;
using System.Net.Http.Headers;
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
using Pivox.Shared;
using Pivox.Shared.Auth;
using Pivox.Shared.Http;

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

    // Broker callback scheme + return URL shared by GitHub OAuth and
    // SSO/OIDC. The broker (pivox-cloud) handles the third-party
    // OAuth dance server-side and redirects to this scheme with the
    // credential carried in the URL fragment. The scheme is just an
    // intercept token for ASWebAuthenticationSession — not a system
    // URL handler registration.
    private const string BrokerCallbackScheme = "pivox";
    private const string BrokerReturnUrl = "pivox://auth-complete";

    // Strong refs so neither survives only on the stack.
    // `_activeWebAuthSession` is volatile because the completion
    // callback (main thread per AppKit) and the caller-side write in
    // RunWebAuthSessionAsync may run on different threads if a future
    // entrypoint kicks an OAuth flow from a background thread (gRPC
    // retry interceptor, future reauth-on-401 path). Volatile makes
    // the strong-ref write/clear visible across threads.
    private volatile ASWebAuthenticationSession? _activeWebAuthSession;
    private AuthSession? _current;

    // UI thread captured at construction. SetCurrent marshals
    // CurrentChanged through this context so subscribers always run
    // on the UI thread regardless of which thread mutated state —
    // FIRAuth callbacks (main per Cocoa SDK contract) AND gRPC
    // CallCredentials async-token paths (threadpool) both route
    // through SetCurrent. Per dotnet/CLAUDE.md Rule 12 — the service
    // is the right marshal boundary; AppRouter shouldn't be the only
    // place this is enforced.
    private readonly SynchronizationContext _uiContext;

    // Set by explicit flows (CreateAccountAsync) that need to suppress
    // the FIRAuth listener so it doesn't race FinalizeAsync — see
    // SetListenerCallback for the suppression check. Volatile because
    // the listener fires on FIRAuth's queue.
    private volatile bool _suppressListenerSetCurrent;

    public MacOsAuthService()
    {
        // Capture the UI thread's SynchronizationContext for marshaling
        // CurrentChanged. AppDelegate.DidFinishLaunching constructs this
        // service on the AppKit main thread, where .NET-for-macOS has
        // installed a context that posts back to the main run loop.
        _uiContext = SynchronizationContext.Current
            ?? throw new InvalidOperationException(
                "MacOsAuthService must be constructed on a thread with a UI "
                + "SynchronizationContext (the AppKit main thread).");

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
            // Explicit flows that fetch a fresh JWT after a profile
            // mutation (CreateAccountAsync sets DisplayName, then
            // reloads — the post-reload JWT carries the new `name`
            // claim and differs from the JWT the listener captured
            // before the mutation). Without suppression the listener
            // path fires with the stale JWT, AppRouter swaps to
            // Shell, then FinalizeAsync fires with the fresh JWT and
            // swaps Shell to Shell — two window builds.
            if (_suppressListenerSetCurrent) return;

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

    public async Task<AuthSession> SignInWithGitHubAsync(CancellationToken ct = default)
    {
        // GitHub OAuth runs through the pivox-cloud broker (not directly
        // against GitHub) so the client_secret stays server-side.
        var accessToken = await PerformBrokerOAuthAsync(
            providerSlug: "github",
            expectedProvider: "github",
            expectedKind: "github_access_token",
            tokenFieldName: "token",
            loginHint: null,
            ct: ct);

        var credential = FIRGitHubAuthProvider.CredentialWithToken(accessToken);
        var tcs = new TaskCompletionSource<(FIRAuthDataResult?, NSError?)>();
        FIRAuth.Auth.SignInWithCredential(credential, (r, e) => tcs.SetResult((r, e)));
        var (result, error) = await tcs.Task.WaitAsync(ct);
        return await FinalizeAsync(result, error, ct);
    }

    public async Task<AuthSession> SignInWithSsoAsync(
        string providerId, string loginHint, CancellationToken ct = default)
    {
        // OIDC broker flow: id_token + rawNonce come back via the
        // fragment; build an OAuthProvider credential from them.
        var (idToken, nonce) = await PerformSsoBrokerOAuthAsync(
            providerId, loginHint, ct);

        // FIROAuthProvider.credentialWithProviderID:IDToken:rawNonce: —
        // the 3-arg overload (no accessToken) is what the OIDC broker
        // shape needs. The credential factory ties the id_token to the
        // raw nonce so Firebase verifies the JWT's nonce claim matches
        // what the broker bound into the authorize request.
        var credential = FIROAuthProvider.CredentialWithProviderID(
            providerId, idToken, nonce);
        var tcs = new TaskCompletionSource<(FIRAuthDataResult?, NSError?)>();
        FIRAuth.Auth.SignInWithCredential(credential, (r, e) => tcs.SetResult((r, e)));
        var (result, error) = await tcs.Task.WaitAsync(ct);
        return await FinalizeAsync(result, error, ct);
    }

    public async Task<AuthSession> CreateAccountAsync(
        string email, string password, string displayName, CancellationToken ct = default)
    {
        // Suppress the FIRAuth listener for the duration of this
        // multi-step flow. CreateUserWithEmail triggers the listener
        // with a token that doesn't yet have the `name` claim; we
        // then commit ProfileChangeRequest + Reload, after which the
        // explicit GetIDToken returns a JWT with the new claim. The
        // two JWTs differ, so dedup wouldn't suppress the
        // listener's stale event — and we'd see two CurrentChanged
        // events (stale → fresh) and two route transitions. The flag
        // makes the listener no-op during the window; FinalizeAsync's
        // explicit SetCurrent is the only event the consumer sees.
        _suppressListenerSetCurrent = true;
        try
        {
            return await CreateAccountInnerAsync(email, password, displayName, ct);
        }
        finally
        {
            _suppressListenerSetCurrent = false;
        }
    }

    private async Task<AuthSession> CreateAccountInnerAsync(
        string email, string password, string displayName, CancellationToken ct)
    {
        // Step 1: create the Firebase user (email/password).
        var createTcs = new TaskCompletionSource<(FIRAuthDataResult?, NSError?)>();
        FIRAuth.Auth.CreateUserWithEmail(email, password, (r, e) => createTcs.SetResult((r, e)));
        var (result, createError) = await createTcs.Task.WaitAsync(ct);

        if (createError is not null)
        {
            throw new InvalidOperationException(
                $"Firebase createUser failed: {createError.LocalizedDescription} (code {createError.Code})");
        }
        if (result?.User is null)
        {
            throw new InvalidOperationException("Firebase createUser returned no user.");
        }

        // Step 2: set the display name on the freshly-created user.
        // Mirrors SwiftUI's flow — Firebase's createUser doesn't take
        // displayName as a param, it's a follow-up profile-change call.
        //
        // The ObjC selector is `-profileChangeRequest` (a method that
        // returns a fresh `FIRUserProfileChangeRequest`); the sharpie
        // binding flattens it to a C# property of the same name. Each
        // read invokes the method and returns a NEW request object —
        // safe to grab once, mutate, and commit. Don't cache across
        // calls.
        var changeRequest = result.User.ProfileChangeRequest;
        changeRequest.DisplayName = displayName;
        var commitTcs = new TaskCompletionSource<NSError?>();
        changeRequest.CommitChangesWithCompletion(err => commitTcs.SetResult(err));
        var commitError = await commitTcs.Task.WaitAsync(ct);
        if (commitError is not null)
        {
            // Profile-change failure is non-fatal — the account exists
            // and is signed in. Surface the error in the log but keep
            // the session.
            Console.Error.WriteLine(
                $"[Auth] displayName commit failed: {commitError.LocalizedDescription}");
        }
        else
        {
            // Reload to pick up the new displayName on the User object.
            var reloadTcs = new TaskCompletionSource<NSError?>();
            result.User.ReloadWithCompletion(err => reloadTcs.SetResult(err));
            await reloadTcs.Task.WaitAsync(ct);
        }

        return await FinalizeAsync(result, null, ct);
    }

    public async Task SendPasswordResetAsync(string email, CancellationToken ct = default)
    {
        var tcs = new TaskCompletionSource<NSError?>();
        FIRAuth.Auth.SendPasswordResetWithEmail(email, err => tcs.SetResult(err));
        var error = await tcs.Task.WaitAsync(ct);
        if (error is not null)
        {
            throw new InvalidOperationException(
                $"Firebase sendPasswordReset failed: {error.LocalizedDescription} (code {error.Code})");
        }
    }

    public Task<string?> ResolveSsoProviderAsync(
        string email, CancellationToken ct = default)
        => SsoProviderResolver.ResolveAsync(email, ct);

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
        // Marshal to the UI thread before dedup + raise. Callers
        // include FIRAuth's listener (main per Cocoa contract, but
        // safer to assume nothing) and gRPC CallCredentials'
        // GetIdTokenAsync continuation (threadpool). Subscribers
        // (AppDelegate → AppRouter) should always run on the UI
        // thread, regardless of which path triggered this.
        if (SynchronizationContext.Current == _uiContext)
        {
            ApplySetCurrent(session);
        }
        else
        {
            _uiContext.Post(static (state) =>
            {
                var (self, s) = ((MacOsAuthService, AuthSession?))state!;
                self.ApplySetCurrent(s);
            }, (this, session));
        }
    }

    private void ApplySetCurrent(AuthSession? session)
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

        var tokenResponse = await SharedHttp.Instance.PostAsync(
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

    // ───── Broker OAuth (GitHub, SSO) ──────────────────────────

    /// <summary>
    /// Runs the pivox-cloud broker OAuth flow for the given provider
    /// slug (e.g. "github"). The broker handles the upstream OAuth
    /// dance server-side (client_secret never reaches the client) and
    /// redirects back to <see cref="BrokerCallbackScheme"/> with the
    /// credential in the URL fragment. Returns the requested token
    /// field value (e.g. the access token for GitHub).
    /// </summary>
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
        {
            startUrl += $"&login_hint={Uri.EscapeDataString(loginHint.Trim())}";
        }

        var callbackUrl = await RunWebAuthSessionAsync(startUrl, BrokerCallbackScheme);

        var items = ParseFragment(new Uri(callbackUrl));

        if (items.TryGetValue("error", out var errCode))
        {
            var desc = items.TryGetValue("error_description", out var d) ? d : errCode;
            throw new InvalidOperationException(
                $"Broker sign-in failed: {desc}");
        }
        if (!items.TryGetValue("provider", out var provider) || provider != expectedProvider)
        {
            throw new InvalidOperationException(
                $"Broker returned wrong provider (got '{provider}', expected '{expectedProvider}').");
        }
        if (!items.TryGetValue("kind", out var kind) || kind != expectedKind)
        {
            throw new InvalidOperationException(
                $"Broker returned unexpected credential kind '{kind}'.");
        }
        if (!items.TryGetValue(tokenFieldName, out var token) || string.IsNullOrEmpty(token))
        {
            throw new InvalidOperationException(
                $"Broker callback missing '{tokenFieldName}'.");
        }
        return token;
    }

    /// <summary>
    /// SSO/OIDC variant: returns (id_token, raw_nonce). Firebase
    /// requires both to verify the id_token's nonce claim against the
    /// one the broker bound into the authorize request.
    /// </summary>
    private async Task<(string IdToken, string Nonce)> PerformSsoBrokerOAuthAsync(
        string providerId, string loginHint, CancellationToken ct)
    {
        var encodedReturn = Uri.EscapeDataString(BrokerReturnUrl);
        var startUrl =
            $"{CloudConfig.BrokerBaseUrl}/internal/v1/auth/{Uri.EscapeDataString(providerId)}/start"
            + $"?return={encodedReturn}";
        if (!string.IsNullOrWhiteSpace(loginHint))
        {
            startUrl += $"&login_hint={Uri.EscapeDataString(loginHint.Trim())}";
        }

        var callbackUrl = await RunWebAuthSessionAsync(startUrl, BrokerCallbackScheme);
        var items = ParseFragment(new Uri(callbackUrl));

        if (items.TryGetValue("error", out var errCode))
        {
            var desc = items.TryGetValue("error_description", out var d) ? d : errCode;
            throw new InvalidOperationException(
                $"SSO sign-in failed: {desc}");
        }
        if (!items.TryGetValue("provider", out var provider) || provider != providerId)
        {
            throw new InvalidOperationException(
                $"Broker returned wrong provider (got '{provider}', expected '{providerId}').");
        }
        if (!items.TryGetValue("kind", out var kind) || kind != "oidc_id_token")
        {
            throw new InvalidOperationException(
                $"Broker returned unexpected credential kind '{kind}'.");
        }
        if (!items.TryGetValue("token", out var idToken) || string.IsNullOrEmpty(idToken))
        {
            throw new InvalidOperationException("Broker callback missing id_token.");
        }
        if (!items.TryGetValue("nonce", out var nonce) || string.IsNullOrEmpty(nonce))
        {
            throw new InvalidOperationException("Broker callback missing nonce.");
        }
        return (idToken, nonce);
    }

    /// <summary>Parses the fragment of a callback URL into a flat
    /// dictionary. The broker carries credentials in the fragment
    /// (per OAuth implicit-style redirects) — the query is unused.</summary>
    private static Dictionary<string, string> ParseFragment(Uri callback)
    {
        var fragment = callback.Fragment.TrimStart('#');
        // ParseQueryString handles URL-encoded key=value&key=value.
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
                    // User-cancellation (closing the web sheet) shows
                    // up as ASWebAuthenticationSessionErrorDomain /
                    // code 1 (CanceledLogin). Mirrors the SwiftUI
                    // reference: don't surface as an error — return
                    // a cancellation so VM-side catch (OperationCanceledException)
                    // silently no-ops, leaving the form in its prior
                    // state for the user to retry.
                    if (error.Domain == "ASWebAuthenticationSessionErrorDomain"
                        && error.Code == 1)
                    {
                        tcs.SetCanceled();
                    }
                    else
                    {
                        tcs.SetException(new InvalidOperationException(
                            $"OAuth web session failed: {error.LocalizedDescription} (code {error.Code})"));
                    }
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
