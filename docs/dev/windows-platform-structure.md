# Windows Platform Structure

## Why the Restructure

The Pivox Windows platform was restructured from a flat directory into two targets to enable XAML reuse between the WinUI 3 desktop application and an ActiveX control. Shared source files (Views, services, auth) are compiled directly into each target — no shared DLL, no `dllexport`/`dllimport`. Both the app (Pivox.exe) and the ActiveX control (PivoxActiveX.dll) compile the same source files from `shared/`.

## Directory Layout

```
platform/windows/
├── WinAppState.h / .cpp        # Windows Registry / PasswordVault state
├── WinAuthService.h / .cpp     # Firebase Auth + OAuth (AuthStateListener)
├── Directory.Build.props        # MSBuild properties (configure_file'd to build dir)
├── Directory.Build.targets      # MSBuild targets (module.g.cpp, MIDL workaround)
│
├── app/                         # WinUI 3 executable (Pivox.exe)
│   ├── App.idl                  # Empty namespace block (NOT a runtimeclass)
│   ├── App.xaml                 # ApplicationDefinition (XamlControlsResources)
│   ├── App.xaml.h / .cpp        # Application lifecycle, PivoxServices init
│   ├── MainWindow.idl / .xaml / .h / .cpp
│   ├── pch.h                    # Full WinUI Application + Window headers
│   └── CMakeLists.txt
│
├── shared/                      # Shared source compiled into each target (NOT a DLL)
│   ├── PivoxServices.h / .cpp   # Service locator — bridges app state to shared pages
│   ├── GoogleOAuth.cpp          # OAuth2Manager coroutine (Google Sign-In)
│   ├── DragService.h / .cpp     # Compile-time drag abstraction (PIVOX_ACTIVEX_HOST)
│   ├── pch.h                    # WinUI controls (no Application headers)
│   ├── Views/
│   │   ├── LoginPage.idl / .xaml / .h / .cpp
│   │   ├── RegisterPage.idl / .xaml / .h / .cpp
│   │   ├── MainPage.idl / .xaml / .h / .cpp
│   │   └── TestPage.idl / .xaml / .h / .cpp
│   ├── Controls/                # Custom controls (future)
│   └── Resources/               # ResourceDictionaries (future: SVG-wrapped icons)
│
└── activex/                     # ATL-based ActiveX control (PivoxActiveX.dll)
    ├── PivoxControl.idl         # COM IDL (not WinRT IDL)
    ├── PivoxControl.h / .cpp    # ATL CComCoClass, ActiveX interfaces
    ├── PivoxControl.rgs         # ATL registry script
    ├── PivoxControl.def         # DLL exports
    ├── XamlIslandHost.h / .cpp  # DesktopWindowXamlSource setup/teardown
    ├── DragSource.h / .cpp      # Manual in-process drag (timer + IDropTarget)
    ├── PivoxActiveX.manifest    # WinRT activatableClass entries → PivoxActiveX.dll
    ├── dllmain.cpp              # DLL entry + ATL module
    ├── resource.h               # Resource IDs
    ├── pch.h                    # ATL + WinRT + Windows headers
    ├── CMakeLists.txt
    └── tests/
        ├── test_com_interfaces.cpp
        ├── test_drag_formats.cpp
        ├── CMakeLists.txt
        └── test_host/
            ├── main.cpp         # Minimal Win32 ActiveX container
            └── CMakeLists.txt
```

### What lives where and why

**`platform/windows/` (root)** — Platform-level services that are neither app-specific nor XAML-related. `WinAppState.cpp` and `WinAuthService.cpp` are compiled directly into each target (listed in `WIN_SHARED_SOURCES`). There is no `pivox_win_state` static lib.

**`app/`** — The WinUI 3 Application definition and the app's top-level window. `App.xaml` is the `ApplicationDefinition` (tagged via `VS_XAML_TYPE`), which XAML Islands does not use. `MainWindow` subscribes to auth state changes and swaps content accordingly — it does not navigate between pages.

**`shared/`** — All XAML pages/views and services reused by both the app and ActiveX control. Source files are compiled into each target directly — this is NOT a DLL. Contains the `PivoxServices` service locator, `DragService` (compile-time abstraction), and OAuth flow logic. Pages do not navigate — `MainWindow`/`PivoxControl` subscribe to auth state changes from `WinAuthService` and swap content.

