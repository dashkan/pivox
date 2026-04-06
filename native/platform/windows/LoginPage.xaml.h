#pragma once
#include "LoginPage.g.h"

namespace winrt::Pivox::implementation
{
    struct LoginPage : LoginPageT<LoginPage>
    {
        LoginPage();

        void OnPageLoaded(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnSignIn(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnSwitchToRegister(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnGoogleSignIn(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnGitHubSignIn(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnEmailKeyDown(IInspectable const& sender, Microsoft::UI::Xaml::Input::KeyRoutedEventArgs const& e);
        void OnPasswordKeyDown(IInspectable const& sender, Microsoft::UI::Xaml::Input::KeyRoutedEventArgs const& e);
        void OnFormChanged(IInspectable const& sender, IInspectable const& e);

    private:
        void SubmitSignIn();
        void NavigateToMainApp();
        void ShowError(const std::string& message);
        void UpdateButtonState();
        void SetLoading(bool loading);
    };
}

namespace winrt::Pivox::factory_implementation
{
    struct LoginPage : LoginPageT<LoginPage, implementation::LoginPage>
    {
    };
}
