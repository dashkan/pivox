namespace Pivox.Shared.Ai;

/// <summary>
/// Cross-platform chat surface. Each platform implements this against
/// its native gRPC stack — macOS via <c>Grpc.Net.Client</c> through
/// <c>PivoxClient.Ai</c>, Windows likewise once WinUI wires its own
/// <c>PivoxClient</c> instance — and produces
/// <see cref="ChatStreamEvent"/> values that the cross-platform
/// <see cref="ConversationViewModel"/> consumes without knowing about
/// proto types.
///
/// The interface is intentionally narrow: one method that issues a
/// streaming generation. Stateless (no conversation id) for Phase B;
/// the server's <c>StreamGenerateContent</c> RPC handles stateless
/// calls natively, so this surface doesn't need a per-conversation
/// abstraction yet.
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
    /// <paramref name="turns"/>. The implementation:
    ///
    /// <list type="number">
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
    IAsyncEnumerable<ChatStreamEvent> StreamGenerateAsync(
        IReadOnlyList<ChatTurn> turns,
        CancellationToken cancellationToken = default);
}

/// <summary>One conversation turn sent to the model. Phase B's
/// vertical slice supports text-only turns; multi-part messages
/// (file attachments, tool results) are Phase D+ scope.</summary>
public sealed record ChatTurn(MessageRole Role, string Text);