**`activex/`** — The ATL-based ActiveX control. Hosts XAML content via XAML Islands (`DesktopWindowXamlSource`). Contains COM plumbing (IDL, registry script, exports) and the manual in-process drag system.

## Shared Source

Shared source files in `shared/` are compiled directly into each target. There is no shared DLL, no `dllexport`/`dllimport`, no `.winmd` metadata. Both the app and the ActiveX control get their own compiled copy.

### What it contains

- **XAML pages** — LoginPage, RegisterPage, MainPage, TestPage (and future views)
- **Custom controls** — In `Controls/` (future)
- **ResourceDictionaries** — In `Resources/` (future: SVG-wrapped icons)
- **GoogleOAuth** — OAuth2Manager coroutine for Google Sign-In
- **PivoxServices** — Static service locator providing access to `WinAppState` and `WinAuthService`
- **DragService** — Compile-time drag abstraction (`PIVOX_ACTIVEX_HOST` preprocessor)

### How it's consumed

**By the app (Pivox):**
1. App creates `WinAppState` and `WinAuthService` instances
2. App calls `PivoxServices::initialize(appState, authService)` to inject them
3. `MainWindow` subscribes to `WinAuthService::onAuthStateChanged()` and swaps content between login and main views
4. XAML runtime discovers page types through the target's own `module.g.cpp` type registrations

**By the ActiveX control (PivoxActiveX):**
1. Control initializes Windows App SDK via `MddBootstrapInitialize()`
2. Control creates services and calls `PivoxServices::initialize()` if the app hasn't already
3. `XamlIslandHost` creates a `DesktopWindowXamlSource` inside the control's HWND
4. `PivoxControl` subscribes to `WinAuthService::onAuthStateChanged()` and swaps content

### App namespace

The app namespace is `Pivox`. It was briefly changed to `PivoxApp` to avoid a conflict with the former `PivoxShared` DLL, but reverted to `Pivox` when the shared DLL was eliminated.

### Why no ms-appx:/// URIs

The shared source must work in both contexts — WinUI 3 Application and XAML Islands. The `ms-appx:///` URI scheme resolves resources from the app package, but:
- XAML Islands does not have an app package context
- The ActiveX control runs inside an arbitrary host process (no `.appx` / `.msix`)

All resources use `{ThemeResource}`, `{StaticResource}`, and inline SVG path data instead.

## The ActiveX Control

### Architecture

**ATL-based** — Uses `CComObjectRootEx`, `CComCoClass`, `CComControl`, and ATL interface implementation templates. No raw COM/OLE boilerplate.

**XAML Islands hosting** — The control creates a child HWND inside the ActiveX control's in-place HWND (received via `IOleInPlaceSite`). A `DesktopWindowXamlSource` is attached to this child HWND, and XAML content compiled into PivoxActiveX.dll is loaded into it.

**Framework-dependent** — NOT `WindowsAppSDKSelfContained`. The Windows App SDK runtime must be installed on the target machine. `DllRegisterServer` checks for the minimum required runtime version and fails with `ERROR_PRODUCT_UNINSTALLED` if not present. The elevated installer that calls `regsvr32` should enforce this as a prerequisite.

**x86 only** — The ActiveX control targets x86 (`-A Win32`) for maximum host compatibility. Built in a separate CMake configure from the main x64/arm64 app.

### Interfaces implemented

| Interface | Purpose |
|-----------|---------|
| `IPivoxControl` | Custom dispatch interface (NavigateTo, Shutdown, IsInitialized) |
| `IOleObject` | OLE embedding lifecycle |
| `IOleInPlaceObject` | In-place activation (receives HWND) |
| `IOleInPlaceActiveObject` | Active object for message translation |
| `IOleControl` | Control-specific OLE behavior |
| `IViewObject2` / `IViewObjectEx` | Rendering and hit testing |
| `IPersistStreamInit` | Stream-based persistence |
| `IConnectionPointContainer` | Connection point enumeration |
| `IDataObject` | Drag data exposure |
| `IProvideClassInfo2` | Type information |
| `ISupportErrorInfo` | Rich error info |

### Lifecycle

