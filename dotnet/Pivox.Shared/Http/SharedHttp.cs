using System.Net.Http;

namespace Pivox.Shared.Http;

/// <summary>
/// Process-wide <see cref="HttpClient"/>. Every cross-platform and
/// platform-specific consumer that issues plain HTTP requests
/// (OAuth token exchange, SSO provider resolution, etc.) should use
/// <see cref="Instance"/> — NOT construct its own.
///
/// Why a single instance: <see cref="HttpClient"/> is designed to be
/// reused. Repeated <c>new HttpClient()</c> creates new HTTP/2
/// connection pools per instance, exhausts ephemeral ports under
/// load, and bypasses DNS-refresh behavior that the pooled handler
/// provides. The canonical .NET pattern is one client per
/// destination type for the life of the app — for our scale (a
/// handful of REST calls against pivox-cloud + Google's token
/// endpoint), one instance covers everything.
///
/// <see cref="IHttpClientFactory"/> is the heavier-weight alternative
/// for apps with DI containers, log/metric instrumentation, and
/// per-call configuration. We deliberately don't run a DI container
/// (see dotnet/CLAUDE.md "Constructor pattern" rationale), so the
/// static singleton is the right shape.
///
/// Configuration: default <see cref="HttpClient.Timeout"/> (100 s)
/// is too generous for our use; we set 30 s here. Per-call
/// overrides via <see cref="HttpRequestMessage"/> + an explicit
/// <see cref="System.Threading.CancellationToken"/> are still
/// honored.
/// </summary>
public static class SharedHttp
{
    public static readonly HttpClient Instance = new()
    {
        Timeout = TimeSpan.FromSeconds(30),
    };
}
