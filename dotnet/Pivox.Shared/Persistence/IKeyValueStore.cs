namespace Pivox.Shared.Persistence;

/// <summary>
/// Cross-platform key-value store for non-secret application
/// preferences (active organization, panel layout, last-opened
/// conversation id, remembered email, etc.).
///
/// Each platform implements this against its native preferences
/// store: <c>NSUserDefaults</c> on macOS,
/// <c>ApplicationData.LocalSettings</c> on WinUI. The interface stays
/// narrow — basic primitives plus enum support via the extension
/// methods in <see cref="KeyValueStoreExtensions"/> — so the impl
/// surface is small and AOT-trim-safe by construction.
///
/// Storage scope: process-local user preferences. Don't put secrets
/// here — Firebase JWTs, OAuth refresh tokens, license keys, etc.
/// belong behind a separate <c>ISecretStore</c> abstraction backed
/// by Keychain (macOS) / Credential Manager (WinUI). That interface
/// doesn't exist yet because there are no current consumers; add
/// when the first one arrives. The naming here
/// (<c>IKeyValueStore</c>, not <c>IPersistenceStore</c>) is honest
/// about the boundary so a casual reader doesn't drop a secret in.
///
/// JSON-blob storage: callers that need to persist a complex POCO
/// should serialize via their OWN <c>JsonSerializerContext</c>
/// (their type, their context for AOT-trim-safety) and pass the
/// serialized string through <see cref="SetString"/>. The interface
/// deliberately doesn't expose a generic <c>Set&lt;T&gt;</c> —
/// that would require either reflection (AOT-unsafe) or a single
/// shared context registry (a different kind of friction). Better
/// to keep the abstraction narrow and let callers manage their own
/// serialization registration.
/// </summary>
public interface IKeyValueStore
{
    /// <summary>Reads the string under <paramref name="key"/>, or
    /// returns null if the key is absent. Empty string is preserved
    /// (distinct from null).</summary>
    string? GetString(string key);

    /// <summary>Writes <paramref name="value"/> under
    /// <paramref name="key"/>. Passing null removes the key
    /// (semantically equivalent to "no preference set").</summary>
    void SetString(string key, string? value);

    /// <summary>Reads the boolean under <paramref name="key"/>.
    /// Returns true with the value when present, false when absent.</summary>
    bool TryGetBool(string key, out bool value);

    void SetBool(string key, bool value);

    /// <summary>Reads the double under <paramref name="key"/>.
    /// Returns true with the value when present, false when absent.</summary>
    bool TryGetDouble(string key, out double value);

    void SetDouble(string key, double value);
}

/// <summary>
/// Extensions over <see cref="IKeyValueStore"/> for compound
/// patterns the interface doesn't directly expose.
///
/// <see cref="TryGetEnum{T}"/> / <see cref="SetEnum{T}"/> store
/// enum values as their string-name representation via
/// <see cref="Enum.TryParse{TEnum}(string, out TEnum)"/> /
/// <see cref="Enum.ToString()"/>. String-encoded survives enum
/// re-numbering across releases (numeric values can shift if a
/// caller adds an entry in the middle of the enum) and is
/// AOT-trim-safe — the enum methods don't need reflection.
/// </summary>
public static class KeyValueStoreExtensions
{
    public static bool TryGetEnum<T>(
        this IKeyValueStore store, string key, out T value)
        where T : struct, Enum
    {
        var raw = store.GetString(key);
        if (raw is not null && Enum.TryParse<T>(raw, out value))
        {
            return true;
        }
        value = default;
        return false;
    }

    public static void SetEnum<T>(
        this IKeyValueStore store, string key, T value)
        where T : struct, Enum
        => store.SetString(key, value.ToString());
}
