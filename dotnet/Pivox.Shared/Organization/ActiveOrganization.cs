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
/// <para>Threading: writes raise
/// <see cref="INotifyPropertyChanged.PropertyChanged"/> on the
/// thread that performed the write. Callers should set
/// <see cref="Current"/> from the UI thread so subscribers can
/// safely touch UI directly. The viewmodel layer
/// (<c>ConversationViewModel</c>, future dashboard view-models)
/// observes the change and clears its per-org state — when an
/// organization switch happens mid-stream, any in-flight chat is
/// cancelled and the transcript is reset.</para>
/// </summary>
public sealed class ActiveOrganization : INotifyPropertyChanged
{
    private const string PersistenceKey = "pivox.active_organization";

    private readonly IKeyValueStore _store;
    private string? _current;

    public ActiveOrganization(IKeyValueStore store)
    {
        ArgumentNullException.ThrowIfNull(store);
        _store = store;
        // Restore the last-selected organization on construction.
        // Empty-string persisted values are normalized to null (an
        // empty string isn't a valid resource name).
        var saved = store.GetString(PersistenceKey);
        _current = string.IsNullOrEmpty(saved) ? null : saved;
    }

    /// <summary>Currently-active organization resource name (e.g.
    /// <c>organizations/acme</c>), or null when none is selected.
    /// Setting fires <see cref="PropertyChanged"/> and persists the
    /// new value through the configured
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
            if (_current == normalized) return;
            _current = normalized;
            _store.SetString(PersistenceKey, normalized);
            RaisePropertyChanged();
        }
    }

    public event PropertyChangedEventHandler? PropertyChanged;

    private void RaisePropertyChanged([CallerMemberName] string? propertyName = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(propertyName));
}
