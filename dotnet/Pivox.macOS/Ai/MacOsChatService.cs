using System.Runtime.CompilerServices;
using Grpc.Core;
using Pivox.Client;
using Pivox.Shared.Ai;

namespace Pivox.Ai;

/// <summary>
/// macOS implementation of <see cref="IChatService"/>. Thin adapter
/// over <see cref="PivoxClient.Ai"/>'s generated
/// <c>AiChat.AiChatClient</c>: builds the proto request, calls
/// <c>StreamGenerateContent</c>, maps each
/// <c>ServerEvent</c> oneof case to a domain
/// <see cref="ChatStreamEvent"/>, and translates RpcException +
/// cancellation into <see cref="ChatException"/>.
///
/// Phase B scope: text-track events only. Reasoning, tool-call,
/// tool-output, and artifact events are consumed and dropped — the
/// stream isn't surfaced to the UI yet, and dropping events keeps the
/// stream advancing without producing spurious UI state. Phase C/D add
/// the corresponding event surfaces as those features get UI.
///
/// Layering: lives in <c>Pivox.macOS</c> (not <c>Pivox.Shared</c>)
/// because the proto types (<c>Pivox.Ai.V1.*</c>) are generated into
/// <c>Pivox.Client</c>, which <c>Pivox.Shared</c> doesn't depend on.
/// Conversion at this seam keeps the cross-platform layer free of
/// gRPC concepts.
/// </summary>
public sealed class MacOsChatService : IChatService
{
    private readonly PivoxClient _client;

    public MacOsChatService(PivoxClient client)
    {
        ArgumentNullException.ThrowIfNull(client);
        _client = client;
        AssertRoleAlignment();
    }

    /// <summary>One-time runtime check that the proto
    /// <c>pivox.ai.v1.Role</c> enum values match
    /// <see cref="MessageRole"/> — we rely on numeric alignment for
    /// the <c>(Role)(int)</c> cast in <see cref="BuildRequest"/>.
    /// If proto regenerates with a different numbering, this throws
    /// on first use rather than silently miscategorizing every turn.
    /// Cheap (runs once per service instance, ~6 enum comparisons).</summary>
    private static void AssertRoleAlignment()
    {
        if ((int)global::Pivox.Ai.V1.Role.Unspecified != (int)MessageRole.Unspecified
            || (int)global::Pivox.Ai.V1.Role.User != (int)MessageRole.User
            || (int)global::Pivox.Ai.V1.Role.Assistant != (int)MessageRole.Assistant)
        {
            throw new InvalidOperationException(
                "Proto Role enum numbering drifted from MessageRole; " +
                "update the cast in MacOsChatService.BuildRequest.");
        }
    }

    public async IAsyncEnumerable<ChatStreamEvent> StreamGenerateAsync(
        string organizationName,
        IReadOnlyList<ChatTurn> turns,
        [EnumeratorCancellation] CancellationToken cancellationToken = default)
    {
        ArgumentException.ThrowIfNullOrEmpty(organizationName);
        if (!organizationName.StartsWith("organizations/", StringComparison.Ordinal))
        {
            throw new ArgumentException(
                "organizationName must be a full resource name of the " +
                "form 'organizations/{organization}'.",
                nameof(organizationName));
        }
        ArgumentNullException.ThrowIfNull(turns);
        if (turns.Count == 0)
        {
            throw new ChatException(
                ChatErrorKind.Server,
                "StreamGenerateAsync requires at least one turn.");
        }

        var request = BuildRequest(organizationName, turns);

        // AsyncServerStreamingCall is IDisposable — wrap its consumption
        // in a try/finally to ensure the gRPC call resources release
        // even if the consumer breaks out of the iteration early.
        using var call = _client.Ai.StreamGenerateContent(
            request,
            cancellationToken: cancellationToken);

        // Iterate the response stream. We deliberately don't pre-wrap
        // RpcException at the call site (StreamGenerateContent itself
        // doesn't throw — failures arrive when MoveNext awaits). The
        // mapping happens around each MoveNext via the helper.
        await foreach (var serverEvent in
            EnumerateWithMapping(call, cancellationToken).ConfigureAwait(true))
        {
            var mapped = MapEvent(serverEvent);
            if (mapped is not null)
            {
                yield return mapped;
            }
        }
    }

