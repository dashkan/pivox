#include "pch.h"
#include "RegisterPage.xaml.h"
#include "RegisterPage.g.cpp"
#include "App.xaml.h"

namespace winrt::Pivox::implementation
{
    RegisterPage::RegisterPage()
    {
        InitializeComponent();
    }

    void RegisterPage::ShowError(const std::string& message)
    {
        ErrorText().Text(winrt::to_hstring(message));
        ErrorText().Opacity(1);
    }

    void RegisterPage::OnSignUp(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        ErrorText().Opacity(0);
        ErrorText().Text(L" ");

        auto email = winrt::to_string(EmailBox().Text());
        auto displayName = winrt::to_string(DisplayNameBox().Text());
        auto password = winrt::to_string(PasswordBox().Password());
        auto confirm = winrt::to_string(ConfirmPasswordBox().Password());

        if (password != confirm)
        {
            ShowError("Passwords do not match.");
            return;
        }

        auto result = App::AuthService()->createAccount(email, password, displayName);

        if (!result.ok())
        {
            ShowError(result.errorMessage);
            return;
        }

        NavigateToMainApp();
    }

    void RegisterPage::OnGoogleSignIn(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        auto result = App::AuthService()->validateGoogleSignIn();
        if (!result.ok())
        {
            ShowError(result.errorMessage);
            return;
        }

        App::AppState()->saveString("remembered_email", "");

        auto dispatcher = this->DispatcherQueue();
        auto weakThis = get_weak();

        App::AuthService()->signInWithGoogleAsync([dispatcher, weakThis](pivox::AuthResult result) {
            dispatcher.TryEnqueue([weakThis, result]() {
                if (auto strongThis = weakThis.get())
                {
                    if (result.ok())
                    {
                        strongThis->NavigateToMainApp();
                    }
                    else
                    {
                        strongThis->ShowError(result.errorMessage);
                    }
                }
            });
        });
    }

    void RegisterPage::OnGitHubSignIn(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        auto result = App::AuthService()->validateGitHubSignIn();
        if (!result.ok())
        {
            ShowError(result.errorMessage);
        }
    }

    void RegisterPage::OnSwitchToLogin(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        if (auto frame = this->Frame())
        {
            frame.GoBack();
        }
    }

    void RegisterPage::NavigateToMainApp()
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
