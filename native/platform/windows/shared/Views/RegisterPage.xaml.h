#pragma once
#include "RegisterPage.g.h"

namespace winrt::Pivox::implementation
{
    struct RegisterPage : RegisterPageT<RegisterPage>
    {
        RegisterPage();

        void OnSignUp(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnSwitchToLogin(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnGoogleSignIn(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnGitHubSignIn(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);

    private:
        void ShowError(const std::string& message);
        void SetLoading(bool loading);
    };
}

namespace winrt::Pivox::factory_implementation
{
    struct RegisterPage : RegisterPageT<RegisterPage, implementation::RegisterPage>
    {
    };
}
