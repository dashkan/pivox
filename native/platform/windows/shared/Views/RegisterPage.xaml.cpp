#include "pch.h"
#include "RegisterPage.xaml.h"
#include "RegisterPage.g.cpp"
#include "PivoxServices.h"

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

    void RegisterPage::SetLoading(bool loading)
    {
        EmailBox().IsEnabled(!loading);
        DisplayNameBox().IsEnabled(!loading);
        PasswordBox().IsEnabled(!loading);
        ConfirmPasswordBox().IsEnabled(!loading);
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

        SetLoading(true);

        auto dispatcher = this->DispatcherQueue();
        auto weakThis = get_weak();

        pivox::PivoxServices::authService()->createAccountAsync(email, password, displayName,
            [dispatcher, weakThis](pivox::AuthResult result) {
                dispatcher.TryEnqueue([weakThis, result]() {
                    if (auto strongThis = weakThis.get())
                    {
                        strongThis->SetLoading(false);
                        if (!result.ok())
                        {
                            strongThis->ShowError(result.errorMessage);
                            return;
                        }
                        strongThis->NavigateToMainApp();
                    }
                });
            });
    }

    void RegisterPage::OnGoogleSignIn(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
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
                        if (result.ok())
                        {
                            strongThis->NavigateToMainApp();
                        }
                        else if (!result.errorMessage.empty())
                        {
                            strongThis->ShowError(result.errorMessage);
                        }
                    }
                });
            });
    }

    void RegisterPage::OnGitHubSignIn(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        auto result = pivox::PivoxServices::authService()->validateGitHubSignIn();
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
