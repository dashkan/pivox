#pragma once
// XamlIslandHost — manages WinUI 3 XAML Islands lifecycle for ActiveX controls.
//
// Process-wide initialization (bootstrap, dispatcher, Application) is done once.
// Per-instance DWXS slots are pooled via an internal IslandManager — slots are
// parked (hidden) on release and reused on re-acquisition, avoiding the WinUI
// re-creation crash (DesktopWindowXamlSource cannot be destroyed safely).
//
// Key patterns (hard-won, differs from all MS docs):
// - ScopedActCtx: push/pop activation context per WinRT call, NEVER permanently
// - ApplicationT<AppWithMetadata>: IXamlMetadataProvider required for TextBox etc.
// - ResourceManagerRequested: redirects MRT to DLL directory, no files next to host
// - Bridge.MoveAndResize({0,0,0,0}) to hide; offscreen coords kill it permanently

#include <map>

namespace pivox {

// RAII activation context push/pop.
// NEVER leave pushed permanently — crashes MFC hosts (iNEWS).
struct ScopedActCtx {
    ULONG_PTR cookie = 0;
    bool active = false;
    ScopedActCtx();
    ~ScopedActCtx();
};

// Island slot — one DesktopWindowXamlSource + its child HWND.
struct IslandSlot {
    HWND childHwnd = nullptr;
    HWND topLevelHwnd = nullptr;
    bool parked = false;
    winrt::Microsoft::UI::Xaml::Hosting::DesktopWindowXamlSource source{ nullptr };
};

class XamlIslandHost {
public:
    XamlIslandHost() = default;
    ~XamlIslandHost() = default;

    XamlIslandHost(const XamlIslandHost&) = delete;
    XamlIslandHost& operator=(const XamlIslandHost&) = delete;

    // Process-wide init: bootstrap, dispatcher, Application, theme.
    // Safe to call multiple times — guarded by static flags.
    static HRESULT InitializeProcess();

    // Per-instance: acquire a DWXS slot, position it in siteHwnd.
    IslandSlot* AcquireSlot(HWND siteHwnd);

    // Release a slot back to the pool (park, collapse bridge).
    void ReleaseSlot(IslandSlot* slot);

    // Load XamlControlsResources (once). Must be called after AcquireSlot
    // and before creating any XAML pages.
    void EnsureResources();

    // Set XAML content on the slot's source.
    HRESULT SetContent(IslandSlot* slot, const winrt::Microsoft::UI::Xaml::UIElement& content);

    // Reposition the slot to match the site HWND's client rect.
    void UpdatePosition(IslandSlot* slot, HWND siteHwnd);

private:
    // Island slot pool (process-wide singleton).
    static void EnsureManagerWindow();
    static IslandSlot* CreateSlot(HWND topLevelHwnd);
    static void ParkSlot(IslandSlot* slot);
    static void PositionBridge(IslandSlot* slot);
};

} // namespace pivox
