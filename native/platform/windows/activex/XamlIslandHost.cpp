#include "pch.h"
#include "XamlIslandHost.h"
#include "PivoxServices.h"

using namespace winrt::Microsoft::UI::Xaml::Hosting;

namespace pivox {

XamlIslandHost::XamlIslandHost() = default;

XamlIslandHost::~XamlIslandHost() {
    Shutdown();
}

HRESULT XamlIslandHost::Initialize(HWND parentHwnd) {
    if (initialized_) return S_OK;
    if (!parentHwnd || !IsWindow(parentHwnd)) return E_INVALIDARG;

    HRESULT hr = InitializeWindowsAppSDK();
    if (FAILED(hr)) return hr;

    hr = CreateXamlIsland(parentHwnd);
    if (FAILED(hr)) return hr;

    initialized_ = true;
    return S_OK;
}

HRESULT XamlIslandHost::InitializeWindowsAppSDK() {
    try {
        // Framework-dependent: requires Windows App SDK runtime installed.
        auto result = winrt::Microsoft::Windows::ApplicationModel::WindowsAppRuntime::
            DeploymentManager::GetStatus();
        if (result.Status() != winrt::Microsoft::Windows::ApplicationModel::
            WindowsAppRuntime::DeploymentStatus::Ok)
        {
            // Attempt initialization — will fail if runtime is not present.
            auto initResult = winrt::Microsoft::Windows::ApplicationModel::
                WindowsAppRuntime::DeploymentManager::Initialize();
            if (initResult.Status() != winrt::Microsoft::Windows::ApplicationModel::
                WindowsAppRuntime::DeploymentStatus::Ok)
            {
                return E_FAIL;
            }
        }
        return S_OK;
    } catch (const winrt::hresult_error& ex) {
        return ex.code();
    }
}

HRESULT XamlIslandHost::CreateXamlIsland(HWND parentHwnd) {
    try {
        xamlSource_ = DesktopWindowXamlSource();

        // Get the interop interface to attach to a parent HWND.
        auto interop = xamlSource_.as<IDesktopWindowXamlSourceNative2>();

        HRESULT hr = interop->AttachToWindow(parentHwnd);
        if (FAILED(hr)) return hr;

        hr = interop->get_WindowHandle(&childHwnd_);
        if (FAILED(hr)) return hr;

        // Size the island to fill the parent.
        RECT rc;
        GetClientRect(parentHwnd, &rc);
        SetWindowPos(childHwnd_, nullptr, 0, 0,
                     rc.right - rc.left, rc.bottom - rc.top,
                     SWP_NOZORDER | SWP_SHOWWINDOW);

        return S_OK;
    } catch (const winrt::hresult_error& ex) {
        return ex.code();
    }
}

HRESULT XamlIslandHost::NavigateTo(const wchar_t* pageName) {
    if (!initialized_ || !xamlSource_) return E_NOT_VALID_STATE;

    try {
        // Create a Frame and navigate to the requested page type.
        winrt::Microsoft::UI::Xaml::Controls::Frame frame;
        winrt::Windows::UI::Xaml::Interop::TypeName pageType;
        pageType.Name = pageName;
        pageType.Kind = winrt::Windows::UI::Xaml::Interop::TypeKind::Metadata;
        frame.Navigate(pageType);

        xamlSource_.Content(frame);
        return S_OK;
    } catch (const winrt::hresult_error& ex) {
        return ex.code();
    }
}

void XamlIslandHost::Resize(int width, int height) {
    if (childHwnd_) {
        SetWindowPos(childHwnd_, nullptr, 0, 0, width, height,
                     SWP_NOZORDER | SWP_NOMOVE);
    }
}

bool XamlIslandHost::ProcessMessage(const MSG& msg) {
    if (!xamlSource_) return false;

    try {
        auto interop = xamlSource_.as<IDesktopWindowXamlSourceNative2>();
        BOOL handled = FALSE;
        interop->PreTranslateMessage(&msg, &handled);
        return handled != FALSE;
    } catch (...) {
        return false;
    }
}

void XamlIslandHost::Shutdown() {
    if (xamlSource_) {
        xamlSource_.Close();
        xamlSource_ = nullptr;
    }
    childHwnd_ = nullptr;
    initialized_ = false;
}

} // namespace pivox
