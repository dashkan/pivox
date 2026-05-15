using Pivox.Shared.Ai;
using Pivox.Shared.Tests.Persistence;
using Pivox.Shared.Tests.Threading;
using Xunit;

namespace Pivox.Shared.Tests.Ai;

/// <summary>
/// Tests for <see cref="ChatPanelState"/> — the observable visibility
/// holder for the AI chat panel.
///
/// All tests run inside <see cref="UiThread.Run"/> so the
/// constructor's <c>SynchronizationContext.Current</c> capture
/// succeeds and writes from the test thread take the fast
/// synchronous path. See <c>dotnet/CLAUDE.md</c> Rule 12 →
/// "Testing Rule-12 services" for the rationale.
/// </summary>
public class ChatPanelStateTests
{
    private const string PersistenceKey = "pivox.chat_panel_visible";

    [Fact]
    public void Initial_State_DefaultsToCollapsed_WhenStoreEmpty() => UiThread.Run(() =>
    {
        // No persisted value → IsVisible defaults to false. Conserves
        // screen space for new users who haven't opted into the panel.
        var store = new InMemoryKeyValueStore();
        var state = new ChatPanelState(store);

        Assert.False(state.IsVisible);
        return Task.CompletedTask;
    });

    [Fact]
    public void Initial_State_RestoresPersistedTrue() => UiThread.Run(() =>
    {
        // A previously-visible panel reopens on relaunch. Store carries
        // the bool directly (we use SetBool / TryGetBool).
        var store = new InMemoryKeyValueStore();
        store.SetBool(PersistenceKey, true);

        var state = new ChatPanelState(store);

        Assert.True(state.IsVisible);
        return Task.CompletedTask;
    });

    [Fact]
    public void Initial_State_RestoresPersistedFalse() => UiThread.Run(() =>
    {
        // A previously-collapsed panel stays collapsed on relaunch.
        // Critical to distinguish "user explicitly collapsed" from
        // "no preference" — both are false, but the persistence
        // round-trip must work for the explicit case.
        var store = new InMemoryKeyValueStore();
        store.SetBool(PersistenceKey, false);

        var state = new ChatPanelState(store);

        Assert.False(state.IsVisible);
        return Task.CompletedTask;
    });

    [Fact]
    public void Setter_PersistsValue_AndFiresPropertyChanged() => UiThread.Run(() =>
    {
        var store = new InMemoryKeyValueStore();
        var state = new ChatPanelState(store);

        var raised = 0;
        state.PropertyChanged += (_, args) =>
        {
            if (args.PropertyName == nameof(state.IsVisible)) raised++;
        };

        state.IsVisible = true;

        Assert.True(state.IsVisible);
        Assert.Equal(1, raised);
        Assert.True(store.TryGetBool(PersistenceKey, out var stored));
        Assert.True(stored);
        return Task.CompletedTask;
    });

    [Fact]
    public void Setter_SameValue_IsSuppressed() => UiThread.Run(() =>
    {
        // No PropertyChanged, no persistence write, on a same-value
        // set. Avoids flicker on subscribers when the toolbar toggle
        // is rapidly clicked in the same state and avoids unnecessary
        // disk traffic against IKeyValueStore.
        var store = new InMemoryKeyValueStore();
        var state = new ChatPanelState(store) { IsVisible = true };

        var raised = 0;
        state.PropertyChanged += (_, _) => raised++;

        state.IsVisible = true;

        Assert.Equal(0, raised);
        return Task.CompletedTask;
    });

    [Fact]
    public void Toggle_FlipsIsVisible() => UiThread.Run(() =>
    {
        // Toggle() is the primary callsite from the toolbar item —
        // it doesn't need to know the current state to flip it. Keep
        // the helper so platform code doesn't reach in for a
        // !IsVisible inversion.
        var store = new InMemoryKeyValueStore();
        var state = new ChatPanelState(store);
        Assert.False(state.IsVisible);

        state.Toggle();
        Assert.True(state.IsVisible);

        state.Toggle();
        Assert.False(state.IsVisible);
        return Task.CompletedTask;
    });

    [Fact]
    public void Constructor_ThrowsIfStoreNull() => UiThread.Run(() =>
    {
        Assert.Throws<ArgumentNullException>(() => new ChatPanelState(null!));
        return Task.CompletedTask;
    });

    [Fact]
    public void Constructor_ThrowsIfNoSynchronizationContext()
    {
        // Rule 12: constructed off the UI thread should fail loud.
        // Run on a dedicated Thread (not Task.Run — the
        // VSTHRD002 analyzer rightly complains about
        // GetAwaiter().GetResult() and this isn't the place to
        // suppress it) so SynchronizationContext.Current is null at
        // the ctor call.
        var store = new InMemoryKeyValueStore();
        Exception? captured = null;

        var thread = new Thread(() =>
        {
            try
            {
                _ = new ChatPanelState(store);
            }
            catch (Exception ex)
            {
                captured = ex;
            }
        });
        thread.Start();
        thread.Join();

        Assert.IsType<InvalidOperationException>(captured);
        Assert.Contains(
            "SynchronizationContext", captured!.Message,
            StringComparison.OrdinalIgnoreCase);
    }
}
