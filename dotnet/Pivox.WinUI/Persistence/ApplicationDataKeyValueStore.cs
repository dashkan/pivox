using Pivox.Shared.Persistence;
using Windows.Storage;

namespace Pivox.Persistence;

/// <summary>
/// WinUI implementation of <see cref="IKeyValueStore"/> backed by
/// <c>Windows.Storage.ApplicationData.Current.LocalSettings</c>. Settings
/// persist in the app package's per-user local settings container —
/// roamed across launches, scoped to the package identity declared in
/// <c>Package.appxmanifest</c>. Unlike the macOS side (which shares
/// <c>NSUserDefaults</c> with the SwiftUI build under the same
/// <c>CFBundleIdentifier</c>), there is no cross-stack store
/// interoperation on Windows — package identity is the boundary.
///
/// Threading: <see cref="IPropertySet"/>-backed
/// <see cref="ApplicationDataContainer.Values"/> is documented
/// thread-safe for reads and writes; the methods here add no locking,
/// callers don't need to marshal to the dispatcher thread for KV
/// access. Writes flush asynchronously to disk; a crash within a small
/// window after a write may lose it. Acceptable for preferences (we
/// re-derive on next launch); not acceptable for secrets (which go
/// behind a separate API in a future commit).
///
/// Disambiguation: where the macOS impl had to reach for the indexer
/// (<c>Defaults[key]</c>) to tell "absent" from "stored false/0" —
/// because Sharpie binds <c>NSUserDefaults.ObjectForKey</c> as
/// non-public — <c>LocalSettings.Values</c> exposes
/// <see cref="System.Collections.Generic.IDictionary{TKey,TValue}.TryGetValue"/>
/// directly. The "key absent" vs. "value present and equals the
/// type's zero" distinction is read off <see cref="bool"/>-returning
/// <c>TryGetValue</c>; no indexer workaround needed.
/// </summary>
public sealed class ApplicationDataKeyValueStore : IKeyValueStore
{
    private static ApplicationDataContainer Settings
        => ApplicationData.Current.LocalSettings;

    public string? GetString(string key)
    {
        // LocalSettings.Values stores objects of arbitrary type. A
        // missing key and a stored non-string (or stored empty string)
        // both collapse to null here, matching macOS impl semantics
        // — callers see one canonical "no value" sentinel.
        if (!Settings.Values.TryGetValue(key, out var raw)) return null;
        var s = raw as string;
        return string.IsNullOrEmpty(s) ? null : s;
    }

    public void SetString(string key, string? value)
    {
        if (string.IsNullOrEmpty(value))
        {
            Settings.Values.Remove(key);
        }
        else
        {
            Settings.Values[key] = value;
        }
    }

    public bool TryGetBool(string key, out bool value)
    {
        // TryGetValue distinguishes "absent" from "present-and-false"
        // directly: false return ⇒ absent, true return ⇒ value
        // present (then the `is bool` pattern guards against the
        // legacy case of a value stored under this key by an older
        // build using a different type).
        if (Settings.Values.TryGetValue(key, out var raw) && raw is bool b)
        {
            value = b;
            return true;
        }
        value = false;
        return false;
    }

    public void SetBool(string key, bool value) => Settings.Values[key] = value;

    public bool TryGetDouble(string key, out double value)
    {
        if (Settings.Values.TryGetValue(key, out var raw) && raw is double d)
        {
            value = d;
            return true;
        }
        value = 0;
        return false;
    }

    public void SetDouble(string key, double value) => Settings.Values[key] = value;
}
