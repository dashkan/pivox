using Pivox.Shared.Persistence;

namespace Pivox.Auth;

/// <summary>
/// Persists the "remember me" email across app launches via the
/// shared <see cref="IKeyValueStore"/> abstraction (backed by
/// <c>ApplicationData.LocalSettings</c> on WinUI; see
/// <c>ApplicationDataKeyValueStore</c>). Mirrors the macOS
/// <c>RememberedEmail</c> file by shape, including the
/// <c>remembered_email</c> key — keys are portable across platforms,
/// stores are not.
///
/// Per-platform — lives in the WinUI project, not Pivox.Shared:
/// trivially small but bound to whichever <see cref="IKeyValueStore"/>
/// the composition root injects.
/// </summary>
public sealed class RememberedEmail
{
    private const string Key = "remembered_email";

    private readonly IKeyValueStore _store;

    public RememberedEmail(IKeyValueStore store)
    {
        ArgumentNullException.ThrowIfNull(store);
        _store = store;
    }

    public string? Get() => _store.GetString(Key);

    public void Set(string? email) => _store.SetString(Key, email);
}
