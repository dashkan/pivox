using System.ComponentModel;

namespace Pivox.Shared.Auth;

/// <summary>
/// View-model for the sign-up surface. Same INotifyPropertyChanged
/// shape as <see cref="LoginViewModel"/> so the macOS AppKit binding
/// and the WinUI XAML binding consume identical state.
///
/// Fields mirror SwiftUI's <c>RegisterView</c>: email, display name,
/// password, confirm password. Client-side validates that passwords
/// match before calling Firebase — the SDK doesn't enforce match,
/// only minimum length.
/// </summary>
public sealed class RegisterViewModel : INotifyPropertyChanged
{
    private readonly IAuthService _auth;

    private string _email = "";
    private string _displayName = "";
    private string _password = "";
    private string _confirmPassword = "";
    private bool _isLoading;
    private string? _errorMessage;

    public RegisterViewModel(IAuthService auth)
    {
        _auth = auth ?? throw new ArgumentNullException(nameof(auth));
    }

    public event PropertyChangedEventHandler? PropertyChanged;

    public string Email
    {
        get => _email;
        set { if (_email == value) return; _email = value; Raise(nameof(Email)); Raise(nameof(CanSubmit)); }
    }

    public string DisplayName
    {
        get => _displayName;
        set { if (_displayName == value) return; _displayName = value; Raise(nameof(DisplayName)); Raise(nameof(CanSubmit)); }
    }

    public string Password
    {
        get => _password;
        set { if (_password == value) return; _password = value; Raise(nameof(Password)); Raise(nameof(CanSubmit)); }
    }

    public string ConfirmPassword
    {
        get => _confirmPassword;
        set { if (_confirmPassword == value) return; _confirmPassword = value; Raise(nameof(ConfirmPassword)); Raise(nameof(CanSubmit)); }
    }

    public bool IsLoading
    {
        get => _isLoading;
        private set { if (_isLoading == value) return; _isLoading = value; Raise(nameof(IsLoading)); Raise(nameof(CanSubmit)); }
    }

    public string? ErrorMessage
    {
        get => _errorMessage;
        private set { if (_errorMessage == value) return; _errorMessage = value; Raise(nameof(ErrorMessage)); }
    }

    /// <summary>Submit gate: all four fields filled + not loading.
    /// Password-match validation runs at submit time so the user sees
    /// a clear error message rather than a permanently-disabled
    /// button.</summary>
    public bool CanSubmit
        => !IsLoading
           && !string.IsNullOrWhiteSpace(_email)
           && !string.IsNullOrWhiteSpace(_displayName)
           && !string.IsNullOrEmpty(_password)
           && !string.IsNullOrEmpty(_confirmPassword);

    /// <summary>Create the account. Returns true on success.</summary>
    public async Task<bool> CreateAccountAsync(CancellationToken ct = default)
    {
        if (!CanSubmit) return false;

        if (_password != _confirmPassword)
        {
            ErrorMessage = "Passwords do not match.";
            return false;
        }

        IsLoading = true;
        ErrorMessage = null;
        try
        {
            await _auth.CreateAccountAsync(_email.Trim(), _password, _displayName.Trim(), ct);
            return true;
        }
        catch (OperationCanceledException) { return false; }
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

    /// <summary>Social sign-up shortcut: skips the form, runs OAuth.
    /// On success the user lands signed-in just like the Login flow.</summary>
    public async Task<bool> SignInWithGoogleAsync(CancellationToken ct = default)
        => await RunAsync(() => _auth.SignInWithGoogleAsync(ct));

    public async Task<bool> SignInWithGitHubAsync(CancellationToken ct = default)
        => await RunAsync(() => _auth.SignInWithGitHubAsync(ct));

    private async Task<bool> RunAsync(Func<Task<AuthSession>> call)
    {
        if (IsLoading) return false;
        IsLoading = true;
        ErrorMessage = null;
        try
        {
            await call();
            return true;
        }
        catch (OperationCanceledException) { return false; }
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
