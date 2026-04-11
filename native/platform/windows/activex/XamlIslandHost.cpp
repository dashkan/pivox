#include "pch.h"
#include "XamlIslandHost.h"

#include <winrt/Microsoft.UI.Interop.h>
#include <winrt/Microsoft.UI.Xaml.Markup.h>
#include <winrt/Microsoft.Windows.ApplicationModel.Resources.h>
#include <WindowsAppSDK-VersionInfo.h>
#include <MddBootstrap.h>

namespace pivox {

// ============================================================
// Process-wide statics
// ============================================================
static bool s_processInitDone = false;
static bool s_themeLoaded = false;
static HANDLE s_actCtx = INVALID_HANDLE_VALUE;
static HWND s_managerHwnd = nullptr;
static std::map<HWND, std::unique_ptr<IslandSlot>> s_slots;
static winrt::Microsoft::UI::Xaml::Application s_app{ nullptr };
static winrt::Microsoft::UI::Dispatching::DispatcherQueueController s_dispatcherController{ nullptr };

// ============================================================
// ScopedActCtx
// ============================================================
ScopedActCtx::ScopedActCtx() {
    if (s_actCtx != INVALID_HANDLE_VALUE)
        active = (::ActivateActCtx(s_actCtx, &cookie) != FALSE);
}

ScopedActCtx::~ScopedActCtx() {
    if (active) ::DeactivateActCtx(0, cookie);
}

// ============================================================
// Application subclass with IXamlMetadataProvider
// Required for TextBox, ProgressBar, ColorPicker, etc. to render.
// ============================================================
struct AppWithMetadata : winrt::Microsoft::UI::Xaml::ApplicationT<AppWithMetadata,
    winrt::Microsoft::UI::Xaml::Markup::IXamlMetadataProvider>
{
    winrt::Microsoft::UI::Xaml::Markup::IXamlMetadataProvider m_provider{ nullptr };

    AppWithMetadata()
    {
        // Activate the shared component's XamlMetaDataProvider.
        auto factory = winrt::get_activation_factory<winrt::Windows::Foundation::IActivationFactory>(
            winrt::hstring(L"Pivox.XamlMetaDataProvider"));
        m_provider = factory.ActivateInstance<winrt::Windows::Foundation::IInspectable>()
            .as<winrt::Microsoft::UI::Xaml::Markup::IXamlMetadataProvider>();
    }

    winrt::Microsoft::UI::Xaml::Markup::IXamlType GetXamlType(
        winrt::Windows::UI::Xaml::Interop::TypeName const& type) { return m_provider.GetXamlType(type); }
    winrt::Microsoft::UI::Xaml::Markup::IXamlType GetXamlType(
        winrt::hstring const& fullName) { return m_provider.GetXamlType(fullName); }
    winrt::com_array<winrt::Microsoft::UI::Xaml::Markup::XmlnsDefinition>
        GetXmlnsDefinitions() { return m_provider.GetXmlnsDefinitions(); }
};

// ============================================================
// Process-wide initialization
// ============================================================
HRESULT XamlIslandHost::InitializeProcess()
{
    if (s_processInitDone) return S_OK;

    OutputDebugStringW(L"[PivoxActiveX] Process init starting\n");

    // 1. Create activation context from the embedded manifest in our DLL.
    if (s_actCtx == INVALID_HANDLE_VALUE) {
        wchar_t dllPath[MAX_PATH];
        HMODULE hMod = nullptr;
        ::GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS,
            reinterpret_cast<LPCWSTR>(&s_processInitDone), &hMod);
        ::GetModuleFileNameW(hMod, dllPath, MAX_PATH);

        // Try embedded manifest first (resource ID 2 = ISOLATIONAWARE_MANIFEST_RESOURCE_ID for DLLs).
        ACTCTXW actCtx = {};
        actCtx.cbSize = sizeof(actCtx);
        actCtx.dwFlags = ACTCTX_FLAG_RESOURCE_NAME_VALID | ACTCTX_FLAG_HMODULE_VALID;
        actCtx.hModule = hMod;
        actCtx.lpResourceName = MAKEINTRESOURCEW(2);
        actCtx.lpSource = dllPath;
        s_actCtx = ::CreateActCtxW(&actCtx);

        // Fallback to external .manifest file.
        if (s_actCtx == INVALID_HANDLE_VALUE) {
            wchar_t manifestPath[MAX_PATH];
            wcscpy_s(manifestPath, dllPath);
            wcscat_s(manifestPath, L".manifest");
            ACTCTXW extCtx = {};
            extCtx.cbSize = sizeof(extCtx);
            extCtx.lpSource = manifestPath;
            s_actCtx = ::CreateActCtxW(&extCtx);
        }

        OutputDebugStringW(s_actCtx != INVALID_HANDLE_VALUE
            ? L"[PivoxActiveX] Activation context OK\n"
            : L"[PivoxActiveX] CreateActCtxW FAILED\n");
    }

