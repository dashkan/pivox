using Pivox.Shared.Persistence;

namespace Pivox.Shared.Tests.Persistence;

/// <summary>
/// In-memory <see cref="IKeyValueStore"/> for unit tests. Backs the
/// shared persistence abstraction with a plain
/// <see cref="Dictionary{TKey, TValue}"/> so tests can verify both
/// the consuming class's behavior AND that the right keys/values
/// are written, without needing a real
/// <c>NSUserDefaults</c>/<c>ApplicationData.LocalSettings</c> stand-in.
///
/// Mirrors the platform impl semantics: <see cref="SetString"/> with
/// null removes the key; absent keys produce <c>false</c> from the
/// <c>TryGet</c> methods. Tests can also seed initial values via the
/// <see cref="Backing"/> dictionary before constructing the
/// consuming class — useful for "what does this VM do when a value
/// is already persisted?" scenarios.
/// </summary>
internal sealed class InMemoryKeyValueStore : IKeyValueStore
{
    public Dictionary<string, object?> Backing { get; } = new();

    public string? GetString(string key)
    {
        if (!Backing.TryGetValue(key, out var v)) return null;
        var s = v as string;
        // Mirror NsUserDefaultsKeyValueStore + the WinUI handoff
        // skeleton: empty-string and absent are collapsed into null
        // so callers see one canonical "no preference" representation
        // regardless of platform. A test that exposes a divergence
        // between this impl and the platform impls would be the kind
        // of test-infra drift the project's CLAUDE.md test guidance
        // explicitly warns against.
        return string.IsNullOrEmpty(s) ? null : s;
    }

    public void SetString(string key, string? value)
    {
        if (string.IsNullOrEmpty(value)) Backing.Remove(key);
        else Backing[key] = value;
    }

    public bool TryGetBool(string key, out bool value)
    {
        if (Backing.TryGetValue(key, out var v) && v is bool b)
        {
            value = b;
            return true;
        }
        value = false;
        return false;
    }

    public void SetBool(string key, bool value) => Backing[key] = value;

    public bool TryGetDouble(string key, out double value)
    {
        if (Backing.TryGetValue(key, out var v) && v is double d)
        {
            value = d;
            return true;
        }
        value = 0;
        return false;
    }

    public void SetDouble(string key, double value) => Backing[key] = value;
}
