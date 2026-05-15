using System.Runtime.CompilerServices;
using System.Threading.Channels;
using Pivox.Shared.Ai;

namespace Pivox.Shared.Tests.Ai;

/// <summary>
/// In-memory <see cref="IChatService"/> for driving
/// <see cref="ConversationViewModel"/> through scripted event sequences
/// in tests. Lets the test:
///
/// <list type="bullet">
/// <item>Capture the turns the viewmodel sends (via
///   <see cref="LastTurnsSent"/>).</item>
/// <item>Push events into the stream
///   (<see cref="Emit"/> / <see cref="Complete"/>).</item>
/// <item>Inject a fault into the stream
///   (<see cref="Throw"/>).</item>
/// </list>
///
/// Channel-based so the test can interleave its assertions with event
/// emission — useful for verifying state transitions at each boundary
/// (Loading → Streaming on first TextStart, Streaming → Idle on
/// TextEnd, etc.).
/// </summary>
internal sealed class StubChatService : IChatService
{
    private readonly Channel<ChatStreamEvent> _channel =
        Channel.CreateUnbounded<ChatStreamEvent>(
            new UnboundedChannelOptions
            {
                SingleReader = true,
                SingleWriter = true,
            });

    public IReadOnlyList<ChatTurn>? LastTurnsSent { get; private set; }
    public string? LastOrganizationSent { get; private set; }
    public int InvocationCount { get; private set; }

    public async IAsyncEnumerable<ChatStreamEvent> StreamGenerateAsync(
        string organizationName,
        IReadOnlyList<ChatTurn> turns,
        [EnumeratorCancellation] CancellationToken cancellationToken = default)
    {
        LastOrganizationSent = organizationName;
        LastTurnsSent = turns;
        InvocationCount++;

        await foreach (var evt in _channel.Reader
            .ReadAllAsync(cancellationToken)
            .ConfigureAwait(true))
        {
            yield return evt;
        }
    }

    /// <summary>Push a single event into the stream. Returns
    /// immediately; the viewmodel observes it on its next iteration.</summary>
    public void Emit(ChatStreamEvent evt) => _channel.Writer.TryWrite(evt);

    /// <summary>Close the stream successfully (channel reader sees
    /// completion). The viewmodel's <c>await foreach</c> exits
    /// cleanly.</summary>
    public void Complete() => _channel.Writer.TryComplete();

    /// <summary>Close the stream with an exception. The viewmodel's
    /// <c>await foreach</c> propagates the throw, exercising the
    /// error-handling branch.</summary>
    public void Throw(Exception ex) => _channel.Writer.TryComplete(ex);
}