    /// <summary>Iterate the gRPC response stream, translating
    /// <see cref="RpcException"/> and cancellation into
    /// <see cref="ChatException"/>. Returns raw
    /// <c>ServerEvent</c>s so the caller can decide which oneof cases
    /// to surface.</summary>
    private static async IAsyncEnumerable<global::Pivox.Ai.V1.ServerEvent>
        EnumerateWithMapping(
            AsyncServerStreamingCall<global::Pivox.Ai.V1.ServerEvent> call,
            [EnumeratorCancellation] CancellationToken ct)
    {
        // Manual MoveNext lets us wrap each await individually so
        // exception mapping doesn't have to cover the yield path.
        var reader = call.ResponseStream;
        while (true)
        {
            bool hasNext;
            try
            {
                hasNext = await reader.MoveNext(ct).ConfigureAwait(true);
            }
            catch (RpcException rpc)
            {
                throw MapRpcException(rpc);
            }
            catch (OperationCanceledException oce) when (ct.IsCancellationRequested)
            {
                throw new ChatException(
                    ChatErrorKind.Cancelled, "request cancelled", oce);
            }

            if (!hasNext) yield break;
            yield return reader.Current;
        }
    }

    /// <summary>Build the <c>GenerateContentRequest</c> from a turn
    /// list. Maps <see cref="ChatTurn"/> → <c>InputMessage</c> with a
    /// single <c>TextPart</c> per message. Phase B is text-only;
    /// multi-part messages (file attachments, tool results) are a
    /// later phase.</summary>
    private static global::Pivox.Ai.V1.GenerateContentRequest BuildRequest(
        string organizationName, IReadOnlyList<ChatTurn> turns)
    {
        var request = new global::Pivox.Ai.V1.GenerateContentRequest
        {
            Parent = organizationName,
        };

        foreach (var turn in turns)
        {
            var input = new global::Pivox.Ai.V1.InputMessage
            {
                // Numeric values align with the proto Role enum by
                // construction (see MessageRole's doc-comment). Cast
                // through int rather than via name-based mapping —
                // catches any future drift at compile time when the
                // numeric ranges diverge.
                Role = (global::Pivox.Ai.V1.Role)(int)turn.Role,
            };
            input.Parts.Add(new global::Pivox.Ai.V1.MessagePart
            {
                Text = new global::Pivox.Ai.V1.TextPart { Text = turn.Text },
            });
            request.Messages.Add(input);
        }

        return request;
    }

    /// <summary>Translate a <c>ServerEvent</c> to a domain
    /// <see cref="ChatStreamEvent"/>. Returns null for events outside
    /// the text track — the caller drops those. Reasoning, tool, and
    /// artifact tracks are Phase C+ surfaces.</summary>
    private static ChatStreamEvent? MapEvent(global::Pivox.Ai.V1.ServerEvent evt)
    {
        switch (evt.EventCase)
        {
            case global::Pivox.Ai.V1.ServerEvent.EventOneofCase.TextStart:
                return new TextStartEvent(evt.TextStart.MessageId);
            case global::Pivox.Ai.V1.ServerEvent.EventOneofCase.TextDelta:
                return new TextDeltaEvent(evt.TextDelta.Delta);
            case global::Pivox.Ai.V1.ServerEvent.EventOneofCase.TextEnd:
                return new TextEndEvent();
            default:
                // Phase B drops non-text tracks. Log so observability
                // tells us what's on the wire when Phase C/D ramps
                // up the reasoning / tool / artifact surfaces.
                System.Diagnostics.Debug.WriteLine(
                    $"[MacOsChatService] dropping ServerEvent: {evt.EventCase}");
                return null;
        }
    }

    /// <summary>Categorize a gRPC failure for the UI layer. The raw
    /// RpcException is preserved as the inner exception for log
    /// inspection; the surfaced message is generic per the auth-leak
    /// rationale documented on <see cref="ChatErrorKind"/>.</summary>
    private static ChatException MapRpcException(RpcException rpc)
    {
        var kind = rpc.StatusCode switch
        {
            StatusCode.Unauthenticated => ChatErrorKind.AuthenticationRequired,
            // PermissionDenied is distinct from AuthenticationRequired:
            // the caller IS authenticated but lacks org/role access.
            // Re-signing in won't fix it — the UI should route to
            // "no access" rather than the sign-in screen.
            StatusCode.PermissionDenied => ChatErrorKind.PermissionDenied,
            StatusCode.Unavailable => ChatErrorKind.Network,
            StatusCode.DeadlineExceeded => ChatErrorKind.Network,
            StatusCode.Cancelled => ChatErrorKind.Cancelled,
            _ => ChatErrorKind.Server,
        };
        // Generic messages keyed off the kind; raw status detail
        // stays in the inner exception for diagnostic logs.
        var message = kind switch
        {
            ChatErrorKind.AuthenticationRequired =>
                "Authentication required. Please sign in again.",
            ChatErrorKind.PermissionDenied =>
                "You don't have access to this chat.",
            ChatErrorKind.Network =>
                "Network problem. Please try again.",
            ChatErrorKind.Cancelled =>
                "Cancelled.",
            _ =>
                "Something went wrong. Please try again.",
        };
        return new ChatException(kind, message, rpc);
    }
}
