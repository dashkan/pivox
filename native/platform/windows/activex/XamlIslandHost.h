#pragma once
#include "pch.h"

namespace pivox {

class XamlIslandHost {
public:
    XamlIslandHost();
    ~XamlIslandHost();

    XamlIslandHost(const XamlIslandHost&) = delete;
    XamlIslandHost& operator=(const XamlIslandHost&) = delete;

    HRESULT Initialize(HWND parentHwnd);
    HRESULT NavigateTo(const wchar_t* pageName);
    void Resize(int width, int height);
    void Shutdown();

    bool IsInitialized() const { return initialized_; }

private:
    HRESULT InitializeWindowsAppSDK();

    winrt::Microsoft::UI::Dispatching::DispatcherQueueController dispatcherController_{ nullptr };
    winrt::Microsoft::UI::Xaml::Hosting::WindowsXamlManager xamlManager_{ nullptr };
    winrt::Microsoft::UI::Xaml::Application app_{ nullptr };
    winrt::Microsoft::UI::Xaml::Hosting::DesktopWindowXamlSource xamlSource_{ nullptr };
    HWND parentHwnd_ = nullptr;
    bool initialized_ = false;
};

} // namespace pivox
