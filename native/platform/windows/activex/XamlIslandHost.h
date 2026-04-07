#pragma once
#include "pch.h"

namespace pivox {

/// Manages the DesktopWindowXamlSource lifecycle for hosting
/// WinUI 3 content inside the ActiveX control's HWND.
class XamlIslandHost {
public:
    XamlIslandHost();
    ~XamlIslandHost();

    XamlIslandHost(const XamlIslandHost&) = delete;
    XamlIslandHost& operator=(const XamlIslandHost&) = delete;

    /// Initialize Windows App SDK and create the XAML Island.
    /// @param parentHwnd The ActiveX control's in-place HWND.
    /// @return S_OK on success, or an error HRESULT.
    HRESULT Initialize(HWND parentHwnd);

    /// Navigate the hosted content to a named page from the shared library.
    /// @param pageName Fully qualified WinRT type name (e.g., "Pivox.LoginPage").
    HRESULT NavigateTo(const wchar_t* pageName);

    /// Resize the XAML Island to match the control's client area.
    void Resize(int width, int height);

    /// Process a Windows message for the XAML Island dispatcher.
    /// @return true if the message was handled by the XAML Island.
    bool ProcessMessage(const MSG& msg);

    /// Tear down the XAML Island and release all resources.
    void Shutdown();

    bool IsInitialized() const { return initialized_; }

private:
    HRESULT InitializeWindowsAppSDK();
    HRESULT CreateXamlIsland(HWND parentHwnd);

    winrt::Microsoft::UI::Xaml::Hosting::DesktopWindowXamlSource xamlSource_{ nullptr };
    HWND childHwnd_ = nullptr;
    bool initialized_ = false;
};

} // namespace pivox
