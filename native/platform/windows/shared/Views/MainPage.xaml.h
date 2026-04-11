#pragma once
#include "MainPage.g.h"

namespace winrt::Pivox::implementation
{
    struct MainPage : MainPageT<MainPage>
    {
        MainPage();

        void OnNavSelectionChanged(
            winrt::Microsoft::UI::Xaml::Controls::NavigationView const& sender,
            winrt::Microsoft::UI::Xaml::Controls::NavigationViewSelectionChangedEventArgs const& args);
        void OnSignOut(
            winrt::Windows::Foundation::IInspectable const& sender,
            winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);
    };
}

namespace winrt::Pivox::factory_implementation
{
    struct MainPage : MainPageT<MainPage, implementation::MainPage>
    {
    };
}
