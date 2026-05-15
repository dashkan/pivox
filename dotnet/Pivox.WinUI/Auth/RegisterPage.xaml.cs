using System.ComponentModel;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Pivox.Shared.Auth;
using Pivox.Shared.Navigation;

namespace Pivox.Auth;

/// <summary>
/// Sign-up form. Four fields (email, display name, password, confirm
/// password) + social buttons. Mirrors macOS
/// <c>RegisterViewController</c>.
/// </summary>
public sealed partial class RegisterPage : Page
{
    private readonly RegisterViewModel _vm;
    private readonly AppRouter _router;

    public RegisterPage(RegisterViewModel vm, AppRouter router)
    {
        InitializeComponent();
        _vm = vm;
        _router = router;
        _vm.PropertyChanged += OnViewModelChanged;
        ApplyState();
    }

    // ── field bindings ───────────────────────────────────────────

    private void EmailBox_TextChanged(object sender, TextChangedEventArgs e)
        => _vm.Email = EmailBox.Text;

    private void DisplayNameBox_TextChanged(object sender, TextChangedEventArgs e)
        => _vm.DisplayName = DisplayNameBox.Text;

    private void PasswordBox_PasswordChanged(object sender, RoutedEventArgs e)
        => _vm.Password = PasswordBox.Password;

    private void ConfirmPasswordBox_PasswordChanged(object sender, RoutedEventArgs e)
        => _vm.ConfirmPassword = ConfirmPasswordBox.Password;

    // ── button handlers ──────────────────────────────────────────

    private async void CreateAccountButton_Click(object sender, RoutedEventArgs e)
        => await _vm.CreateAccountAsync();

    private async void GoogleButton_Click(object sender, RoutedEventArgs e)
        => await _vm.SignInWithGoogleAsync();

    private async void GitHubButton_Click(object sender, RoutedEventArgs e)
        => await _vm.SignInWithGitHubAsync();

    private void SignInButton_Click(object sender, RoutedEventArgs e)
        => _router.Pop();

    // ── state sync ───────────────────────────────────────────────

    private void OnViewModelChanged(object? sender, PropertyChangedEventArgs e)
        => ApplyState();

    private void ApplyState()
    {
        var loading = _vm.IsLoading;

        CreateAccountButton.IsEnabled = _vm.CanSubmit;
        GoogleButton.IsEnabled = !loading;
        GitHubButton.IsEnabled = !loading;
        EmailBox.IsEnabled = !loading;
        DisplayNameBox.IsEnabled = !loading;
        PasswordBox.IsEnabled = !loading;
        ConfirmPasswordBox.IsEnabled = !loading;

        ErrorLabel.Text = _vm.ErrorMessage ?? " ";
    }
}
