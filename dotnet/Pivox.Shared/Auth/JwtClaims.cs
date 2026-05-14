using System.Text;
using System.Text.Json;

namespace Pivox.Shared.Auth;

/// <summary>
/// Tiny JWT payload reader. We don't validate the signature here —
/// Firebase already signed and validated the token end-to-end before
/// handing it to us. We just need to read the custom claims (in
/// particular <c>pivox_user_id</c>, set by the Cloud Functions
/// beforeSignIn blocking trigger).
///
/// Stays in shared land so both platform implementations decode the
/// same way; no need for a heavyweight JWT NuGet package.
/// </summary>
public static class JwtClaims
{
    /// <summary>
    /// Reads a base64url-encoded JWT payload segment and returns its
    /// JSON document. Caller disposes.
    /// </summary>
    public static JsonDocument ParsePayload(string jwt)
    {
        var parts = jwt.Split('.');
        if (parts.Length < 2)
        {
            throw new ArgumentException("Not a JWT (missing payload segment).", nameof(jwt));
        }
        var payloadJson = Encoding.UTF8.GetString(Base64UrlDecode(parts[1]));
        return JsonDocument.Parse(payloadJson);
    }

    /// <summary>
    /// Extracts the Pivox user ID from the JWT's <c>pivox_user_id</c>
    /// claim. Throws if the claim is missing or empty.
    /// </summary>
    public static string ExtractPivoxUserId(string jwt)
    {
        using var doc = ParsePayload(jwt);
        if (!doc.RootElement.TryGetProperty("pivox_user_id", out var claim)
            || claim.ValueKind != JsonValueKind.String
            || string.IsNullOrEmpty(claim.GetString()))
        {
            throw new InvalidOperationException(
                "JWT is missing the 'pivox_user_id' claim. The Cloud Functions " +
                "beforeSignIn blocking trigger should have set it.");
        }
        return claim.GetString()!;
    }

    /// <summary>
    /// Returns the token's expiry as a <see cref="DateTimeOffset"/>.
    /// Reads the standard <c>exp</c> claim (seconds since Unix epoch).
    /// </summary>
    public static DateTimeOffset ExtractExpiresAt(string jwt)
    {
        using var doc = ParsePayload(jwt);
        if (!doc.RootElement.TryGetProperty("exp", out var expClaim)
            || expClaim.ValueKind != JsonValueKind.Number)
        {
            throw new InvalidOperationException("JWT missing 'exp' claim.");
        }
        return DateTimeOffset.FromUnixTimeSeconds(expClaim.GetInt64());
    }

    private static byte[] Base64UrlDecode(string s)
    {
        // JWT segments are base64url-encoded without padding; pad and
        // swap chars to standard base64 before decoding.
        var padded = s.Replace('-', '+').Replace('_', '/');
        padded = padded.PadRight(padded.Length + (4 - padded.Length % 4) % 4, '=');
        return Convert.FromBase64String(padded);
    }
}
