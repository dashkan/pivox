#pragma once
#include "DelegatedLoginPage.g.h"

namespace winrt::Pivox::implementation
{
    struct DelegatedLoginPage : DelegatedLoginPageT<DelegatedLoginPage>
    {
        DelegatedLoginPage();

        void OnSignIn(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);

    private:
        void StartDelegatedAuth();
        void SetLoading(bool loading);
        void ShowError(const std::string& message);
    };
}

namespace winrt::Pivox::factory_implementation
{
    struct DelegatedLoginPage : DelegatedLoginPageT<DelegatedLoginPage, implementation::DelegatedLoginPage>
    {
    };
}
