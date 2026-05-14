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
    /// Returns a JWT that is guaranteed valid for at least
    /// <paramref name="staleWindow"/> from now. Refreshes the underlying
    /// Firebase token if the current one would expire within that window.
    /// Throws <see cref="InvalidOperationException"/> if not signed in.
    ///
    /// Designed for the gRPC interceptor: pass e.g. 60s as the stale
    /// window so the token doesn't expire mid-call.
    /// </summary>
    Task<string> GetFreshIdTokenAsync(
        TimeSpan staleWindow,
        CancellationToken ct = default);
}