1. **Creation** — Host calls `CoCreateInstance(CLSID_PivoxControl)`
2. **Activation** — Host calls `IOleObject::DoVerb(OLEIVERB_INPLACEACTIVATE)`. Control initializes Windows App SDK, creates XAML Island, navigates to default page.
3. **Running** — XAML content renders inside the host. Drag operations use manual in-process drag (timer polling + direct `IDropTarget` calls). See `docs/dev/windows-activex-dragdrop.md`.
4. **Deactivation** — `IOleInPlaceObject::InPlaceDeactivate()`. XAML Island is torn down cleanly before COM release.

### Drag source

Drag operations use a manual in-process drag system. `DragService::HandleDragStarting()` cancels the native WinUI drag (which fails in elevated hosts) and starts a timer-based polling loop that calls `IDropTarget` directly. The XAML UI calls `DragService` identically in both targets — the compile-time `PIVOX_ACTIVEX_HOST` preprocessor selects the manual path for ActiveX, no-op for the app. See `docs/dev/windows-activex-dragdrop.md` for full details.

### Registration

The control registers in HKLM (requires elevation via `regsvr32`). The registry script (`PivoxControl.rgs`) creates:
- ProgID: `Pivox.PivoxControl.1` / `Pivox.PivoxControl`
- CLSID entries with `InprocServer32`, `ThreadingModel=Apartment`
- `Control` and `MiscStatus` keys for ActiveX container discovery

## CMake Target Relationships

```
pivox_core (STATIC)
    ├── Pivox (EXE) — WinUI 3 app, links pivox_core + Firebase SDK
    │       Compiles: app/ + shared/ + WinAppState.cpp + WinAuthService.cpp
    │
    └── PivoxActiveX (SHARED/.dll) — ActiveX, links pivox_core + Firebase SDK
            Compiles: activex/ + shared/ + WinAppState.cpp + WinAuthService.cpp
            Defines: PIVOX_ACTIVEX_HOST
```

There is no `pivox_win_state` static lib and no `PivoxShared` DLL. `WinAppState.cpp` and `WinAuthService.cpp` are listed in `WIN_SHARED_SOURCES` and compiled directly into each target.

### Directory.Build.props

Scoped by `$(ProjectName)` condition to avoid affecting unrelated CMake targets:
- **All Windows targets** — `NuGetTargetMoniker=native,Version=v0.0`
- **Pivox** — `RuntimeIdentifiers=win10-x64;win10-arm64`
- **PivoxActiveX** — `RuntimeIdentifiers=win10-x86`

### Directory.Build.targets

- **Pivox** — Includes `module.g.cpp` in compilation, sets empty MIDL `OutputDirectory` (CMake workaround)
- **PivoxActiveX** — Same module.g.cpp and MIDL handling

Both files are `configure_file()`'d from the source tree into the CMake build directory. They must NOT be in the project root — this breaks CMake's compiler detection.

### NuGet packages

All Windows targets use the same package versions:
- `Microsoft.Windows.CppWinRT` 2.0.250303.1
- `Microsoft.WindowsAppSDK` 1.8.260317003
- `Microsoft.Windows.SDK.BuildTools` 10.0.26100.7705

### IDL/XAML wiring

Each target with XAML uses the same CMake loop pattern:
```cmake
foreach(_SOURCE ${_SOURCES})
    # Find .idl files, set DependentUpon to matching .xaml
endforeach()
```

This ensures `module.g.cpp` includes the correct headers for XAML type registration.

## Build Commands

### CMake build separation

`PIVOX_BUILD_ACTIVEX` CMake option controls which target is built. The app and ActiveX control are mutually exclusive in a single configure — they target different architectures. A hard `FATAL_ERROR` fires if `PIVOX_BUILD_ACTIVEX` is set without `-A Win32`.

### App (x64, default)

```bash
mkdir build-win-x64 && cd build-win-x64
cmake -G "Visual Studio 18 2026" -A x64 ..

# Debug (default)
cmake --build .

# Release
cmake --build . --config Release

# Tests
ctest -C Debug
ctest -C Release
```

### App (arm64)

```bash
mkdir build-win-arm64 && cd build-win-arm64
cmake -G "Visual Studio 18 2026" -A ARM64 ..
cmake --build .
```

