using System.ComponentModel;

namespace Pivox.Shared.Auth;

/// <summary>
/// View-model for the login surface. State + transitions live here so
/// both <c>Pivox.macOS</c> (AppKit) and <c>Pivox.WinUI</c> (XAML) bind
/// the same logic — the UI layer only renders state and forwards user
/// input.
///
/// Scope (matches SwiftUI <c>LoginView</c> phase 1):
/// <list type="bullet">
/// <item>Email + password sign-in.</item>
/// <item>Google OAuth.</item>
/// <item>Loading state during requests.</item>
/// <item>Error message surfacing.</item>
/// </list>
///
/// Out of scope (mirrors deferred SwiftUI features):
/// <list type="bullet">
/// <item>Email-first SSO resolution (<c>didResolveAsPassword</c> two-step
///   flow) — needs <c>IAuthService.ResolveSsoProvider</c>.</item>
/// <item>MFA challenge (<c>pendingMFAResolver</c>) — needs
///   <c>IAuthService.PendingMfa</c>.</item>
/// <item>GitHub OAuth — needs <c>IAuthService.SignInWithGitHubAsync</c>.</item>
/// <item>Remember-me persistence.</item>
/// </list>
///
/// Threading: <see cref="IAuthService"/> dispatches its SDK work
/// internally, and the await continuations land back on the calling
/// (UI) thread under <see cref="System.Threading.SynchronizationContext"/>.
/// All property mutations happen on the UI thread; <see cref="PropertyChanged"/>
/// fires from there too. The VM is not thread-safe by design.
/// </summary>
public sealed class LoginViewModel : INotifyPropertyChanged
{
    private readonly IAuthService _auth;

    private string _email = "";
    private string _password = "";
    private bool _isLoading;
    private string? _errorMessage;

    public LoginViewModel(IAuthService auth)
    {
        _auth = auth ?? throw new ArgumentNullException(nameof(auth));
    }

    public event PropertyChangedEventHandler? PropertyChanged;

    public string Email
    {
        get => _email;
        set
        {
            if (_email == value) return;
            _email = value;
            Raise(nameof(Email));
            Raise(nameof(CanSubmit));
        }
    }

    public string Password
    {
        get => _password;
        set
        {
            if (_password == value) return;
            _password = value;
            Raise(nameof(Password));
            Raise(nameof(CanSubmit));
        }
    }

    /// <summary>True while a sign-in request (email/password or OAuth) is
    /// in flight. UI disables inputs and shows progress when true.</summary>
    public bool IsLoading
    {
        get => _isLoading;
        private set
        {
            if (_isLoading == value) return;
            _isLoading = value;
            Raise(nameof(IsLoading));
            Raise(nameof(CanSubmit));
        }
    }

    /// <summary>User-facing error from the most recent attempt. Cleared on
    /// each new submission. Null = no error visible.</summary>
    public string? ErrorMessage
    {
        get => _errorMessage;
        private set
        {
            if (_errorMessage == value) return;
            _errorMessage = value;
            Raise(nameof(ErrorMessage));
        }
    }

    /// <summary>Primary button is enabled iff both fields are non-empty
    /// and no request is in flight. Email validation (format) is left to
    /// the auth backend; minimum gate is "user typed something."</summary>
    public bool CanSubmit
        => !IsLoading
           && !string.IsNullOrWhiteSpace(_email)
           && !string.IsNullOrEmpty(_password);

    /// <summary>Email + password sign-in. Returns true on success.
    /// Failures land in <see cref="ErrorMessage"/>; the caller doesn't
    /// need to inspect exceptions.</summary>
    public async Task<bool> SignInWithEmailAsync(CancellationToken ct = default)
    {
        if (!CanSubmit) return false;
        return await RunAsync(() => _auth.SignInWithEmailAsync(
            _email.Trim(), _password, ct));
    }

    /// <summary>Google OAuth sign-in. Same success/error contract as
    /// <see cref="SignInWithEmailAsync"/>.</summary>
    public async Task<bool> SignInWithGoogleAsync(CancellationToken ct = default)
    {
        if (IsLoading) return false;
        return await RunAsync(() => _auth.SignInWithGoogleAsync(ct));
    }

    /// <summary>Wraps an auth call with the IsLoading + ErrorMessage
    /// bookkeeping. Caller hands in the actual SDK call; this owns the
    /// state-machine transitions so the UI bindings see consistent
    /// notifications regardless of which sign-in method ran.</summary>
    private async Task<bool> RunAsync(Func<Task<AuthSession>> call)
    {
        IsLoading = true;
        ErrorMessage = null;
        try
        {
            await call();
            return true;
        }
        catch (OperationCanceledException)
        {
            // User cancelled (e.g., closed Google OAuth popup). Don't
            // surface as an error — the cancellation is the user's
            // intent, not a failure.
            return false;
        }
        catch (Exception ex)
        {
            ErrorMessage = ex.Message;
            return false;
        }
        finally
        {
            IsLoading = false;
        }
    }

    private void Raise(string name)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));
}
