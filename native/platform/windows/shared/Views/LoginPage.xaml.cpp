#include "pch.h"
#include "LoginPage.xaml.h"
#include "LoginPage.g.cpp"
#include "PivoxServices.h"

namespace winrt::Pivox::implementation
{
    LoginPage::LoginPage()
    {
        InitializeComponent();

        // Remember Me: pre-fill email if previously saved.
        auto savedEmail = pivox::PivoxServices::appState()->loadString("remembered_email");
        if (savedEmail.has_value() && !savedEmail->empty())
        {
            EmailBox().Text(winrt::to_hstring(savedEmail.value()));
            RememberMeCheck().IsChecked(true);
        }
    }

    void LoginPage::OnPageLoaded(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        // Auto-focus: if email is pre-filled, focus password; otherwise focus email.
        auto savedEmail = pivox::PivoxServices::appState()->loadString("remembered_email");
        if (savedEmail.has_value() && !savedEmail->empty())
        {
            PasswordBox().Focus(Microsoft::UI::Xaml::FocusState::Programmatic);
        }
        else
        {
            EmailBox().Focus(Microsoft::UI::Xaml::FocusState::Programmatic);
        }
    }

    void LoginPage::OnEmailKeyDown(IInspectable const&, Microsoft::UI::Xaml::Input::KeyRoutedEventArgs const& e)
    {
        if (e.Key() == Windows::System::VirtualKey::Enter)
        {
            PasswordBox().Focus(Microsoft::UI::Xaml::FocusState::Programmatic);
            e.Handled(true);
        }
    }

    void LoginPage::OnPasswordKeyDown(IInspectable const&, Microsoft::UI::Xaml::Input::KeyRoutedEventArgs const& e)
    {
        if (e.Key() == Windows::System::VirtualKey::Enter)
        {
            SubmitSignIn();
            e.Handled(true);
        }
    }

    void LoginPage::OnFormChanged(IInspectable const&, IInspectable const&)
    {
        UpdateButtonState();
    }

    void LoginPage::UpdateButtonState()
    {
        bool hasEmail = EmailBox().Text().size() > 0;
        bool hasPassword = PasswordBox().Password().size() > 0;
        SignInButton().IsEnabled(hasEmail && hasPassword);
    }

    void LoginPage::SetLoading(bool loading)
    {
        SignInButton().IsEnabled(!loading);
        EmailBox().IsEnabled(!loading);
        PasswordBox().IsEnabled(!loading);
        RememberMeCheck().IsEnabled(!loading);
        ForgotPasswordLink().IsEnabled(!loading);
        GoogleSignInButton().IsEnabled(!loading);
        GitHubSignInButton().IsEnabled(!loading);
        SwitchToRegisterLink().IsEnabled(!loading);
        SignInSpinner().IsActive(loading);
        SignInSpinner().Visibility(loading
            ? Microsoft::UI::Xaml::Visibility::Visible
            : Microsoft::UI::Xaml::Visibility::Collapsed);
        SignInText().Text(loading ? L"" : L"Sign In");
    }

    void LoginPage::ShowError(const std::string& message)
    {
        ErrorText().Text(winrt::to_hstring(message));
        ErrorText().Opacity(1);
    }

    void LoginPage::OnSignIn(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        SubmitSignIn();
    }

    void LoginPage::SubmitSignIn()
    {
        auto email = winrt::to_string(EmailBox().Text());
        auto password = winrt::to_string(PasswordBox().Password());
        if (email.empty() || password.empty()) return;

        bool remember = RememberMeCheck().IsChecked().GetBoolean();

        SetLoading(true);
        ErrorText().Opacity(0);
        ErrorText().Text(L" ");

        auto dispatcher = this->DispatcherQueue();
        auto weakThis = get_weak();

        pivox::PivoxServices::authService()->signInWithEmailAsync(email, password,
            [dispatcher, weakThis, remember, email](pivox::AuthResult result) {
                dispatcher.TryEnqueue([weakThis, result, remember, email]() {
                    if (auto strongThis = weakThis.get())
                    {
                        strongThis->SetLoading(false);
                        if (!result.ok())
                        {
                            OutputDebugStringA(("[PivoxAuth] signIn failed: " + result.errorMessage + "\n").c_str());
                            strongThis->ShowError(result.errorMessage);
                            return;
                        }

                        if (remember)
                        {
                            pivox::PivoxServices::appState()->saveString("remembered_email", email);
                        }
                        else
                        {
                            pivox::PivoxServices::appState()->saveString("remembered_email", "");
                        }
                    }
                });
            });
    }

    void LoginPage::OnGoogleSignIn(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        auto result = pivox::PivoxServices::authService()->validateGoogleSignIn();
        if (!result.ok())
        {
            ShowError(result.errorMessage);
            return;
        }

        pivox::PivoxServices::appState()->saveString("remembered_email", "");
        SetLoading(true);

        auto windowId = this->XamlRoot().ContentIslandEnvironment().AppWindowId();
        auto dispatcher = this->DispatcherQueue();
        auto weakThis = get_weak();

        pivox::PivoxServices::authService()->signInWithGoogleAsync(windowId.Value,
            [dispatcher, weakThis](pivox::AuthResult result) {
                dispatcher.TryEnqueue([weakThis, result]() {
                    if (auto strongThis = weakThis.get())
                    {
                        strongThis->SetLoading(false);
                        if (!result.ok() && !result.errorMessage.empty())
                        {
                            strongThis->ShowError(result.errorMessage);
                        }
                    }
                });
            });
    }

    void LoginPage::OnGitHubSignIn(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        auto result = pivox::PivoxServices::authService()->validateGitHubSignIn();
        if (!result.ok())
        {
            ShowError(result.errorMessage);
            return;
        }
        // TODO: GitHub OAuth flow — needs client ID registration.
    }

    void LoginPage::OnSwitchToRegister(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        if (auto frame = this->Frame())
        {
            frame.Navigate(winrt::xaml_typename<Pivox::RegisterPage>());
        }
    }

    // Auth state listener (registered by MainWindow) handles navigation.
}
