#pragma once
#include "MainWindow.g.h"

namespace winrt::PivoxApp::implementation
{
    struct MainWindow : MainWindowT<MainWindow>
    {
        MainWindow();

        void OnNavSelectionChanged(
            Microsoft::UI::Xaml::Controls::NavigationView const& sender,
            Microsoft::UI::Xaml::Controls::NavigationViewSelectionChangedEventArgs const& args);
        void OnMenuExit(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnMenuToggleSidebar(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnMenuMinimize(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnMenuMaximize(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OnSignOut(IInspectable const& sender, Microsoft::UI::Xaml::RoutedEventArgs const& e);

    private:
        void ShowAuth();
        void ShowMainApp();
        void SetupWindow();
        void SaveWindowState();

        winrt::event_token m_changedToken;
    };
}

namespace winrt::PivoxApp::factory_implementation
{
    struct MainWindow : MainWindowT<MainWindow, implementation::MainWindow>
    {
    };
}
