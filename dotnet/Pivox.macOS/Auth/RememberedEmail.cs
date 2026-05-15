using Foundation;

namespace Pivox.Auth;

/// <summary>
/// Persists the "remember me" email across app launches via
/// <see cref="NSUserDefaults"/>. Mirrors the SwiftUI app's
/// <c>AppStateBridge.save(_:forKey: "remembered_email")</c> shape so
/// the two macOS apps interoperate (the dotnet/ build can pick up an
/// email saved by the SwiftUI build, and vice versa, since they share
/// the same bundle identifier and therefore the same defaults
/// container).
///
/// Per-platform — WinUI uses <c>ApplicationData.LocalSettings</c>
/// (see <c>winui-auth.md</c>).
/// </summary>
public sealed class RememberedEmail
{
    private const string Key = "remembered_email";

    public string? Get()
    {
        var value = NSUserDefaults.StandardUserDefaults.StringForKey(Key);
        return string.IsNullOrEmpty(value) ? null : value;
    }

    public void Set(string? email)
    {
        if (string.IsNullOrEmpty(email))
        {
            NSUserDefaults.StandardUserDefaults.RemoveObject(Key);
        }
        else
        {
            NSUserDefaults.StandardUserDefaults.SetString(email, Key);
        }
    }
}
