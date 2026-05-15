namespace Pivox.Shared.Ai;

/// <summary>
/// Exception thrown by <see cref="IChatService"/> implementations to
/// signal the categorized failure mode. The underlying SDK exception,
/// if any, is preserved as <see cref="System.Exception.InnerException"/>
/// for log inspection but is never surfaced to the user — see
/// <see cref="ChatErrorKind"/> for the layering rationale.
/// </summary>
public sealed class ChatException : Exception
{
    public ChatException(ChatErrorKind kind, string message, Exception? inner = null)
        : base(message, inner)
    {
        Kind = kind;
    }

    public ChatErrorKind Kind { get; }
}
