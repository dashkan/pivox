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

    void RegisterPage::OnSignUp(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        auto email = winrt::to_string(EmailBox().Text());
        auto displayName = winrt::to_string(DisplayNameBox().Text());
        auto password = winrt::to_string(PasswordBox().Password());
        auto confirm = winrt::to_string(ConfirmPasswordBox().Password());

        // Client-side password match check.
        if (password != confirm)
        {
            ErrorBar().Message(L"Passwords do not match.");
            ErrorBar().IsOpen(true);
            return;
        }

        auto result = App::AuthService()->createAccount(email, password, displayName);

        if (!result.ok())
        {
            ErrorBar().Message(winrt::to_hstring(result.errorMessage));
            ErrorBar().IsOpen(true);
            return;
        }

        NavigateToMainApp();
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
