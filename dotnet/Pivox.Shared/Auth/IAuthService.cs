namespace Pivox.Shared.Auth;

/// <summary>
/// The cross-platform auth surface. Each platform implements this against
/// its native Firebase SDK — macOS via the Firebase Cocoa SDK binding,
/// Windows via the Firebase C++ SDK behind a C++/WinRT projection — and
/// produces <see cref="AuthSession"/> values that everything upstream
/// consumes without knowing about Firebase.
///
/// Threading: implementations dispatch SDK calls to the appropriate
/// platform thread internally. Callers await on whatever thread they
/// like; events fire on the implementation's choice of context.
/// </summary>
public interface IAuthService
{
    /// <summary>Currently authenticated session, or null when signed out.</summary>
    AuthSession? Current { get; }

    /// <summary>Fires when <see cref="Current"/> changes (sign-in, sign-out,
    /// token refresh that updates the session).</summary>
    event EventHandler<AuthSession?>? CurrentChanged;

    // ───── primary sign-in paths ─────────────────────────────────

    Task<AuthSession> SignInWithEmailAsync(
        string email,
        string password,
        CancellationToken ct = default);

    Task<AuthSession> SignInWithGoogleAsync(CancellationToken ct = default);

    Task<AuthSession> SignInWithGitHubAsync(CancellationToken ct = default);

    /// <summary>
    /// Sign-in via the pivox-cloud OIDC broker for a SAML/OIDC enterprise
    /// provider (typically discovered via <see cref="ResolveSsoProviderAsync"/>).
    /// The broker is the authoritative client — clients never see the IdP's
    /// client_secret.
    /// </summary>
    /// <param name="providerId">Firebase OIDC provider id, e.g.
    /// <c>oidc.acme</c>. Returned by <see cref="ResolveSsoProviderAsync"/>.</param>
    /// <param name="loginHint">Email to pre-fill on the IdP login page,
    /// usually whatever the user typed in step 1.</param>
    Task<AuthSession> SignInWithSsoAsync(
        string providerId,
        string loginHint,
        CancellationToken ct = default);

    // ───── account lifecycle ─────────────────────────────────────

    /// <summary>
    /// Create a new email/password account and sign in. Sets the
    /// Firebase user's <c>displayName</c> in the same call.
    /// </summary>
    Task<AuthSession> CreateAccountAsync(
        string email,
        string password,
        string displayName,
        CancellationToken ct = default);

    /// <summary>
    /// Trigger Firebase's password-reset email. Doesn't return a
    /// confirmation beyond "the request was accepted" — the email
    /// arrives async.
    /// </summary>
    Task SendPasswordResetAsync(string email, CancellationToken ct = default);

    Task SignOutAsync(CancellationToken ct = default);

    // ───── SSO discovery ─────────────────────────────────────────

    /// <summary>
    /// Asks pivox-cloud whether the given email's domain is served by
    /// an enterprise SSO provider. Returns the Firebase provider id
    /// (e.g. <c>oidc.acme</c>) on hit, or null on miss.
    ///
    /// <para>404 from the backend (the existence-probe-defended
    /// response for "no provider configured") maps to null, not an
    /// error — callers surface a generic "couldn't sign in" rather
    /// than disclosing whether the domain is unknown vs. unconfigured.</para>
    ///
    /// <para>The endpoint is intentionally public (pre-auth clients
    /// need to call it before any token exists) — implementations
    /// hit pivox-cloud's <c>auth:resolveProvider</c> REST surface
    /// directly with no Authorization header.</para>
    /// </summary>
    Task<string?> ResolveSsoProviderAsync(
        string email, CancellationToken ct = default);

    // ───── token access ──────────────────────────────────────────

    /// <summary>
    /// Returns the current user's Firebase ID token. The underlying
    /// SDK auto-refreshes internally when the token is close to expiry
    /// (default ~5 min) — callers don't need to reason about staleness.
    /// Throws <see cref="InvalidOperationException"/> if not signed in.
    /// </summary>
    Task<string> GetIdTokenAsync(CancellationToken ct = default);
}
