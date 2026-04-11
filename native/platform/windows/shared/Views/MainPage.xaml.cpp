#include "pch.h"
#include "MainPage.xaml.h"
#if __has_include("MainPage.g.cpp")
#include "MainPage.g.cpp"
#endif
#include "PivoxServices.h"

namespace winrt::Pivox::implementation
{
    MainPage::MainPage()
    {
        InitializeComponent();

        if (NavView().MenuItems().Size() > 0)
        {
            NavView().SelectedItem(NavView().MenuItems().GetAt(0));
        }
    }

    void MainPage::OnNavSelectionChanged(
        winrt::Microsoft::UI::Xaml::Controls::NavigationView const&,
        winrt::Microsoft::UI::Xaml::Controls::NavigationViewSelectionChangedEventArgs const& args)
    {
        if (auto selectedItem = args.SelectedItem())
        {
            auto navItem = selectedItem.as<winrt::Microsoft::UI::Xaml::Controls::NavigationViewItem>();
            auto tag = winrt::unbox_value<winrt::hstring>(navItem.Tag());

            if (tag == L"Profile")
            {
                auto& auth = pivox::PivoxServices::authService();
                if (auth) {
                    auto& user = auth->currentUser();
                    auto displayName = user.displayName.empty() ? "User" : user.displayName;
                    auto email = user.email.empty() ? "" : user.email;
                    ProfileName().Text(winrt::to_hstring(displayName));
                    ProfileEmail().Text(winrt::to_hstring(email));
                    ProfileAvatar().DisplayName(winrt::to_hstring(displayName));
                }

                ContentPanel().Visibility(winrt::Microsoft::UI::Xaml::Visibility::Collapsed);
                ProfilePanel().Visibility(winrt::Microsoft::UI::Xaml::Visibility::Visible);
            }
            else
            {
                ContentPanel().Visibility(winrt::Microsoft::UI::Xaml::Visibility::Visible);
                ProfilePanel().Visibility(winrt::Microsoft::UI::Xaml::Visibility::Collapsed);
                SectionTitle().Text(tag);
            }
        }
    }

    void MainPage::OnSignOut(
        winrt::Windows::Foundation::IInspectable const&,
        winrt::Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        auto& auth = pivox::PivoxServices::authService();
        if (auth) {
            auth->signOut();
        }
        if (auto frame = this->Frame()) {
            frame.Navigate(winrt::xaml_typename<winrt::Pivox::LoginPage>());
        }
    }
}
