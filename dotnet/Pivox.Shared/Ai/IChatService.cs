namespace Pivox.Shared.Ai;

/// <summary>
/// Cross-platform chat surface. Each platform implements this against
/// its native gRPC stack — macOS via <c>Grpc.Net.Client</c> through
/// <c>PivoxClient.Ai</c>, Windows likewise via the same
/// <c>PivoxClient</c> — and produces <see cref="ChatStreamEvent"/>
/// values that the cross-platform <see cref="ConversationViewModel"/>
/// consumes without knowing about proto types.
///
/// The interface is intentionally narrow: one method that issues a
/// streaming generation. Stateless re: organization — the active
/// organization is passed per-call, not bound at construction —
/// so a single service instance handles every organization the
/// user belongs to. Switching organizations doesn't require
/// recreating the service. The viewmodel layer
/// (<see cref="ConversationViewModel"/>) is responsible for
/// providing the active organization on each
/// <c>SendAsync</c>; the service simply forwards it to the proto
/// request.
///
/// Threading: implementations choose their dispatch surface. The
/// viewmodel subscribes to the returned stream from its construction
/// thread (the UI thread, per Rule 12); platform implementations are
/// responsible for resuming on a thread the viewmodel can safely
/// mutate UI state from. The default
/// <c>ConfigureAwait(true)</c> behavior of the consumer's
/// <c>await foreach</c> handles this when the consumer is itself on
/// the UI thread.
/// </summary>
public interface IChatService
{
    /// <summary>
    /// Stream-generate an assistant response for
    /// <paramref name="turns"/>, scoped to
    /// <paramref name="organizationName"/>. The implementation:
    ///
    /// <list type="number">
    /// <item>Builds a <c>GenerateContentRequest</c> with
    ///   <c>Parent = organizationName</c>, maps each
    ///   <see cref="ChatTurn"/> to a proto <c>InputMessage</c>.</item>
    /// <item>Fetches an auth token (via the platform's
    ///   <c>IAuthService</c>) and attaches it as the Bearer token
    ///   on the outbound RPC.</item>
    /// <item>Issues the server-streaming RPC against
    ///   <c>pivox.ai.v1.AiChat.StreamGenerateContent</c>.</item>
    /// <item>Maps each <c>ServerEvent</c> received over the wire to
    ///   the corresponding <see cref="ChatStreamEvent"/> subtype.
    ///   Non-text-track events (reasoning, tool, artifact) are
    ///   silently dropped in Phase B.</item>
    /// <item>Throws <see cref="ChatException"/> with the appropriate
    ///   <see cref="ChatErrorKind"/> on any failure path.</item>
    /// </list>
    ///
    /// Cancellation is observed via
    /// <paramref name="cancellationToken"/>; the returned async
    /// sequence terminates cleanly when triggered.
    /// </summary>
    /// <param name="organizationName">Full resource name of the
    /// organization scoping this call, e.g. <c>organizations/acme</c>.
    /// Must be non-empty and start with <c>organizations/</c>.</param>
    IAsyncEnumerable<ChatStreamEvent> StreamGenerateAsync(
        string organizationName,
        IReadOnlyList<ChatTurn> turns,
        CancellationToken cancellationToken = default);
}

/// <summary>One conversation turn sent to the model. Phase B's
/// vertical slice supports text-only turns; multi-part messages
/// (file attachments, tool results) are Phase D+ scope.</summary>
public sealed record ChatTurn(MessageRole Role, string Text);
