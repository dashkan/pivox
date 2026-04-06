#pragma once
#include "LoginPage.g.h"

namespace winrt::Pivox::implementation
{
    struct LoginPage : LoginPageT<LoginPage>
    {
        LoginPage();

        void OnSignIn(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnSwitchToRegister(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnGoogleSignIn(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnGitHubSignIn(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);

    private:
        void NavigateToMainApp();
    };
}

namespace winrt::Pivox::factory_implementation
{
    struct LoginPage : LoginPageT<LoginPage, implementation::LoginPage>
    {
    };
}