    // 2. Bootstrap Windows App SDK — scoped context.
    {
        ScopedActCtx ctx;
        PACKAGE_VERSION minVersion{};
        minVersion.Version = WINDOWSAPPSDK_RUNTIME_VERSION_UINT64;
        HRESULT hr = MddBootstrapInitialize(
            WINDOWSAPPSDK_RELEASE_MAJORMINOR,
            WINDOWSAPPSDK_RELEASE_VERSION_TAG_W, minVersion);
        if (FAILED(hr)) {
            wchar_t buf[128];
            swprintf_s(buf, _countof(buf), L"[PivoxActiveX] MddBootstrapInitialize failed: 0x%08X\n", hr);
            OutputDebugStringW(buf);
            return hr;
        }
        OutputDebugStringW(L"[PivoxActiveX] Bootstrap OK\n");
    }

    // 3. DispatcherQueue — scoped context.
    {
        ScopedActCtx ctx;
        if (!winrt::Microsoft::UI::Dispatching::DispatcherQueue::GetForCurrentThread()) {
            s_dispatcherController = winrt::Microsoft::UI::Dispatching::
                DispatcherQueueController::CreateOnCurrentThread();
        }
        OutputDebugStringW(L"[PivoxActiveX] DispatcherQueue OK\n");
    }

    // 4. Application with IXamlMetadataProvider + ResourceManagerRequested — scoped context.
    {
        ScopedActCtx ctx;
        s_app = winrt::make<AppWithMetadata>();
        s_app.ResourceManagerRequested([](auto&&, auto&& args) {
            wchar_t dllPath[MAX_PATH];
            HMODULE hMod = nullptr;
            ::GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS,
                reinterpret_cast<LPCWSTR>(&s_processInitDone), &hMod);
            ::GetModuleFileNameW(hMod, dllPath, MAX_PATH);
            std::wstring priPath(dllPath);
            priPath.resize(priPath.find_last_of(L'\\') + 1);
            priPath += L"Pivox.pri";
            args.CustomResourceManager(
                winrt::Microsoft::Windows::ApplicationModel::Resources::
                ResourceManager(priPath.c_str()));
        });
        OutputDebugStringW(L"[PivoxActiveX] Application OK\n");
    }

    s_processInitDone = true;
    OutputDebugStringW(L"[PivoxActiveX] Process init complete\n");
    return S_OK;
}

// ============================================================
// Island slot management
// ============================================================
void XamlIslandHost::EnsureManagerWindow()
{
    if (s_managerHwnd) return;

    WNDCLASSEXW wc = {};
    wc.cbSize = sizeof(wc);
    wc.lpfnWndProc = ::DefWindowProcW;
    wc.hInstance = ::GetModuleHandleW(nullptr);
    wc.lpszClassName = L"PivoxIslandManager";
    ::RegisterClassExW(&wc);

    s_managerHwnd = ::CreateWindowExW(
        0, L"PivoxIslandManager", nullptr, WS_OVERLAPPEDWINDOW,
        0, 0, 0, 0, nullptr, nullptr,
        ::GetModuleHandleW(nullptr), nullptr);

    OutputDebugStringW(s_managerHwnd
        ? L"[PivoxActiveX] Manager window created\n"
        : L"[PivoxActiveX] Manager window FAILED\n");
}

IslandSlot* XamlIslandHost::CreateSlot(HWND topLevelHwnd)
{
    auto slot = std::make_unique<IslandSlot>();

    slot->childHwnd = ::CreateWindowExW(
        0, L"Static", nullptr, WS_CHILD | WS_CLIPCHILDREN,
        0, 0, 1, 1, s_managerHwnd, nullptr,
        ::GetModuleHandleW(nullptr), nullptr);

    if (!slot->childHwnd) {
        OutputDebugStringW(L"[PivoxActiveX] CreateWindowExW failed\n");
        return nullptr;
    }

    slot->source = winrt::Microsoft::UI::Xaml::Hosting::DesktopWindowXamlSource();
    auto windowId = winrt::Microsoft::UI::GetWindowIdFromWindow(topLevelHwnd);
    slot->source.Initialize(windowId);
    slot->topLevelHwnd = topLevelHwnd;

    // Tab focus cycling within the island.
    slot->source.TakeFocusRequested([](auto& source, auto& args) {
        using Reason = winrt::Microsoft::UI::Xaml::Hosting::XamlSourceFocusNavigationReason;
        auto reason = args.Request().Reason();
        Reason newReason = (reason == Reason::First) ? Reason::First : Reason::Last;
        source.NavigateFocus(
            winrt::Microsoft::UI::Xaml::Hosting::XamlSourceFocusNavigationRequest(newReason));
    });

    auto* raw = slot.get();
    s_slots[raw->childHwnd] = std::move(slot);

    wchar_t buf[128];
    swprintf_s(buf, _countof(buf), L"[PivoxActiveX] Created new slot (total=%zu)\n", s_slots.size());
    OutputDebugStringW(buf);
    return raw;
}

