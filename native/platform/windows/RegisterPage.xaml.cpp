#include "pch.h"
#include "RegisterPage.xaml.h"
#include "RegisterPage.g.cpp"

namespace winrt::Pivox::implementation
{
    RegisterPage::RegisterPage()
    {
        InitializeComponent();
    }

    void RegisterPage::OnSignUp(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        // Placeholder — navigate to main app (same as login).
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

    void RegisterPage::OnSwitchToLogin(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        if (auto frame = this->Frame())
        {
            frame.GoBack();
        }
    }
}
