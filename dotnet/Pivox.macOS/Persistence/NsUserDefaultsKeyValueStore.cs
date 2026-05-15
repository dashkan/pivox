using Foundation;
using Pivox.Shared.Persistence;

namespace Pivox.MacOs.Persistence;

/// <summary>
/// macOS implementation of <see cref="IKeyValueStore"/> backed by
/// <c>NSUserDefaults.StandardUserDefaults</c>. Settings persist in
/// the app's defaults container, keyed by
/// <c>CFBundleIdentifier</c> (<c>app.pivox.native</c>) — meaning the
/// dotnet build and the SwiftUI build share the same store. Any
/// keys read or written here interoperate across both apps as long
/// as the key naming matches.
///
/// Threading: <see cref="NSUserDefaults"/> is documented thread-safe
/// for reads and writes. The methods here add no locking; callers
/// don't need to marshal to the main thread for KV access. Writes
/// are buffered by the OS — <c>NSUserDefaults</c>
/// auto-synchronizes periodically and on app termination, so a
/// crash within ~5 seconds of a write may lose it. Acceptable for
/// preferences (we re-derive on next launch); not acceptable for
/// anything that has to be durable on every write (which is why
/// secrets go through a separate API in a future commit).
/// </summary>
public sealed class NsUserDefaultsKeyValueStore : IKeyValueStore
{
    private static NSUserDefaults Defaults => NSUserDefaults.StandardUserDefaults;

    public string? GetString(string key)
    {
        var raw = Defaults.StringForKey(key);
        // NSUserDefaults returns null for absent keys AND for keys
        // whose value isn't a string. We collapse both into null and
        // additionally normalize empty string to null so callers
        // don't have to handle the "set then cleared to empty"
        // edge case.
        return string.IsNullOrEmpty(raw) ? null : raw;
    }

    public void SetString(string key, string? value)
    {
        if (string.IsNullOrEmpty(value))
        {
            Defaults.RemoveObject(key);
        }
        else
        {
            Defaults.SetString(value, key);
        }
    }

    public bool TryGetBool(string key, out bool value)
    {
        // NSUserDefaults.BoolForKey returns false for both "absent"
        // and "explicitly false." Use ObjectForKey to disambiguate.
        if (Defaults.ValueForKey(new NSString(key)) is null)
        {
            value = false;
            return false;
        }
        value = Defaults.BoolForKey(key);
        return true;
    }

    public void SetBool(string key, bool value) => Defaults.SetBool(value, key);

    public bool TryGetDouble(string key, out double value)
    {
        if (Defaults.ValueForKey(new NSString(key)) is null)
        {
            value = 0;
            return false;
        }
        value = Defaults.DoubleForKey(key);
        return true;
    }

    public void SetDouble(string key, double value) => Defaults.SetDouble(value, key);
}
