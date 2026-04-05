#include "pch.h"
#include "LoginPage.xaml.h"
#include "LoginPage.g.cpp"
#include "MainWindow.xaml.h"

namespace winrt::Pivox::implementation
{
    LoginPage::LoginPage()
    {
        InitializeComponent();
    }

    void LoginPage::OnSignIn(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        // Placeholder — navigate to main app.
        // Walk up: Page -> Frame -> Grid (AuthContainer) -> Grid (root) -> Window.
        if (auto frame = this->Frame())
        {
            if (auto authContainer = frame.Parent().as<Microsoft::UI::Xaml::FrameworkElement>())
            {
                if (auto rootGrid = authContainer.Parent().as<Microsoft::UI::Xaml::FrameworkElement>())
                {
                    // Find the MainContainer and toggle visibility.
                    if (auto panel = rootGrid.as<Microsoft::UI::Xaml::Controls::Panel>())
                    {
                        // AuthContainer = child 0, MainContainer = child 1
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

    void LoginPage::OnSwitchToRegister(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        if (auto frame = this->Frame())
        {
            frame.Navigate(winrt::xaml_typename<Pivox::RegisterPage>());
        }
    }
}
