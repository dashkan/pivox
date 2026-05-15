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
/// (Pivox.macOS on macOS, Pivox.WinUI on Windows) and passed wherever
/// gRPC access is needed.
///
/// Transport: TLS only. The dotnet stack has no plaintext gRPC mode —
/// see <see cref="CloudConfig"/> for the rationale.
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
        : this(CloudConfig.GrpcUri, auth)
    {
    }

    public PivoxClient(Uri endpoint, IAuthService auth)
    {
        var callCredentials = AuthCallCredentials.FromAuthService(auth);

        // TLS path — CallCredentials compose with SslCredentials so
        // gRPC attaches them automatically on every call.
        var options = new GrpcChannelOptions
        {
            Credentials = ChannelCredentials.Create(
                ChannelCredentials.SecureSsl,
                callCredentials),
        };

        _channel = GrpcChannel.ForAddress(endpoint, options);
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
    public global::Pivox.Ai.V1.AiChat.AiChatClient Ai
        => new(_channel);

    // Add more services here as features need them. Pivox.Client owns
    // every generated *Client class from pivox/**/*.proto.

    public void Dispose() => _channel.Dispose();
}
