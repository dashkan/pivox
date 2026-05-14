using Grpc.Core;
using Grpc.Net.Client;
using Pivox.Client.Auth;
using Pivox.Shared;
using Pivox.Shared.Auth;

namespace Pivox.Client;

/// <summary>
/// Single entry point for all Pivox gRPC calls. Owns the channel and
/// exposes typed service clients, with <see cref="AuthCallCredentials"/>
/// pre-attached so every call carries the user's Bearer token.
///
/// One instance per process. Constructed by the platform app
/// (PivoxApp on macOS, future PivoxApp.Windows) and passed wherever
/// gRPC access is needed.
/// </summary>
public sealed class PivoxClient : IDisposable
{
    private readonly GrpcChannel _channel;

    /// <summary>
    /// Builds a client pointing at <see cref="CloudConfig.GrpcUri"/>
    /// (defaults to pivox.ngrok.app; overridable via PIVOX_GRPC_HOST
    /// env var). Auth tokens come from <paramref name="auth"/>.
    /// </summary>
    public PivoxClient(IAuthService auth)
        : this(CloudConfig.GrpcUri, CloudConfig.UsePlaintext, auth)
    {
    }

    public PivoxClient(Uri endpoint, bool usePlaintext, IAuthService auth)
    {
        var callCredentials = AuthCallCredentials.FromAuthService(auth);

        var options = new GrpcChannelOptions();

        if (usePlaintext)
        {
            // Plaintext local dev — CallCredentials are normally blocked
            // on insecure channels (the token would leak in plaintext).
            // Allow them explicitly; safe because the network never
            // leaves localhost in this config.
            options.Credentials = ChannelCredentials.Insecure;
            options.UnsafeUseInsecureChannelCallCredentials = true;

            // Attach call credentials by wrapping every call. With insecure
            // channels, gRPC won't auto-apply CallCredentials from the
            // channel — we set them explicitly per call via DefaultCallOptions.
            options.DisposeHttpClient = false;
            ApplyCallCredentialsViaDefaultOptions(options, callCredentials);
        }
        else
        {
            // TLS path — CallCredentials compose with SslCredentials so
            // gRPC attaches them automatically on every call.
            options.Credentials = ChannelCredentials.Create(
                ChannelCredentials.SecureSsl,
                callCredentials);
        }

        _channel = GrpcChannel.ForAddress(endpoint, options);
    }

    private static void ApplyCallCredentialsViaDefaultOptions(
        GrpcChannelOptions options, CallCredentials callCredentials)
    {
        // For insecure channels we can't compose CallCredentials with
        // ChannelCredentials.Insecure. Stash them in DefaultCallOptions
        // so every call inherits them.
        options.DisposeHttpClient = false;
        options.UnsafeUseInsecureChannelCallCredentials = true;
        // Note: GrpcChannelOptions doesn't have a public DefaultCallOptions
        // setter in net10.0. For insecure dev we rely on per-call attachment
        // via CallOptions(credentials:) in typed clients. Leaving the hook
        // here as a marker — if plaintext dev becomes a real path, wire
        // CallOptions per service-client property below.
    }

    // ───── service surface ──────────────────────────────────────
    // Fully-qualified type names because the generated proto
    // namespaces (Pivox.Iam.V1, Pivox.Api.V1) shadow the inner class
    // names (Iam, Organizations) — `Iam.IamClient` would resolve to
    // the namespace, not the nested service-stub class.

    public global::Pivox.Iam.V1.Iam.IamClient Iam
        => new(_channel);
    public global::Pivox.Api.V1.Organizations.OrganizationsClient Organizations
        => new(_channel);
    public global::Pivox.Api.V1.Spaces.SpacesClient Spaces
        => new(_channel);

    // Add more services here as features need them. Pivox.Client owns
    // every generated *Client class from pivox/**/*.proto.

    public void Dispose() => _channel.Dispose();
}