IslandSlot* XamlIslandHost::AcquireSlot(HWND siteHwnd)
{
    EnsureManagerWindow();

    HWND topLevel = ::GetAncestor(siteHwnd, GA_ROOT);
    if (!topLevel) topLevel = siteHwnd;

    // Try to reuse a parked slot (same top-level window).
    for (auto& [hwnd, slot] : s_slots) {
        if (slot->parked && slot->topLevelHwnd == topLevel) {
            slot->parked = false;
            OutputDebugStringW(L"[PivoxActiveX] Reused parked slot\n");
            // Reparent into the new site.
            ::SetParent(slot->childHwnd, siteHwnd);
            UpdatePosition(slot.get(), siteHwnd);
            return slot.get();
        }
    }

    // No reusable slot — create new.
    ScopedActCtx ctx;
    IslandSlot* slot = CreateSlot(topLevel);
    if (!slot) return nullptr;

    // Parent into the site.
    ::SetParent(slot->childHwnd, siteHwnd);
    UpdatePosition(slot, siteHwnd);
    return slot;
}

void XamlIslandHost::ReleaseSlot(IslandSlot* slot)
{
    if (!slot) return;
    auto bridge = slot->source.SiteBridge();
    if (bridge) {
        bridge.MoveAndResize({ 0, 0, 0, 0 });
    }
    ParkSlot(slot);
    slot->parked = true;
    OutputDebugStringW(L"[PivoxActiveX] Slot parked\n");
}

void XamlIslandHost::ParkSlot(IslandSlot* slot)
{
    if (!slot || !slot->childHwnd) return;
    ::ShowWindow(slot->childHwnd, SW_HIDE);
    ::SetParent(slot->childHwnd, s_managerHwnd);
}

void XamlIslandHost::EnsureResources()
{
    if (s_themeLoaded) return;

    ScopedActCtx ctx;
    try {
        winrt::Microsoft::UI::Xaml::Controls::XamlControlsResources xcr;
        s_app.Resources().MergedDictionaries().Append(xcr);
        s_themeLoaded = true;
        OutputDebugStringW(L"[PivoxActiveX] XamlControlsResources loaded\n");
    } catch (const winrt::hresult_error&) {
        try {
            winrt::Microsoft::UI::Xaml::ResourceDictionary themeDict;
            themeDict.Source(winrt::Windows::Foundation::Uri(
                L"ms-appx:///Microsoft.UI.Xaml/Themes/themeresources.xaml"));
            s_app.Resources().MergedDictionaries().Append(themeDict);
            s_themeLoaded = true;
            OutputDebugStringW(L"[PivoxActiveX] Fallback theme resources loaded\n");
        } catch (const winrt::hresult_error& ex2) {
            wchar_t buf[512];
            swprintf_s(buf, _countof(buf), L"[PivoxActiveX] Theme resources failed: 0x%08X %s\n",
                static_cast<int32_t>(ex2.code()), ex2.message().c_str());
            OutputDebugStringW(buf);
        }
    }
}

HRESULT XamlIslandHost::SetContent(IslandSlot* slot, const winrt::Microsoft::UI::Xaml::UIElement& content)
{
    if (!slot || !slot->source) return E_INVALIDARG;
    EnsureResources();
    ScopedActCtx ctx;
    slot->source.Content(content);
    return S_OK;
}

void XamlIslandHost::UpdatePosition(IslandSlot* slot, HWND siteHwnd)
{
    if (!slot || !slot->childHwnd) return;
    RECT rc;
    ::GetClientRect(siteHwnd, &rc);
    ::SetWindowPos(slot->childHwnd, nullptr, 0, 0,
        rc.right, rc.bottom, SWP_NOZORDER | SWP_SHOWWINDOW);
    PositionBridge(slot);
}

void XamlIslandHost::PositionBridge(IslandSlot* slot)
{
    if (!slot || !slot->source) return;
    auto bridge = slot->source.SiteBridge();
    if (!bridge) return;

    RECT rc;
    ::GetClientRect(slot->childHwnd, &rc);

    POINT pt = { 0, 0 };
    ::ClientToScreen(slot->childHwnd, &pt);
    ::ScreenToClient(slot->topLevelHwnd, &pt);

    bridge.MoveAndResize({ pt.x, pt.y, rc.right, rc.bottom });
    bridge.Show();
}

} // namespace pivox