### ActiveX control (x86 only, separate build dir)

Uses the `win-activex` CMake preset which sets `-A Win32` and `PIVOX_BUILD_ACTIVEX=ON`:

```bash
cmake --preset win-activex
cmake --build build-activex-x86

# Or manually:
mkdir build-activex-x86 && cd build-activex-x86
cmake -G "Visual Studio 18 2026" -A Win32 -DPIVOX_BUILD_ACTIVEX=ON ..
cmake --build . --target PivoxActiveX

# Register (elevated)
regsvr32 Debug\PivoxActiveX.dll
```

## Testing Strategy

### Layer 1: Shared C++ core tests (109 tests)

Platform-independent unit tests for the core library. Run on both macOS and Windows.

```bash
ctest -C Debug -R pivox_core_tests
```

### Layer 2: Windows integration tests

Test `WinAppState` (Registry, PasswordVault) and `WinAuthService` (Firebase, OAuth validation) with real Windows APIs.

```bash
ctest -C Debug -R pivox_win_tests
```

### Layer 3: COM interface unit tests

Pure COM object instantiation — no UI needed. `CoCreate` the control, `QueryInterface` for each expected interface, verify `IPersistStreamInit::InitNew()`, connection point enumeration, and `DllRegisterServer` behavior.

```bash
ctest -C Debug -R pivox_activex_tests
```

### Layer 4: Drag format tests

Verify custom clipboard format registration and coexistence with standard formats. Tests that require the control to be activated use a test HWND.

### Layer 5: Test host + UI Automation

Minimal Win32 application (`PivoxTestHost`) that `CoCreate`s and hosts the ActiveX control in a window. Used for:
- Manual visual testing
- Automated UI tests via Microsoft UI Automation (UIA)
- Verifying XAML content renders inside the ActiveX control

## Constraints and Gotchas

### No ms-appx:/// URIs

Shared source and the ActiveX control must never use `ms-appx:///` URIs. XAML Islands has no app package context. All resources use `{ThemeResource}`, `{StaticResource}`, inline path data, or programmatic loading.

### WinUI 3 XAML Islands in Win32 applications

This section documents how to host WinUI 3 (`Microsoft.UI.Xaml`) content inside a Win32 window using XAML Islands with Windows App SDK 1.8+. The WinUI 3 API differs substantially from the UWP XAML Islands documentation on learn.microsoft.com — most UWP patterns do not work.

#### Prerequisites

- Windows App SDK 1.8+ runtime installed (`winget install Microsoft.WindowsAppRuntime.1.8`)
- CppWinRT NuGet package
- WindowsAppSDK NuGet package
- `Microsoft.WindowsAppRuntime.Bootstrap.dll` in the output directory
- The host window must be a real HWND (not windowless)

#### Initialization sequence

All steps happen on the UI thread, typically in a window creation handler:

