namespace Pivox.Shared.Ai;

/// <summary>
/// State machine for a chat conversation viewmodel.
///
/// Transitions (driven by <see cref="ConversationViewModel"/>):
/// <code>
///   Idle      --SendAsync-->        Loading
///   Loading   --TextStartEvent-->   Streaming
///   Loading   --service error-->    Error
///   Streaming --TextDeltaEvent-->   Streaming  (no transition, updates last Message)
///   Streaming --TextEndEvent-->     Idle
///   Streaming --service error-->    Error
///   *         --Cancel-->           Idle
///   Error     --SendAsync-->        Loading    (retry allowed)
/// </code>
/// </summary>
public enum ConversationState
{
    /// <summary>No outbound request in flight. The composer is
    /// available; the next <c>SendAsync</c> will start a new stream.</summary>
    Idle,

    /// <summary>A request has been issued; awaiting the first
    /// <c>TextStartEvent</c> from the server. The composer is disabled
    /// and the UI shows a pre-stream affordance (spinner, dot ellipsis).</summary>
    Loading,

    /// <summary>The server is streaming assistant text. The last
    /// message in <c>ConversationViewModel.Messages</c> is the
    /// in-flight assistant message; its <c>Text</c> mutates per delta.
    /// <c>Cancel</c> aborts the stream cleanly.</summary>
    Streaming,

    /// <summary>The last <c>SendAsync</c> ended with an error.
    /// <c>ConversationViewModel.LastError</c> carries the structured
    /// error. Issuing a new <c>SendAsync</c> transitions back to
    /// Loading.</summary>
    Error,
}
