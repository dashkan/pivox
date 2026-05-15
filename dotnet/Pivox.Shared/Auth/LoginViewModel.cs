using System.ComponentModel;

namespace Pivox.Shared.Auth;

/// <summary>
/// View-model for the sign-in surface. Drives the email-first two-step
/// flow that mirrors SwiftUI <c>LoginView</c>:
///
/// <list type="number">
/// <item><b>Step 1 — email only.</b> Primary button reads "Continue".
///   On submit, <see cref="SubmitEmailStepAsync"/> calls
///   <see cref="IAuthService.ResolveSsoProviderAsync"/>. If the
///   email's domain has an SSO provider, in-band invokes
///   <see cref="IAuthService.SignInWithSsoAsync"/> and returns true
///   on success (router will swap to Shell via auth listener). If
///   no provider, flips <see cref="DidResolveAsPassword"/> to true
///   so the UI reveals the password field, and returns false.</item>
/// <item><b>Step 2 — password revealed.</b> Primary button reads
///   "Sign In". Submit calls <see cref="SignInWithEmailAsync"/>.</item>
/// </list>
///
/// Editing the email after step 1 collapses back to step 1 — the
/// email's resolution may now be different.
///
/// Threading: <see cref="IAuthService"/> dispatches its SDK work
/// internally; await continuations resume on the calling (UI)
/// thread via <see cref="SynchronizationContext"/>. All property
/// mutations happen on the UI thread.
/// </summary>
public sealed class LoginViewModel : INotifyPropertyChanged
{
    private readonly IAuthService _auth;

    private string _email = "";
    private string _password = "";
    private bool _isLoading;
    private bool _didResolveAsPassword;
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
            // Editing the email after a step-1 resolve invalidates the
            // SSO/password decision — the new email may resolve
            // differently. Roll back so the next submit re-resolves.
            // Also clear the password since the user typed it under
            // the assumption of the previous email.
            //
            // Field mutation order: assign the backing fields first
            // (`_didResolveAsPassword = false`, `_password = ""`)
            // BEFORE raising any PropertyChanged. ApplyState in
            // LoginViewController observes the post-mutation snapshot
            // when either event fires and clears the password field's
            // StringValue via the !revealed sync path. Splitting the
            // mutation/raise order would let the view re-read
            // mid-flight and miss either change.
            if (_didResolveAsPassword)
            {
                _didResolveAsPassword = false;
                _password = "";
                Raise(nameof(DidResolveAsPassword));
                Raise(nameof(Password));
            }
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

    /// <summary>True while a sign-in request is in flight. UI disables
    /// inputs and shows progress when true.</summary>
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

    /// <summary>True once step 1 has resolved this email as a
    /// password-auth account (no SSO provider). UI reveals the password
    /// field and morphs the primary button label to "Sign In".</summary>
    public bool DidResolveAsPassword
    {
        get => _didResolveAsPassword;
        private set
        {
            if (_didResolveAsPassword == value) return;
            _didResolveAsPassword = value;
            Raise(nameof(DidResolveAsPassword));
            Raise(nameof(CanSubmit));
            Raise(nameof(PrimaryButtonTitle));
        }
    }

    /// <summary>User-facing error from the most recent attempt. Cleared
    /// on each new submission. Null = no error visible.</summary>
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

    /// <summary>"Continue" in step 1, "Sign In" once the email
    /// resolved as password auth. Bind the primary button's label
    /// here so the morph happens automatically.</summary>
    public string PrimaryButtonTitle
        => _didResolveAsPassword ? "Sign In" : "Continue";

    /// <summary>Primary button enable gate.
    /// <list type="bullet">
    /// <item>Step 1: email non-empty.</item>
    /// <item>Step 2: both fields non-empty.</item>
    /// <item>Always: not loading.</item>
    /// </list></summary>
    public bool CanSubmit
    {
        get
        {
            if (IsLoading) return false;
            if (_didResolveAsPassword)
            {
                return !string.IsNullOrWhiteSpace(_email)
                       && !string.IsNullOrEmpty(_password);
            }
            return !string.IsNullOrWhiteSpace(_email);
        }
    }

    /// <summary>
    /// Submit step 1: resolve SSO vs password from the email. Returns
    /// true if the resolution kicked off an SSO sign-in that completed
    /// successfully (auth state listener will already have triggered
    /// the route transition). Returns false if the email resolved as
    /// password — the UI should now reveal the password field and the
    /// user will resubmit via <see cref="SignInWithEmailAsync"/>.
    /// </summary>
    public async Task<bool> SubmitEmailStepAsync(CancellationToken ct = default)
    {
        if (!CanSubmit || _didResolveAsPassword) return false;

        IsLoading = true;
        ErrorMessage = null;
        try
        {
            var trimmed = _email.Trim();
            string? providerId = null;
            try
            {
                providerId = await _auth.ResolveSsoProviderAsync(trimmed, ct);
            }
            catch (Exception ex)
            {
                // Resolver failed (network, server error). Match the
                // SwiftUI behavior: surface a generic "couldn't reach"
                // message and stay in step 1. Log the underlying
                // failure so devs can see whether it was DNS, HTTP
                // 5xx, JSON parse, etc. — never silently swallow per
                // dotnet/CLAUDE.md guidance.
                Console.Error.WriteLine(
                    $"[Auth] resolveProvider failed: {ex.GetType().Name}: {ex.Message}");
                ErrorMessage = "Couldn't reach the sign-in service. Try again.";
                return false;
            }

            if (!string.IsNullOrEmpty(providerId))
            {
                // SSO path — broker flow, no password reveal.
                await _auth.SignInWithSsoAsync(providerId, trimmed, ct);
                return true;
            }

            // No SSO provider — reveal password, resubmit through
            // SignInWithEmailAsync.
            DidResolveAsPassword = true;
            return false;
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

    /// <summary>Step 2 submit: password sign-in. Returns true on success.</summary>
    public async Task<bool> SignInWithEmailAsync(CancellationToken ct = default)
    {
        if (!CanSubmit || !_didResolveAsPassword) return false;
        return await RunAsync(() => _auth.SignInWithEmailAsync(
            _email.Trim(), _password, ct));
    }

    /// <summary>Google OAuth — bypasses the two-step flow.</summary>
    public async Task<bool> SignInWithGoogleAsync(CancellationToken ct = default)
        => await RunAsync(() => _auth.SignInWithGoogleAsync(ct));

    /// <summary>GitHub OAuth — bypasses the two-step flow.</summary>
    public async Task<bool> SignInWithGitHubAsync(CancellationToken ct = default)
        => await RunAsync(() => _auth.SignInWithGitHubAsync(ct));

    /// <summary>Fire the password-reset email for the currently-typed
    /// email. Sets <see cref="ErrorMessage"/> on failure; returns true
    /// when the request was accepted.</summary>
    public async Task<bool> SendPasswordResetAsync(CancellationToken ct = default)
    {
        if (IsLoading) return false;
        if (string.IsNullOrWhiteSpace(_email))
        {
            ErrorMessage = "Enter your email above first.";
            return false;
        }

        IsLoading = true;
        ErrorMessage = null;
        try
        {
            await _auth.SendPasswordResetAsync(_email.Trim(), ct);
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
