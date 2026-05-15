using System.ComponentModel;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Pivox.Shared.Auth;
using Pivox.Shared.Navigation;

namespace Pivox.Auth;

/// <summary>
/// Sign-in page. Drives the two-step email-first flow via
/// <see cref="LoginViewModel"/>:
///
/// Step 1 — email only, primary button "Continue". Resolves SSO vs
/// password. Step 2 — password revealed, primary button "Sign In".
///
/// Mirrors macOS <c>LoginViewController</c>.
/// </summary>
public sealed partial class LoginPage : Page
{
    private readonly LoginViewModel _vm;
    private readonly AppRouter _router;
    private readonly RememberedEmail _rememberedEmail;
    private bool _previousDidResolveAsPassword;

    public LoginPage(LoginViewModel vm, AppRouter router, RememberedEmail rememberedEmail)
    {
        InitializeComponent();
        _vm = vm;
        _router = router;
        _rememberedEmail = rememberedEmail;

        // Restore remembered email.
        var remembered = _rememberedEmail.Get();
        if (!string.IsNullOrEmpty(remembered))
        {
            EmailBox.Text = remembered;
            _vm.Email = remembered;
            RememberMeCheckbox.IsChecked = true;
        }

        _vm.PropertyChanged += OnViewModelChanged;
        ApplyState();
    }

    // ── field bindings ───────────────────────────────────────────

    private void EmailBox_TextChanged(object sender, TextChangedEventArgs e)
        => _vm.Email = EmailBox.Text;

    private void PasswordBox_PasswordChanged(object sender, RoutedEventArgs e)
        => _vm.Password = PasswordBox.Password;

    // ── button handlers ──────────────────────────────────────────

    private async void PrimaryButton_Click(object sender, RoutedEventArgs e)
    {
        bool success;
        if (_vm.DidResolveAsPassword)
        {
            success = await _vm.SignInWithEmailAsync();
        }
        else
        {
            success = await _vm.SubmitEmailStepAsync();
        }

        if (success) PersistRememberedEmail();
    }

    private async void GoogleButton_Click(object sender, RoutedEventArgs e)
    {
        if (await _vm.SignInWithGoogleAsync()) PersistRememberedEmail();
    }

    private async void GitHubButton_Click(object sender, RoutedEventArgs e)
    {
        if (await _vm.SignInWithGitHubAsync()) PersistRememberedEmail();
    }

    private async void ForgotPasswordButton_Click(object sender, RoutedEventArgs e)
    {
        var requestedEmail = _vm.Email;
        var sent = await _vm.SendPasswordResetAsync();
        if (sent)
        {
            var dialog = new ContentDialog
            {
                Title = "Check your email",
                Content = $"If an account exists for {requestedEmail}, you'll receive a "
                          + "password reset link shortly.",
                CloseButtonText = "OK",
                XamlRoot = XamlRoot,
            };
            await dialog.ShowAsync();
        }
    }

    private void CreateOneButton_Click(object sender, RoutedEventArgs e)
        => _router.Push(new AppRoute.Register());

    // ── state sync ───────────────────────────────────────────────

    private void OnViewModelChanged(object? sender, PropertyChangedEventArgs e)
        => ApplyState();

    private void ApplyState()
    {
        var loading = _vm.IsLoading;
        var revealed = _vm.DidResolveAsPassword;

        PrimaryButton.Content = _vm.PrimaryButtonTitle;
        PrimaryButton.IsEnabled = _vm.CanSubmit;

        PasswordBox.Visibility = revealed ? Visibility.Visible : Visibility.Collapsed;
        ForgotPasswordButton.Visibility = revealed ? Visibility.Visible : Visibility.Collapsed;

        // Clear password field when collapsing back to step 1.
        if (!revealed && PasswordBox.Password != _vm.Password)
            PasswordBox.Password = _vm.Password;

        // Focus password field when step 2 reveals.
        if (revealed && !_previousDidResolveAsPassword)
            PasswordBox.Focus(FocusState.Programmatic);
        _previousDidResolveAsPassword = revealed;

        EmailBox.IsEnabled = !loading;
        PasswordBox.IsEnabled = !loading;
        GoogleButton.IsEnabled = !loading;
        GitHubButton.IsEnabled = !loading;
        RememberMeCheckbox.IsEnabled = !loading;
        ForgotPasswordButton.IsEnabled = !loading;

        ErrorLabel.Text = _vm.ErrorMessage ?? " ";
    }

    private void PersistRememberedEmail()
    {
        if (RememberMeCheckbox.IsChecked == true)
            _rememberedEmail.Set(_vm.Email.Trim());
        else
            _rememberedEmail.Set(null);
    }
}
