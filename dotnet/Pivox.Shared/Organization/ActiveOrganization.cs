using System.ComponentModel;
using System.Runtime.CompilerServices;
using Pivox.Shared.Persistence;

namespace Pivox.Shared.Organization;

/// <summary>
/// Observable holder for the currently-active organization. The
/// app's UI is designed to operate on one organization at a time
/// (chat, dashboards, assets — all per-org); this is the single
/// source of truth for which one.
///
/// <para>Value shape: a full organization resource name of the form
/// <c>organizations/{organization}</c>. Null means "no organization
/// selected" — typically because the user is in pre-auth or
/// because they just signed in and the org list hasn't loaded yet.
/// Consumers that require an organization to operate (the chat
/// service, dashboard fetches) should gate on
/// <c>Current is not null</c> and surface a "select an
/// organization" hint until it resolves.</para>
///
/// <para>Persistence: the last-selected organization persists across
/// launches via the <see cref="IKeyValueStore"/> dependency. On
/// next launch, <see cref="Current"/> initializes to the persisted
/// value if any. Platform impls of <see cref="IKeyValueStore"/>
/// (NSUserDefaults on macOS, <c>ApplicationData.LocalSettings</c>
/// on WinUI) handle the storage detail.</para>
///
/// <para>Threading (Rule 12). The
/// <see cref="SynchronizationContext"/> captured at construction
/// is the delivery thread for
/// <see cref="INotifyPropertyChanged.PropertyChanged"/>. Writes
/// to <see cref="Current"/> can come from any thread — a UI event,
/// a gRPC completion that lands on the threadpool, a future
/// background "switch organization" RPC continuation — and the
/// event still fires on the UI thread. Subscribers (the chat
/// viewmodel, future dashboards) therefore never need to marshal.
/// The state mutation itself ALSO happens on the captured context
/// to keep the mutation + event atomic from any observer's
/// perspective. Mirror shape: <c>AppRouter</c>.</para>
///
/// <para>Eventual consistency: a write from a background thread is
/// posted (not synchronously executed). <see cref="Current"/> read
/// immediately after a background write may still show the prior
/// value until the post lands. Reads from the UI thread always
/// reflect the latest post-processed write.</para>
/// </summary>
public sealed class ActiveOrganization : INotifyPropertyChanged
{
    private const string PersistenceKey = "pivox.active_organization";

    private readonly IKeyValueStore _store;
    private readonly SynchronizationContext _uiContext;
    private string? _current;

    public ActiveOrganization(IKeyValueStore store)
    {
        ArgumentNullException.ThrowIfNull(store);
        _store = store;
        _uiContext = SynchronizationContext.Current
            ?? throw new InvalidOperationException(
                "ActiveOrganization must be constructed on a thread with " +
                "a SynchronizationContext. macOS and Windows apps install " +
                "one via their event-loop runtimes; tests install one via " +
                "the test class fixture.");
        // Restore the last-selected organization on construction.
        // Empty-string persisted values are normalized to null (an
        // empty string isn't a valid resource name).
        var saved = store.GetString(PersistenceKey);
        _current = string.IsNullOrEmpty(saved) ? null : saved;
    }

    /// <summary>Currently-active organization resource name (e.g.
    /// <c>organizations/acme</c>), or null when none is selected.
    /// Setting fires <see cref="PropertyChanged"/> on the UI thread
    /// (captured <see cref="SynchronizationContext"/>) and persists
    /// the new value through the configured
    /// <see cref="IKeyValueStore"/>. Same-value writes are
    /// suppressed (no event, no persistence write).</summary>
    public string? Current
    {
        get => _current;
        set
        {
            // Normalize empty string to null so the "no organization"
            // state has one canonical form.
            var normalized = string.IsNullOrEmpty(value) ? null : value;

            // Fast path: already on the captured context → apply
            // synchronously so single-thread callers get familiar
            // "mutation visible by the time the call returns"
            // semantics. Background callers route through Post →
            // eventual consistency, documented in the class doc.
            if (SynchronizationContext.Current == _uiContext)
            {
                ApplySet(normalized);
            }
            else
            {
                _uiContext.Post(static state =>
                {
                    var (self, v) = ((ActiveOrganization, string?))state!;
                    self.ApplySet(v);
                }, (this, normalized));
            }
        }
    }

    private void ApplySet(string? normalized)
    {
        if (_current == normalized) return;
        _current = normalized;
        _store.SetString(PersistenceKey, normalized);
        RaisePropertyChanged(nameof(Current));
    }

    public event PropertyChangedEventHandler? PropertyChanged;

    private void RaisePropertyChanged([CallerMemberName] string? propertyName = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(propertyName));
}
