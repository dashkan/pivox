using System.Net.Http;
using System.Text;
using System.Text.Json;
using Pivox.Shared.Http;

namespace Pivox.Shared.Auth;

/// <summary>
/// Cross-platform SSO provider resolver. Calls pivox-cloud's
/// <c>internal/v1/auth:resolveProvider</c> REST endpoint with an
/// email address; returns the configured OIDC provider id for the
/// email's domain, or <c>null</c> when no SSO provider applies
/// (the email's domain isn't bound to one, or the broker's
/// configuration deliberately collapses three failure modes —
/// domain unknown, domain not verified, SsoConfig disabled — into a
/// single 404 to avoid existence-probing).
///
/// Shared by both <c>MacOsAuthService.ResolveSsoProviderAsync</c>
/// and <c>WindowsAuthService.ResolveSsoProviderAsync</c> — the HTTP
/// surface has no platform APIs in it, so duplication served no
/// purpose. Per-platform <see cref="IAuthService"/> implementations
/// remain the public surface (they own the rest of auth) but
/// delegate this single method here.
///
/// Pre-auth surface: no <c>Authorization</c> header. The endpoint is
/// designed to be reachable before the user has a JWT.
/// </summary>
public static class SsoProviderResolver
{
    /// <summary>Resolve the SSO provider for an email address. Returns
    /// the provider id (e.g. <c>"oidc.acme"</c>) on success, or
    /// <c>null</c> when no SSO is configured for the email's domain.
    /// Empty / whitespace input also returns <c>null</c>.</summary>
    /// <exception cref="InvalidOperationException">Server returned a
    /// non-200, non-404 status. The HTTP code is included in the
    /// message; the body is not echoed (the body may carry
    /// diagnostic detail useful only in server logs).</exception>
    public static async Task<string?> ResolveAsync(
        string email, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(email);
        var trimmed = email.Trim();
        if (string.IsNullOrEmpty(trimmed)) return null;

        // Hand-build the JSON body via JavaScriptEncoder rather than
        // JsonSerializer + anonymous type — JsonSerializer.Serialize<T>
        // on an unannotated type triggers IL2026 / IL3050 under AOT
        // because the serializer would need reflection. The request
        // shape is one field; the source-gen ceremony isn't worth it.
        var url = $"{CloudConfig.BrokerBaseUrl}/internal/v1/auth:resolveProvider";
        var encoded = System.Text.Encodings.Web.JavaScriptEncoder.Default
            .Encode(trimmed);
        var body = $"{{\"email\":\"{encoded}\"}}";
        using var req = new HttpRequestMessage(HttpMethod.Post, url)
        {
            Content = new StringContent(body, Encoding.UTF8, "application/json"),
        };

        using var resp = await SharedHttp.Instance.SendAsync(req, ct);
        switch ((int)resp.StatusCode)
        {
            case 200:
                var payload = await resp.Content.ReadAsStringAsync(ct);
                using (var doc = JsonDocument.Parse(payload))
                {
                    return doc.RootElement.TryGetProperty("provider_id", out var p)
                        ? p.GetString()
                        : null;
                }
            case 404:
                // "No provider configured" — collapsed with "domain
                // unknown" and "SsoConfig disabled" to avoid
                // existence-probing.
                return null;
            default:
                throw new InvalidOperationException(
                    $"resolveProvider failed: HTTP {(int)resp.StatusCode}");
        }
    }
}
