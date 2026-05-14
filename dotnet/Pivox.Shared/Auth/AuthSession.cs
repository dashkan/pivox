namespace Pivox.Shared.Auth;

/// <summary>
/// A point-in-time snapshot of an authenticated session. Platform-neutral
/// — produced by platform-specific <see cref="IAuthService"/> implementations
/// (Firebase Cocoa SDK on macOS, Firebase C++ SDK via WinRT on Windows)
/// and consumed by everything above (gRPC interceptors, view models, etc.).
///
/// Deliberately does NOT carry refresh tokens or any other SDK-internal
/// state. Refresh is the auth service's responsibility — callers ask for
/// a fresh token via <see cref="IAuthService.GetFreshIdTokenAsync"/>
/// rather than holding refresh material themselves.
/// </summary>
public sealed record AuthSession(
    string IdToken,
    string PivoxUserId,
    string? Email,
    DateTimeOffset ExpiresAt
);
