#pragma once
#include "App.xaml.g.h"

namespace winrt::Pivox::implementation
{
    struct App : AppT<App>
    {
        App();

        void OnLaunched(Microsoft::UI::Xaml::LaunchActivatedEventArgs const&);

    private:
        void WatchForAuthAndComplete();
        winrt::Microsoft::UI::Xaml::Window m_window{ nullptr };
        std::string m_delegatedSessionCode;
        bool m_isDelegatedAuth = false;
    };
}
