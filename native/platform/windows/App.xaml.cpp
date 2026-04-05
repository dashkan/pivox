#include "pch.h"
#include "App.xaml.h"
#include "MainWindow.xaml.h"

// NOTE: App.xaml.cpp does NOT include App.g.cpp.
// See docs/dev/winui3-cmake-guide.md constraint #3.

namespace winrt::Pivox::implementation
{
    App::App()
    {
#if defined _DEBUG && !defined DISABLE_XAML_GENERATED_BREAK_ON_UNHANDLED_EXCEPTION
        UnhandledException([this](IInspectable const&, Microsoft::UI::Xaml::UnhandledExceptionEventArgs const& e)
        {
            if (IsDebuggerPresent())
            {
                auto errorMessage = e.Message();
                __debugbreak();
            }
        });
#endif
    }

    void App::OnLaunched([[maybe_unused]] Microsoft::UI::Xaml::LaunchActivatedEventArgs const& e)
    {
        m_window = winrt::make<MainWindow>();
        m_window.Activate();
    }
}
