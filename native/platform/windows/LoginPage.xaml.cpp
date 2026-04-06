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

    void LoginPage::OnSignIn(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        // Save rememberMe preference.
        bool remember = RememberMeCheck().IsChecked().GetBoolean();
        App::AppState()->saveBool("rememberMe", remember);

        // Attempt sign-in via AuthService.
        auto email = winrt::to_string(EmailBox().Text());
        auto password = winrt::to_string(PasswordBox().Password());
        auto result = App::AuthService()->signInWithEmail(email, password);

        if (!result.ok())
        {
            ErrorBar().Message(winrt::to_hstring(result.errorMessage));
            ErrorBar().IsOpen(true);
            return;
        }

        // Navigate to main app.
        NavigateToMainApp();
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
