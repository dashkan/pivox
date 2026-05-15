using Windows.Storage;

namespace Pivox.Auth;

/// <summary>
/// Persists the "remember me" email across app launches via
/// <see cref="ApplicationData.LocalSettings"/>. Parallels the macOS
/// <c>RememberedEmail</c> that uses <c>NSUserDefaults</c>.
///
/// Per-platform — lives in the WinUI project, not Pivox.Shared.
/// </summary>
public sealed class RememberedEmail
{
    private const string Key = "remembered_email";
    private readonly ApplicationDataContainer _settings
        = ApplicationData.Current.LocalSettings;

    public string? Get() => _settings.Values[Key] as string;

    public void Set(string? email)
    {
        if (string.IsNullOrEmpty(email))
            _settings.Values.Remove(Key);
        else
            _settings.Values[Key] = email;
    }
}
