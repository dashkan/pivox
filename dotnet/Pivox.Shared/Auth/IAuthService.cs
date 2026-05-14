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

    Task<AuthSession> SignInWithEmailAsync(
        string email,
        string password,
        CancellationToken ct = default);

    Task<AuthSession> SignInWithGoogleAsync(CancellationToken ct = default);

    Task SignOutAsync(CancellationToken ct = default);

    /// <summary>
    /// Returns the current user's Firebase ID token. The underlying
    /// SDK auto-refreshes internally when the token is close to expiry
    /// (default ~5 min) — callers don't need to reason about staleness.
    /// Throws <see cref="InvalidOperationException"/> if not signed in.
    /// </summary>
    Task<string> GetIdTokenAsync(CancellationToken ct = default);
}
