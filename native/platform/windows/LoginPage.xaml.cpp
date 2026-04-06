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

        // Remember Me: pre-fill email if previously saved.
        auto savedEmail = App::AppState()->loadString("remembered_email");
        if (savedEmail.has_value() && !savedEmail->empty())
        {
            EmailBox().Text(winrt::to_hstring(savedEmail.value()));
            RememberMeCheck().IsChecked(true);
        }
    }

    void LoginPage::OnPageLoaded(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        // Auto-focus: if email is pre-filled, focus password; otherwise focus email.
        auto savedEmail = App::AppState()->loadString("remembered_email");
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

        bool remember = RememberMeCheck().IsChecked().GetBoolean();

        SetLoading(true);
        ErrorText().Opacity(0);
        ErrorText().Text(L" ");

        auto dispatcher = this->DispatcherQueue();
        auto weakThis = get_weak();

        App::AuthService()->signInWithEmailAsync(email, password,
            [dispatcher, weakThis, remember, email](pivox::AuthResult result) {
                dispatcher.TryEnqueue([weakThis, result, remember, email]() {
                    if (auto strongThis = weakThis.get())
                    {
                        strongThis->SetLoading(false);
                        if (!result.ok())
                        {
                            strongThis->ShowError(result.errorMessage);
                            return;
                        }

                        if (remember)
                        {
                            App::AppState()->saveString("remembered_email", email);
                        }
                        else
                        {
                            App::AppState()->saveString("remembered_email", "");
                        }

                        strongThis->NavigateToMainApp();
                    }
                });
            });
    }

    void LoginPage::OnGoogleSignIn(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        auto result = App::AuthService()->validateGoogleSignIn();
        if (!result.ok())
        {
            ShowError(result.errorMessage);
            return;
        }

        App::AppState()->saveString("remembered_email", "");
        SetLoading(true);

        auto windowId = this->XamlRoot().ContentIslandEnvironment().AppWindowId();
        auto dispatcher = this->DispatcherQueue();
        auto weakThis = get_weak();

        App::AuthService()->signInWithGoogleAsync(windowId.Value,
            [dispatcher, weakThis](pivox::AuthResult result) {
                dispatcher.TryEnqueue([weakThis, result]() {
                    if (auto strongThis = weakThis.get())
                    {
                        strongThis->SetLoading(false);
                        if (result.ok())
                        {
                            strongThis->NavigateToMainApp();
                        }
                        else if (!result.errorMessage.empty())
                        {
                            strongThis->ShowError(result.errorMessage);
                        }
                        // Silent cancel — just re-enable inputs.
                    }
                });
            });
    }

    void LoginPage::OnGitHubSignIn(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        auto result = App::AuthService()->validateGitHubSignIn();
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
