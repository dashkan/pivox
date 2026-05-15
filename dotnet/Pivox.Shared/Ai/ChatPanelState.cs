using System.ComponentModel;
using System.Runtime.CompilerServices;
using Pivox.Shared.Persistence;

namespace Pivox.Shared.Ai;

/// <summary>
/// Observable visibility state for the AI chat panel — shared
/// between macOS and WinUI. The platform shells subscribe to
/// <see cref="PropertyChanged"/> to drive their inspector pane's
/// collapsed/expanded state; the toolbar toggle writes
/// through <see cref="IsVisible"/> (or calls
/// <see cref="Toggle"/>) to flip it.
///
/// <para><b>Why shared.</b> Visibility is a user preference, not a
/// platform concern. Persisting in a shared abstraction keeps the
/// macOS NSUserDefaults backing and the WinUI
/// <c>ApplicationData.LocalSettings</c> backing under the same key
/// (<c>pivox.chat_panel_visible</c>), and prevents the two
/// platforms from drifting on the question "should the panel start
/// open or closed."</para>
///
/// <para><b>Persistence.</b> Written through the injected
/// <see cref="IKeyValueStore"/> on every change. On construction,
/// the persisted bool (if any) becomes the initial value; an
/// absent key produces <c>false</c> (collapsed by default for new
/// users — the panel opts in, doesn't opt out).</para>
///
/// <para><b>Threading (Rule 12).</b> Captures
/// <see cref="SynchronizationContext"/> at construction; writes
/// from the captured context apply synchronously, writes from
/// elsewhere are posted. The dot­netCLAUDE Rule 12 contract.</para>
///
/// <para><b>What this is NOT.</b> Not where "should the toggle be
/// disabled" lives — that's a platform-side derivation from
/// <c>ActiveOrganization.Current != null</c>. The panel can be
/// "visible" in state with no org selected; the platform UI is
/// responsible for either showing an empty state, disabling the
/// toggle, or gating the surface — depending on its UX. This class
/// just owns the bool.</para>
/// </summary>
public sealed class ChatPanelState : INotifyPropertyChanged
{
    private const string PersistenceKey = "pivox.chat_panel_visible";

    private readonly IKeyValueStore _store;
    private readonly SynchronizationContext _uiContext;
    private bool _isVisible;

    public ChatPanelState(IKeyValueStore store)
    {
        ArgumentNullException.ThrowIfNull(store);
        _store = store;
        _uiContext = SynchronizationContext.Current
            ?? throw new InvalidOperationException(
                "ChatPanelState must be constructed on a thread with a " +
                "SynchronizationContext. macOS and Windows apps install " +
                "one via their event-loop runtimes; tests install one via " +
                "Pivox.Shared.Tests/Threading/UiThread.cs.");

        // Restore persisted visibility. TryGetBool(out var v) returns
        // false for both "absent" and "stored false" — and we want
        // false in both cases (collapsed-by-default for new users
        // matches the explicit-collapse case). The distinction matters
        // for other settings; for this one, the two semantics happen
        // to converge.
        _isVisible = store.TryGetBool(PersistenceKey, out var stored) && stored;
    }

    /// <summary>Whether the chat panel is currently visible. Setting
    /// fires <see cref="PropertyChanged"/> on the captured UI thread
    /// and persists the new value through the configured
    /// <see cref="IKeyValueStore"/>. Same-value writes are
    /// suppressed.</summary>
    public bool IsVisible
    {
        get => _isVisible;
        set
        {
            // Fast path: on captured context → apply synchronously.
            // Background callers route through Post (Rule 12).
            if (SynchronizationContext.Current == _uiContext)
            {
                ApplySet(value);
            }
            else
            {
                _uiContext.Post(static state =>
                {
                    var (self, v) = ((ChatPanelState, bool))state!;
                    self.ApplySet(v);
                }, (this, value));
            }
        }
    }

    /// <summary>Flip <see cref="IsVisible"/>. Convenience for
    /// toolbar/menu toggle handlers that don't need to know the
    /// current state to invert it.</summary>
    public void Toggle() => IsVisible = !_isVisible;

    private void ApplySet(bool value)
    {
        if (_isVisible == value) return;
        _isVisible = value;
        _store.SetBool(PersistenceKey, value);
        RaisePropertyChanged(nameof(IsVisible));
    }

    public event PropertyChangedEventHandler? PropertyChanged;

    private void RaisePropertyChanged([CallerMemberName] string? propertyName = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(propertyName));
}
