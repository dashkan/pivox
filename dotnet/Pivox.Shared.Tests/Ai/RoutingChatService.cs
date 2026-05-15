using Pivox.Shared.Ai;

namespace Pivox.Shared.Tests.Ai;

/// <summary>
/// <see cref="IChatService"/> that delegates each call to a swappable
/// inner <see cref="StubChatService"/>. Lets a test exercise a
/// "first call fails, retry succeeds" flow on a single viewmodel
/// instance — the VM's <c>_chat</c> field stays stable while the test
/// swaps the underlying stub between calls.
///
/// Without this, the test has to construct a second viewmodel for the
/// retry, which loses the "same VM, error → retry" continuity it's
/// trying to verify.
/// </summary>
internal sealed class RoutingChatService : IChatService
{
    private StubChatService _inner;

    public RoutingChatService(StubChatService initial)
    {
        _inner = initial;
    }

    public void SetInner(StubChatService next) => _inner = next;

    public IAsyncEnumerable<ChatStreamEvent> StreamGenerateAsync(
        string organizationName,
        IReadOnlyList<ChatTurn> turns,
        CancellationToken cancellationToken = default)
        => _inner.StreamGenerateAsync(organizationName, turns, cancellationToken);
}
