#pragma once
#include "RegisterPage.g.h"

namespace winrt::Pivox::implementation
{
    struct RegisterPage : RegisterPageT<RegisterPage>
    {
        RegisterPage();

        void OnSignUp(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnSwitchToLogin(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
    };
}

namespace winrt::Pivox::factory_implementation
{
    struct RegisterPage : RegisterPageT<RegisterPage, implementation::RegisterPage>
    {
    };
}
