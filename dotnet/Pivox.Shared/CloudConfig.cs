namespace Pivox.Shared;

/// <summary>
/// Single source of truth for the Pivox cloud gRPC endpoint.
///
/// Resolution order:
///   1. <c>PIVOX_GRPC_HOST</c> env var, e.g. "pivox.ngrok.app:443"
///      (host:port, NO scheme).
///   2. Default: <c>pivox.ngrok.app:443</c>. Matches the public
///      tunnel the OAuth broker uses, so gRPC + REST + auth all
///      point at the same backend.
///
/// Transport: TLS only. There is no plaintext deployment mode for
/// the dotnet stack — broker callbacks carry id tokens, gRPC carries
/// Firebase JWTs, neither can ever leave the device unencrypted.
///
/// Divergence from the SwiftUI <c>CloudConfig.swift</c>: the Swift
/// side retains a <c>PIVOX_GRPC_PLAINTEXT</c> escape hatch for local
/// development against a no-TLS gRPC server. The dotnet side
/// deliberately doesn't — that path was never exercised here, and
/// removing it shrinks the auth surface area. SwiftUI is on the
/// retirement track; the parity was a transitional convenience that
/// has expired.
/// </summary>
public static class CloudConfig
{
    /// <summary>"host:port" string, no scheme.</summary>
    public static string GrpcEndpoint
    {
        get
        {
            var env = Environment.GetEnvironmentVariable("PIVOX_GRPC_HOST");
            return !string.IsNullOrEmpty(env) ? env : "pivox.ngrok.app:443";
        }
    }

    /// <summary>
    /// Composes the endpoint into an https:// <see cref="Uri"/> for
    /// <c>GrpcChannel.ForAddress</c>. Always TLS.
    /// </summary>
    public static Uri GrpcUri
    {
        get
        {
            var (host, port) = ParsedEndpoint();
            return new Uri($"https://{host}:{port}");
        }
    }

    /// <summary>
    /// Base URL for pivox-cloud's REST surface: SSO provider resolution,
    /// the OAuth/OIDC broker, etc. Derived from <see cref="GrpcEndpoint"/>'s
    /// host so a single <c>PIVOX_GRPC_HOST</c> override flips both gRPC
    /// and REST. Always HTTPS.
    /// </summary>
    public static string BrokerBaseUrl
    {
        get
        {
            var (host, _) = ParsedEndpoint();
            return $"https://{host}";
        }
    }

    /// <summary>
    /// Parses <see cref="GrpcEndpoint"/> into (host, port). Throws on
    /// malformed values rather than silently picking a default — a
    /// typo here would otherwise misroute every RPC.
    /// </summary>
    public static (string Host, int Port) ParsedEndpoint()
    {
        var parts = GrpcEndpoint.Split(':', 2);
        if (parts.Length != 2 || !int.TryParse(parts[1], out var port))
        {
            throw new InvalidOperationException(
                $"Invalid PIVOX_GRPC_HOST: '{GrpcEndpoint}' (expected host:port).");
        }
        return (parts[0], port);
    }
}
