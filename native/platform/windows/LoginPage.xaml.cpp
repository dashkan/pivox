#include "pch.h"
#include "LoginPage.xaml.h"
#include "LoginPage.g.cpp"
#include "MainWindow.xaml.h"
#include "App.xaml.h"

namespace winrt::Pivox::implementation
{
    LoginPage::LoginPage()
    {
        InitializeComponent();

        // Restore rememberMe checkbox state.
        auto rememberMe = App::AppState()->loadBool("rememberMe");
        if (rememberMe.has_value())
        {
            RememberMeCheck().IsChecked(rememberMe.value());
        }
    }

    void LoginPage::OnPageLoaded(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        // Auto-focus email field on launch.
        EmailBox().Focus(Microsoft::UI::Xaml::FocusState::Programmatic);
    }

    void LoginPage::OnEmailKeyDown(IInspectable const&, Microsoft::UI::Xaml::Input::KeyRoutedEventArgs const& e)
    {
        if (e.Key() == Windows::System::VirtualKey::Enter)
        {
            // Tab to password field.
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

        // Save rememberMe preference.
        bool remember = RememberMeCheck().IsChecked().GetBoolean();
        App::AppState()->saveBool("rememberMe", remember);

        // Show loading state.
        SetLoading(true);
        ErrorText().Opacity(0);
        ErrorText().Text(L" ");

        auto result = App::AuthService()->signInWithEmail(email, password);

        SetLoading(false);

        if (!result.ok())
        {
            ShowError(result.errorMessage);
            return;
        }

        NavigateToMainApp();
    }

    void LoginPage::OnGoogleSignIn(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        auto result = App::AuthService()->validateGoogleSignIn();
        if (!result.ok())
        {
            ShowError(result.errorMessage);
            return;
        }
        // TODO: Launch OAuth2Manager::RequestAuthWithParamsAsync for Google.
    }

    void LoginPage::OnGitHubSignIn(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        auto result = App::AuthService()->validateGitHubSignIn();
        if (!result.ok())
        {
            ShowError(result.errorMessage);
            return;
        }
        // TODO: Launch OAuth2Manager::RequestAuthWithParamsAsync for GitHub.
    }

    void LoginPage::OnSwitchToRegister(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        if (auto frame = this->Frame())
        {
            frame.Navigate(winrt::xaml_typename<Pivox::RegisterPage>());
        }
    }

    void LoginPage::NavigateToMainApp()
    {
        if (auto frame = this->Frame())
        {
            if (auto authContainer = frame.Parent().as<Microsoft::UI::Xaml::FrameworkElement>())
            {
                if (auto rootGrid = authContainer.Parent().as<Microsoft::UI::Xaml::FrameworkElement>())
                {
                    if (auto panel = rootGrid.as<Microsoft::UI::Xaml::Controls::Panel>())
                    {
                        if (panel.Children().Size() >= 2)
                        {
                            panel.Children().GetAt(0).as<Microsoft::UI::Xaml::UIElement>()
                                .Visibility(Microsoft::UI::Xaml::Visibility::Collapsed);
                            panel.Children().GetAt(1).as<Microsoft::UI::Xaml::UIElement>()
                                .Visibility(Microsoft::UI::Xaml::Visibility::Visible);
                        }
                    }
                }
            }
        }
    }
}