```cpp
#include <WindowsAppSDK-VersionInfo.h>
#include <MddBootstrap.h>
#include <winrt/Microsoft.UI.Dispatching.h>
#include <winrt/Microsoft.UI.Xaml.Hosting.h>
#include <winrt/Microsoft.UI.Xaml.Controls.h>
#include <winrt/Microsoft.UI.Xaml.Markup.h>
#include <winrt/Microsoft.UI.Interop.h>

// 1. Bootstrap Windows App SDK (process-wide, once).
//    Links against Microsoft.WindowsAppRuntime.Bootstrap.lib.
PACKAGE_VERSION minVersion{};
minVersion.Version = WINDOWSAPPSDK_RUNTIME_VERSION_UINT64;
MddBootstrapInitialize(WINDOWSAPPSDK_RELEASE_MAJORMINOR,
    WINDOWSAPPSDK_RELEASE_VERSION_TAG_W, minVersion);

// 2. DispatcherQueue (thread-level, once).
if (!winrt::Microsoft::UI::Dispatching::DispatcherQueue::GetForCurrentThread())
    auto controller = winrt::Microsoft::UI::Dispatching::
        DispatcherQueueController::CreateOnCurrentThread();

// 3. Application with IXamlMetadataProvider (process-wide, once).
//    The XAML engine (MetadataAPI.cpp) QIs Application::Current() for
//    IXamlMetadataProvider to resolve control types like TextBox,
//    ProgressBar, ColorPicker. Without it, only built-in types
//    (Button, TextBlock, StackPanel) render — everything else is
//    silently skipped by the XAML parser.
//
//    Use C++/WinRT's ApplicationT<> composable pattern. This handles
//    inner/outer QI delegation correctly. Do NOT use winrt::implements<>
//    (doesn't forward QI to inner Application) or pass a raw provider
//    as outer to CreateInstance (COM aggregation rules prevent it).

struct AppWithMetadata : winrt::Microsoft::UI::Xaml::ApplicationT<AppWithMetadata,
    winrt::Microsoft::UI::Xaml::Markup::IXamlMetadataProvider>
{
    winrt::Microsoft::UI::Xaml::Markup::IXamlMetadataProvider m_provider{ nullptr };

    AppWithMetadata() {
        // Activate the WinRT component's XamlMetaDataProvider.
        // The generated provider chains to XamlControlsXamlMetaDataProvider,
        // which knows all WinUI 3 control types.
        auto factory = winrt::get_activation_factory<
            winrt::Windows::Foundation::IActivationFactory>(
            winrt::hstring(L"MyComponent.XamlMetaDataProvider"));
        m_provider = factory.ActivateInstance<winrt::Windows::Foundation::IInspectable>()
            .as<winrt::Microsoft::UI::Xaml::Markup::IXamlMetadataProvider>();
    }

    // IXamlMetadataProvider — delegate to component provider
    winrt::Microsoft::UI::Xaml::Markup::IXamlType GetXamlType(
        winrt::Windows::UI::Xaml::Interop::TypeName const& type) {
        return m_provider.GetXamlType(type);
    }
    winrt::Microsoft::UI::Xaml::Markup::IXamlType GetXamlType(
        winrt::hstring const& fullName) {
        return m_provider.GetXamlType(fullName);
    }
    winrt::com_array<winrt::Microsoft::UI::Xaml::Markup::XmlnsDefinition>
    GetXmlnsDefinitions() {
        return m_provider.GetXmlnsDefinitions();
    }
};

static winrt::Microsoft::UI::Xaml::Application s_app{ nullptr };
if (!winrt::Microsoft::UI::Xaml::Application::Current()) {
    auto app = winrt::make<AppWithMetadata>();
    s_app = app;
}

// 4. Create the XAML Island source.
auto xamlSource = winrt::Microsoft::UI::Xaml::Hosting::DesktopWindowXamlSource();

// 5. Initialize with the top-level ancestor window's WindowId.
HWND topLevel = ::GetAncestor(hwnd, GA_ROOT);
auto windowId = winrt::Microsoft::UI::GetWindowIdFromWindow(topLevel);
xamlSource.Initialize(windowId);

// 6. Position the SiteBridge inside the host window.
//    The bridge is a child of the top-level window, not the host.
//    Map host client coordinates to the top-level window's client space.
auto bridge = xamlSource.SiteBridge();
POINT pt = { 0, 0 };
::ClientToScreen(hwnd, &pt);
::ScreenToClient(topLevel, &pt);
RECT rc;
::GetClientRect(hwnd, &rc);
bridge.MoveAndResize({ pt.x, pt.y, rc.right, rc.bottom });
bridge.Show();

// 7. Set content — TextBox, ProgressBar, and all WinUI controls now work.
winrt::Microsoft::UI::Xaml::Controls::TextBox textBox;
textBox.PlaceholderText(L"Type here...");
xamlSource.Content(textBox);
```

Reposition the bridge on both `WM_SIZE` and `WM_MOVE` using the same coordinate mapping.

#### What does NOT work with WinUI 3

