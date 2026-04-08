#include "pch.h"
#include "XamlIslandHost.h"

#include <winrt/Microsoft.UI.Interop.h>
#include <winrt/Microsoft.UI.Xaml.Markup.h>
#include <WindowsAppSDK-VersionInfo.h>
#include <MddBootstrap.h>

namespace pivox {

static bool s_bootstrapInitialized = false;
static bool s_xamlInitialized = false;

static LONG WINAPI CrashHandler(EXCEPTION_POINTERS* ep) {
    wchar_t buf[128];
    swprintf_s(buf, L"SEH crash: 0x%08X at 0x%p",
               ep->ExceptionRecord->ExceptionCode,
               ep->ExceptionRecord->ExceptionAddress);
    MessageBoxW(nullptr, buf, L"PivoxActiveX Crash", MB_OK);
    return EXCEPTION_EXECUTE_HANDLER;
}

XamlIslandHost::XamlIslandHost() = default;

XamlIslandHost::~XamlIslandHost() {
    Shutdown();
}

HRESULT XamlIslandHost::InitializeWindowsAppSDK() {
    if (s_bootstrapInitialized) return S_OK;

    PACKAGE_VERSION minVersion{};
    minVersion.Version = WINDOWSAPPSDK_RUNTIME_VERSION_UINT64;
    HRESULT hr = MddBootstrapInitialize(
        WINDOWSAPPSDK_RELEASE_MAJORMINOR,
        WINDOWSAPPSDK_RELEASE_VERSION_TAG_W,
        minVersion);
    if (FAILED(hr)) return hr;

    s_bootstrapInitialized = true;
    return S_OK;
}

HRESULT XamlIslandHost::Initialize(HWND parentHwnd) {
    if (initialized_) return S_OK;
    if (!parentHwnd || !IsWindow(parentHwnd)) return E_INVALIDARG;
    parentHwnd_ = parentHwnd;

    try {
        // 1. Bootstrap Windows App SDK (process-wide, once).
        HRESULT hr = InitializeWindowsAppSDK();
        if (FAILED(hr)) {
            wchar_t buf[128];
            swprintf_s(buf, L"Bootstrap failed: 0x%08X", hr);
            MessageBoxW(parentHwnd, buf, L"PivoxActiveX", MB_OK);
            return hr;
        }

        // 2. DispatcherQueue (thread-wide, once).
        if (!winrt::Microsoft::UI::Dispatching::DispatcherQueue::GetForCurrentThread()) {
            dispatcherController_ = winrt::Microsoft::UI::Dispatching::
                DispatcherQueueController::CreateOnCurrentThread();
        }

        // 3. Create XAML Island source and initialize with top-level window.
        xamlSource_ = winrt::Microsoft::UI::Xaml::Hosting::DesktopWindowXamlSource();

        HWND topLevelHwnd = GetAncestor(parentHwnd, GA_ROOT);
        if (!topLevelHwnd) topLevelHwnd = parentHwnd;

        auto topLevelWindowId = winrt::Microsoft::UI::GetWindowIdFromWindow(topLevelHwnd);
        xamlSource_.Initialize(topLevelWindowId);

        // 4. Get SiteBridge and parent it.
        auto bridge = xamlSource_.SiteBridge();
        if (!bridge) {
            MessageBoxW(parentHwnd, L"STEP 4 FAIL: SiteBridge is null", L"PivoxActiveX", MB_OK);
            return E_FAIL;
        }

        // Don't call SetParent — it closes the bridge's internal state.
        // The bridge is already a child of the top-level window via Initialize(windowId).
        // Just position it relative to our control's HWND.
        RECT rc;
        GetClientRect(parentHwnd, &rc);

        // Map our client rect to screen coordinates for the bridge.
        POINT pt = { 0, 0 };
        ClientToScreen(parentHwnd, &pt);
        HWND topLevelHwnd2 = GetAncestor(parentHwnd, GA_ROOT);
        ScreenToClient(topLevelHwnd2, &pt);

        bridge.MoveAndResize({ pt.x, pt.y, rc.right - rc.left, rc.bottom - rc.top });
        bridge.Show();

        // Don't set content yet — test empty island rendering first.
        OutputDebugStringW(L"[PivoxActiveX] XAML Island initialized (empty)\n");

        initialized_ = true;
        return S_OK;
    } catch (const winrt::hresult_error& ex) {
        wchar_t buf[256];
        swprintf_s(buf, L"XAML init: 0x%08X\n%s",
                   static_cast<int32_t>(ex.code()), ex.message().c_str());
        MessageBoxW(parentHwnd, buf, L"PivoxActiveX", MB_OK);
        return ex.code();
    }
}

HRESULT XamlIslandHost::NavigateTo(const wchar_t* pageName) {
    if (!initialized_ || !xamlSource_) return E_NOT_VALID_STATE;

    try {
        winrt::Microsoft::UI::Xaml::Controls::Frame frame;
        winrt::Windows::UI::Xaml::Interop::TypeName pageType;
        pageType.Name = pageName;
        pageType.Kind = winrt::Windows::UI::Xaml::Interop::TypeKind::Metadata;
        frame.Navigate(pageType);

        xamlSource_.Content(frame);
        return S_OK;
    } catch (const winrt::hresult_error& ex) {
        wchar_t buf[256];
        swprintf_s(buf, L"NavigateTo: 0x%08X\n%s",
                   static_cast<int32_t>(ex.code()), ex.message().c_str());
        MessageBoxW(parentHwnd_, buf, L"PivoxActiveX", MB_OK);
        return ex.code();
    }
}

void XamlIslandHost::Resize(int width, int height) {
    if (!initialized_ || !xamlSource_ || !parentHwnd_) return;
    auto bridge = xamlSource_.SiteBridge();
    if (bridge) {
        POINT pt = { 0, 0 };
        ClientToScreen(parentHwnd_, &pt);
        HWND topLevel = GetAncestor(parentHwnd_, GA_ROOT);
        ScreenToClient(topLevel, &pt);
        bridge.MoveAndResize({ pt.x, pt.y, width, height });
    }
}

void XamlIslandHost::Shutdown() {
    if (xamlSource_) {
        xamlSource_.Close();
        xamlSource_ = nullptr;
    }
    // Don't shut down xamlManager_ or bootstrap — they're process/thread-wide
    // and other control instances may still need them.
    parentHwnd_ = nullptr;
    initialized_ = false;
}

} // namespace pivox
