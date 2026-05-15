using Pivox.Shared.Persistence;

namespace Pivox.Auth;

/// <summary>
/// Persists the "remember me" email across app launches via the
/// shared <see cref="IKeyValueStore"/> abstraction. Routed through
/// the shared key so the macOS dotnet build and the SwiftUI build
/// (which both run under bundle id <c>app.pivox.native</c> and share
/// the <see cref="Foundation.NSUserDefaults"/> container)
/// interoperate: an email saved by one is picked up by the other.
///
/// Cross-platform note: WinUI uses
/// <c>ApplicationData.LocalSettings</c> (its own
/// <see cref="IKeyValueStore"/> impl) so the key is portable but
/// the store is per-platform — no cross-OS interoperation.
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