| Approach | Error | Why |
|----------|-------|-----|
| `IApplicationFactory::CreateInstance(nullptr, inner)` — no `IXamlMetadataProvider` | TextBox, ProgressBar invisible (`0x802B000A`) | XAML engine QIs `Application::Current()` for `IXamlMetadataProvider` (MetadataAPI.cpp line 353). Without it, the parser can't resolve control types from the WinUI controls library. Only built-in types (Button, TextBlock) render. |
| `winrt::implements<Wrapper, IXamlMetadataProvider>` as outer to `CreateInstance` | `E_NOINTERFACE` (0x80004002) | `winrt::implements<>` doesn't forward unknown QIs to the inner Application. Must use `ApplicationT<>` composable pattern. |
| Passing raw `IXamlMetadataProvider` object as outer to `CreateInstance` | `E_NOINTERFACE` (0x80004002) | WinRT composition requires the outer to control identity. A standalone WinRT object from a different DLL can't serve as the outer. |
| `WindowsXamlManager::InitializeForCurrentThread()` then `Initialize(windowId)` | `RO_E_CLOSED` (0x80000013) | They conflict — WindowsXamlManager takes ownership of the XAML lifecycle |
| `IDesktopWindowXamlSourceNative2::AttachToWindow()` | `E_NOINTERFACE` (0x80004002) | UWP-only interface, not implemented on WinUI 3's DesktopWindowXamlSource |
| `SetParent(bridgeHwnd, hostHwnd)` | `RO_E_CLOSED` on `MoveAndResize()` | Reparenting breaks the bridge's internal state |
| `Application::Start()` | Blocks forever | It runs its own message loop; the host process owns the pump |
| `IApplicationFactory::CreateInstance()` as member variable | Crash on control re-creation | Must be a **process-lifetime static**; as a member it dies with the control and zombifies the XAML framework |
| `DeactivateActCtx` on control destroy | Crash `c015000f` on re-creation | Activation context cookies are invalid after deactivation; activate once, never deactivate |
| Loading `generic.xaml` into `Application.Resources` | Crash during render tick | Duplicates the XAML engine's internal template resolution, causing conflicts |
| `XamlControlsResources` constructor | `AcrylicBackgroundFillColorDefaultBrush not found` or `RPC_E_WRONG_THREAD` | Requires full XAML thread context (WindowsXamlManager) AND framework PRI registration — both conflict with `Initialize(windowId)`. Use `ApplicationT<>` with `IXamlMetadataProvider` instead. |

#### WinRT activation for compiled XAML (shared-source model)

Since all XAML is compiled directly into PivoxActiveX.dll (no separate component DLL), the embedded manifest (`PivoxActiveX.manifest`) registers all WinRT types as `activatableClass` entries pointing to `PivoxActiveX.dll` itself:

```xml
<file name="PivoxActiveX.dll">
  <activatableClass name="Pivox.XamlMetaDataProvider"
      threadingModel="both" xmlns="urn:schemas-microsoft-com:winrt.v1" />
  <activatableClass name="Pivox.LoginPage"
      threadingModel="both" xmlns="urn:schemas-microsoft-com:winrt.v1" />
  <!-- ... all pages ... -->
</file>
```

The manifest is embedded at link time via `/MANIFESTINPUT`. No external `.manifest` file needed at runtime.

**DLL exports** — `PivoxControl.def` maps CppWinRT-generated functions:

```
EXPORTS
DllCanUnloadNow = WINRT_CanUnloadNow                    PRIVATE
DllGetActivationFactory = WINRT_GetActivationFactory    PRIVATE
```

**MRT resource loading** — `Application.ResourceManagerRequested` redirects PRI resolution to the DLL's own directory:

```cpp
s_app.ResourceManagerRequested([](auto&&, auto&& args) {
    wchar_t dllPath[MAX_PATH];
    HMODULE hMod = nullptr;
    ::GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS,
        reinterpret_cast<LPCWSTR>(&s_bootstrapped), &hMod);
    ::GetModuleFileNameW(hMod, dllPath, MAX_PATH);

    std::wstring priPath(dllPath);
    priPath.resize(priPath.find_last_of(L'\\') + 1);
    priPath += L"resources.pri";

    auto customMgr = winrt::Microsoft::Windows::ApplicationModel::Resources::
        ResourceManager(priPath.c_str());
    args.CustomResourceManager(customMgr);
});
```

**Required output directory structure (next to registered DLL):**
```
<dll_directory>/
  PivoxActiveX.dll                 # COM-registered DLL (manifest embedded)
  Pivox/
    LoginPage.xbf                  # Compiled XAML binaries
    RegisterPage.xbf
    MainPage.xbf
    TestPage.xbf
  resources.pri                    # PRI for XBF resolution
  Microsoft.WindowsAppRuntime.Bootstrap.dll
```

**Files needed next to host EXE: NONE.**

