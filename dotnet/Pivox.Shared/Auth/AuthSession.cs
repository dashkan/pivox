using System.Security.Claims;

namespace Pivox.Shared.Auth;

/// <summary>
/// A point-in-time snapshot of an authenticated session. Platform-neutral —
/// produced by platform-specific <see cref="IAuthService"/> implementations
/// (Firebase Cocoa SDK on macOS, Firebase C++ SDK via WinRT on Windows) —
/// and consumed by everything above (gRPC interceptors, view models, etc.).
///
/// Carries the parsed <see cref="FirebaseIdentity"/> alongside the raw
/// JWT. Convenience accessors (<see cref="PivoxUserId"/>,
/// <see cref="DisplayName"/>, etc.) forward to the identity for the
/// claims consumers read most often.
///
/// Does NOT carry authorization state (org/space role memberships).
/// That data lives in pivox-cloud and is fetched via <c>iam.v1.*</c>
/// RPCs — a separate concern from authentication.
/// </summary>
public sealed record AuthSession(string IdToken, FirebaseIdentity Identity)
{
    /// <summary>
    /// Standard .NET principal wrapping the Firebase identity.
    /// Use this anywhere a <see cref="ClaimsPrincipal"/> is expected.
    /// </summary>
    public ClaimsPrincipal Principal => new(Identity);

    /// <summary>Token expiry — for refresh staleness decisions.</summary>
    public DateTimeOffset ExpiresAt => Identity.ExpiresAt;

    public string PivoxUserId => Identity.PivoxUserId;
    public string FirebaseUid => Identity.FirebaseUid;
    public string? DisplayName => Identity.DisplayName;
    public string? Email => Identity.Email;
    public string? PictureUrl => Identity.PictureUrl;
}
