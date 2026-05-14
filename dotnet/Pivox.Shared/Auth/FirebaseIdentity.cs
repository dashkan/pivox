using System.Security.Claims;
using Microsoft.IdentityModel.JsonWebTokens;

namespace Pivox.Shared.Auth;

/// <summary>
/// A <see cref="ClaimsIdentity"/> constructed from a Firebase-signed JWT.
/// Encapsulates Firebase's specific claim names and exposes the claims
/// we actually consume as strongly-typed properties.
///
/// We trust the signature — Firebase signed the token server-side and
/// we received it over TLS. No validation pipeline here; this class
/// reads claims from a token we already trust.
///
/// Authorization (org/space role memberships) is NOT in the JWT. That
/// data lives in pivox-cloud and is fetched via <c>iam.v1.*</c> RPCs.
/// Do not add role/membership claims to this identity; do not call
/// <c>principal.IsInRole(...)</c>. JWT carries identity only.
/// </summary>
public sealed class FirebaseIdentity : ClaimsIdentity
{
    public FirebaseIdentity(string jwt)
        : base(
            new JsonWebToken(jwt).Claims,
            authenticationType: "Firebase",
            // Firebase uses OIDC-standard claim names. "name" is the
            // display-name claim (populated for social-provider sign-ins;
            // may be null for raw email/password without displayName).
            nameType: "name",
            // Roles intentionally NOT mapped — Pivox RBAC is server-side.
            roleType: null)
    {
        var token = new JsonWebToken(jwt);
        IdToken = jwt;
        ExpiresAt = token.ValidTo;
    }

    /// <summary>The raw JWT — for outbound Bearer auth on gRPC calls.</summary>
    public string IdToken { get; }

    /// <summary>Token expiry from the standard <c>exp</c> claim.</summary>
    public DateTimeOffset ExpiresAt { get; }

    /// <summary>
    /// Pivox user ID, set by the Cloud Functions <c>beforeSignIn</c>
    /// blocking trigger on every Firebase sign-in. Throws if missing —
    /// every JWT we accept must have it.
    /// </summary>
    public string PivoxUserId =>
        FindFirst("pivox_user_id")?.Value
        ?? throw new InvalidOperationException(
            "JWT missing 'pivox_user_id' claim — the Cloud Functions " +
            "beforeSignIn blocking trigger should have set it.");

    /// <summary>Firebase UID (standard <c>sub</c> claim).</summary>
    public string FirebaseUid =>
        FindFirst("sub")?.Value
        ?? throw new InvalidOperationException("JWT missing 'sub' claim.");

    /// <summary>
    /// Display name. Populated from social-provider sign-ins; null for
    /// raw email/password users who haven't set a displayName.
    /// Equivalent to <see cref="ClaimsIdentity.Name"/> since
    /// <c>NameClaimType</c> is "name".
    /// </summary>
    public string? DisplayName => Name;

    /// <summary>Email address. Null in unusual provider configurations.</summary>
    public string? Email => FindFirst("email")?.Value;

    /// <summary>Avatar URL — populated by social providers, null otherwise.</summary>
    public string? PictureUrl => FindFirst("picture")?.Value;
}