#### `AppxPriInitialPath`

MSBuild property that aligns the PRI embed path with `RootNamespace` when the project name differs. Both Pivox.exe and PivoxActiveX.dll set `VS_GLOBAL_AppxPriInitialPath "Pivox"` so the compiled XAML binaries are found under the `Pivox/` directory regardless of target name.

#### XAML content approaches

| Approach | Works? | Tradeoffs |
|----------|--------|-----------|
| **Programmatic C++** | Yes | No resource loading needed. Verbose. |
| **`XamlReader::Load()` from string** | Yes | Runtime XAML parsing. No `x:Name` bindings. No compile-time validation. |
| **Compiled XAML in same DLL** | Yes (with PRI + manifest) | Best performance. PRI resolved via `ResourceManagerRequested`. Activation manifest embedded in DLL. |

#### Build configuration — COM IDL + CppWinRT in the same project

When a project has both COM IDL (`.idl` producing `.tlb`) and needs CppWinRT for WinRT API consumption, CppWinRT's build targets hijack MIDL and try to produce `.winmd` from the COM IDL. This fails.

**Solution — MSBuild properties in `<PropertyGroup Label="Globals">`:**

```xml
<CppWinRTOptimized>true</CppWinRTOptimized>
<CppWinRTProjectStyle>None</CppWinRTProjectStyle>
<CppWinRTModernIDL>false</CppWinRTModernIDL>
<CppWinRTEnableLegacyCoroutines>false</CppWinRTEnableLegacyCoroutines>
```

| Property | Effect |
|----------|--------|
| `CppWinRTProjectStyle=None` | Skips `mdmerge` — no `.winmd` generation from COM IDL |
| `CppWinRTModernIDL=false` | Standard MIDL for COM IDL (produces `.tlb`, `_i.h`, `_i.c`) |
| `CppWinRTEnableLegacyCoroutines=false` | Suppresses `/await` deprecation warning on VS 2026 |
| Do NOT set `CppWinRTEnabled` | Let the NuGet package set it; explicit values break tool paths |
| Do NOT set `UseWinUI` | Triggers XAML compilation which conflicts with COM IDL |

**CMake equivalents:**

| MSBuild Property | CMake |
|-----------------|-------|
| `CppWinRTProjectStyle=None` | `VS_GLOBAL_CppWinRTProjectStyle "None"` |
| `CppWinRTModernIDL=false` | `VS_GLOBAL_CppWinRTModernIDL "false"` |
| `CppWinRTEnableLegacyCoroutines=false` | `VS_GLOBAL_CppWinRTEnableLegacyCoroutines "false"` |

#### XAML code-behind pattern

Do NOT call `InitializeComponent` in the constructor:

```cpp
// MyControl.h
MyControl() { }  // No InitializeComponent here

// MyControl.cpp
#include "pch.h"
#include "MyControl.h"
#if __has_include("MyControl.g.cpp")
#include "MyControl.g.cpp"
#endif
```

#### Lifecycle and control re-creation

The XAML framework maintains process-wide global state. All of the following must be **created once and never destroyed** during the process lifetime:

| Resource | Scope | Rule |
|----------|-------|------|
| `MddBootstrapInitialize` | Process | Call once. Never call `MddBootstrapShutdown` until process exit. |
| `DispatcherQueueController` | Thread | Create once per UI thread. Never shut down between island instances. |
| Activation context (`CreateActCtxW`) | Process | Activate once. **Never call `DeactivateActCtx`** — it crashes (`c015000f`). |
| `Microsoft.UI.Xaml.Application` | Process | Create via `ApplicationT<AppWithMetadata>` (composable pattern with `IXamlMetadataProvider`). Store as static. Never let it go out of scope or call `Exit()`. |

**On control creation** (`WM_CREATE`):
1. Guard all of the above with `once_flag` or null checks
2. Create a new `DesktopWindowXamlSource` (this is per-instance, lightweight)
3. Call `Initialize(windowId)`, position SiteBridge, set content

**On control destruction** (`WM_DESTROY`):
```cpp
m_xamlSource.Content(nullptr);   // Clear content first
m_xamlSource.Close();            // Close the island
m_xamlSource = nullptr;
m_xamlInitialized = false;
// Do NOT touch DispatcherQueue, Application, Bootstrap, or activation context.
```

