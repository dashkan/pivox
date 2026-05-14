namespace Pivox.Shared;

/// <summary>
/// Single source of truth for the Pivox cloud gRPC endpoint and
/// transport security. Mirrors the SwiftUI app's CloudConfig.swift
/// — same env var names + semantics so one .envrc switches both
/// stacks pointing at the same backend.
///
/// Resolution order:
///   1. <c>PIVOX_GRPC_HOST</c> env var, e.g. "localhost:50051"
///      (host:port, NO scheme). <c>PIVOX_GRPC_PLAINTEXT=1</c>
///      disables TLS — for local dev against a plaintext server.
///   2. Default: <c>pivox.ngrok.app:443</c> over TLS. Matches the
///      public tunnel the OAuth broker uses, so gRPC + REST +
///      auth all point at the same backend.
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

    /// <summary>True if the gRPC channel should use plaintext (no TLS).
    /// ngrok terminates TLS on :443; local dev typically uses plaintext
    /// on :50051.</summary>
    public static bool UsePlaintext =>
        Environment.GetEnvironmentVariable("PIVOX_GRPC_PLAINTEXT") == "1";

    /// <summary>
    /// Composes the endpoint into a <see cref="Uri"/> for
    /// <c>GrpcChannel.ForAddress</c>. Scheme follows <see cref="UsePlaintext"/>.
    /// </summary>
    public static Uri GrpcUri
    {
        get
        {
            var (host, port) = ParsedEndpoint();
            var scheme = UsePlaintext ? "http" : "https";
            return new Uri($"{scheme}://{host}:{port}");
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