The `DesktopWindowXamlSource` is the only thing that gets created and destroyed per control instance. Everything else is process-lifetime infrastructure.

**Why:** The XAML framework binds to the `DispatcherQueue` and `Application` singleton on first use. If these are destroyed, the framework enters a zombie state — subsequent `DesktopWindowXamlSource` creation crashes with access violations in `Microsoft.UI.Xaml.dll`.

#### Framework-dependent deployment

The control requires the Windows App SDK runtime on the target machine:
```
winget install Microsoft.WindowsAppRuntime.1.8
```

`Microsoft.WindowsAppRuntime.Bootstrap.dll` must be in the same directory as the host DLL.

### App.xaml / ApplicationDefinition

The `App.xaml` with `VS_XAML_TYPE "ApplicationDefinition"` is specific to the WinUI 3 app. XAML Islands uses a code-only Application subclass (via `ApplicationT<>`) that implements `IXamlMetadataProvider` — but it has NO `.xaml` file and NO `ApplicationDefinition`. The ActiveX control must NOT have an `ApplicationDefinition`. The `ApplicationT<>` instance activates the `XamlMetaDataProvider` from PivoxActiveX.dll itself (not a separate component DLL).

### App.idl is an empty namespace block

`App.idl` must be `namespace Pivox {}` — NOT a runtimeclass. The namespace is `Pivox` (not `PivoxApp` — that was a temporary name used during the shared-DLL era). Declaring App as a runtimeclass generates conflicting factory functions that prevent the app from launching.

### App.xaml.cpp does NOT include App.g.cpp

Unlike other windows/pages, `App.xaml.cpp` must not include `App.g.cpp`. Other windows (MainWindow) and pages (LoginPage, RegisterPage) include their respective `.g.cpp`.

### Static variable duplication

Since shared source is compiled directly into each target, each module has its own copy of file-scope statics in WinAuthService.cpp (notably `s_googleOAuthLauncher`). This is safe because each target is a separate process/DLL — the app (Pivox.exe) and ActiveX (PivoxActiveX.dll) never share an address space for these statics.

### PivoxServices initialization order

`PivoxServices::initialize()` must be called before any shared page is instantiated. The app does this in `App::App()`. The ActiveX control does this in `DoVerb(OLEIVERB_INPLACEACTIVATE)`.

## Suppressed Warnings

All warnings are treated as errors (`/W4 /WX` for compiler, `/WX` for linker). The following are explicitly suppressed:

| Warning | Scope | Reason |
|---------|-------|--------|
| **C4100** (unreferenced parameter) | All MSVC targets | Equivalent to GCC/Clang `-Wno-unused-parameter`. Common in interface implementations and callback signatures where parameters are part of the contract but unused. |
| **C4251** (DLL-interface for class members) | PivoxActiveX target | CppWinRT-generated code in DLL targets triggers this for `std::shared_ptr` members. No actual DLL boundary for shared source — safe to suppress. |
| **LNK4099** (missing PDB) | All Windows targets | Firebase C++ SDK 13.5.0 pre-built `.lib` files ship without `.pdb` files. Cannot fix without building Firebase from source. |
| **LNK4075** (/INCREMENTAL vs /OPT) | WinUI/CppWinRT targets | NuGet targets inject `/OPT:ICF` which conflicts with CMake's default `/INCREMENTAL` in Debug builds. Harmless — incremental linking is simply disabled. |
| **LNK4078** (multiple .drectve sections) | PivoxActiveX | Custom `.def` file and CppWinRT both inject linker directives. The sections have different attributes but the final exports are correct. |

## Icons Approach

Icons use SVG path data wrapped in XAML `ResourceDictionary` entries, sourced from Microsoft's [fluentui-system-icons](https://github.com/microsoft/fluentui-system-icons) repository.

**Why not FluentIcons.WinUI NuGet:**
- The NuGet package loads icon fonts via `ms-appx:///` URIs
- XAML Islands has no app package context — font loading would fail
- SVG path data in `ResourceDictionary` works in both Application and XAML Islands contexts

Icon definitions go in `shared/Resources/` as `.xaml` `ResourceDictionary` files containing `PathGeometry` or `Path` elements. Both targets compile these into their output.
