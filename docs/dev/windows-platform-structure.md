# Windows Platform Structure

## Why the Restructure

The Pivox Windows platform was restructured from a flat directory into three targets to enable XAML reuse between the WinUI 3 desktop application and an ActiveX control. The shared WinRT component library (DLL + .winmd) contains XAML pages, custom controls, and shared Windows-specific logic that both consumers host — the app via its standard Application model, the ActiveX control via XAML Islands.

## Directory Layout

```
platform/windows/
├── WinAppState.h / .cpp        # Windows Registry / PasswordVault state
├── WinAuthService.h / .cpp     # Firebase Auth + OAuth management
├── Directory.Build.props        # MSBuild properties (configure_file'd to build dir)
├── Directory.Build.targets      # MSBuild targets (module.g.cpp, MIDL workaround)
│
├── app/                         # WinUI 3 executable (Pivox)
│   ├── App.idl                  # Empty namespace block (NOT a runtimeclass)
│   ├── App.xaml                 # ApplicationDefinition (XamlControlsResources)
│   ├── App.xaml.h / .cpp        # Application lifecycle, PivoxServices init
│   ├── MainWindow.idl / .xaml / .h / .cpp
│   ├── pch.h                    # Full WinUI Application + Window headers
│   └── CMakeLists.txt
│
├── shared/                      # WinRT component library (PivoxShared.dll + .winmd)
│   ├── PivoxServices.h / .cpp   # Service locator — bridges app state to shared pages
│   ├── GoogleOAuth.cpp          # OAuth2Manager coroutine (Google Sign-In)
│   ├── pch.h                    # WinUI controls (no Application headers)
│   ├── CMakeLists.txt
│   ├── Views/
│   │   ├── LoginPage.idl / .xaml / .h / .cpp
│   │   └── RegisterPage.idl / .xaml / .h / .cpp
│   ├── Controls/                # Custom controls (future)
│   └── Resources/               # ResourceDictionaries (future: SVG-wrapped icons)
│
└── activex/                     # ATL-based ActiveX control (PivoxActiveX.ocx)
    ├── PivoxControl.idl         # COM IDL (not WinRT IDL)
    ├── PivoxControl.h / .cpp    # ATL CComCoClass, ActiveX interfaces
    ├── PivoxControl.rgs         # ATL registry script
    ├── PivoxControl.def         # DLL exports
    ├── XamlIslandHost.h / .cpp  # DesktopWindowXamlSource setup/teardown
    ├── DragSource.h / .cpp      # Drag initiation bridging WinUI DragStarting
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

**`platform/windows/` (root)** — Platform-level services that are neither app-specific nor XAML-related. `WinAppState` and `WinAuthService` are compiled into the `pivox_win_state` static library, shared by all targets and tests.

**`app/`** — The WinUI 3 Application definition and the app's top-level window. `App.xaml` is the `ApplicationDefinition` (tagged via `VS_XAML_TYPE`), which XAML Islands does not use. `MainWindow` is the app's top-level window — the ActiveX control has no window of its own.

**`shared/`** — All XAML pages/views reused by both the app and ActiveX control. Contains the `PivoxServices` service locator that breaks the dependency from pages back to the `App` class. OAuth flow logic lives here because it's tied to the XAML pages' sign-in buttons.

**`activex/`** — The ATL-based ActiveX control. Hosts shared library XAML content via XAML Islands (`DesktopWindowXamlSource`). Contains COM plumbing (IDL, registry script, exports) and the drag source bridge.

## The Shared Library

**PivoxShared** is a WinRT component library — a DLL that exports WinRT types discoverable via `.winmd` metadata. Both the app and the ActiveX control consume it.

### What it contains

- **XAML pages** — LoginPage, RegisterPage (and future views)
- **Custom controls** — In `Controls/` (future)
- **ResourceDictionaries** — In `Resources/` (future: SVG-wrapped icons)
- **GoogleOAuth** — OAuth2Manager coroutine for Google Sign-In
- **PivoxServices** — Static service locator providing access to `WinAppState` and `WinAuthService`

### How it's consumed

**By the app (Pivox):**
1. App creates `WinAppState` and `WinAuthService` instances
2. App calls `PivoxServices::initialize(appState, authService)` to inject them into the shared DLL
3. App navigates to shared pages via `Frame.Navigate()` with string-based `TypeName`
4. XAML runtime discovers page types through the shared DLL's `module.g.cpp` type registrations

**By the ActiveX control (PivoxActiveX):**
1. Control initializes Windows App SDK via `DeploymentManager::Initialize()`
2. Control creates services and calls `PivoxServices::initialize()` if the app hasn't already
3. `XamlIslandHost` creates a `DesktopWindowXamlSource` inside the control's HWND
4. Shared pages are loaded via the same `TypeName`-based navigation

### Why no ms-appx:/// URIs

The shared library must work in both contexts — WinUI 3 Application and XAML Islands. The `ms-appx:///` URI scheme resolves resources from the app package, but:
- XAML Islands does not have an app package context
- The ActiveX control runs inside an arbitrary host process (no `.appx` / `.msix`)

All resources use `{ThemeResource}`, `{StaticResource}`, and inline SVG path data instead.

## The ActiveX Control

### Architecture

**ATL-based** — Uses `CComObjectRootEx`, `CComCoClass`, `CComControl`, and ATL interface implementation templates. No raw COM/OLE boilerplate.

**XAML Islands hosting** — The control creates a child HWND inside the ActiveX control's in-place HWND (received via `IOleInPlaceSite`). A `DesktopWindowXamlSource` is attached to this child HWND, and XAML content from the shared library is loaded into it.

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
3. **Running** — XAML content renders inside the host. Drag operations use WinUI `DragStarting` events bridged to OLE `IDataObject`.
4. **Deactivation** — `IOleInPlaceObject::InPlaceDeactivate()`. XAML Island is torn down cleanly before COM release.

### Drag source

Drag operations originate from WinUI content via the `DragStarting` event. `DragSource` attaches to XAML elements and uses `DataPackage.SetData(formatId, value)` with custom clipboard format strings. WinRT handles format registration. The control is source only — never a drop target.

### Registration

The control registers in HKLM (requires elevation via `regsvr32`). The registry script (`PivoxControl.rgs`) creates:
- ProgID: `Pivox.PivoxControl.1` / `Pivox.PivoxControl`
- CLSID entries with `InprocServer32`, `ThreadingModel=Apartment`
- `Control` and `MiscStatus` keys for ActiveX container discovery

## CMake Target Relationships

```
pivox_core (STATIC)
    └── pivox_win_state (STATIC) — links pivox_core, Firebase C++ SDK
            ├── PivoxShared (SHARED/DLL) — WinRT component, links pivox_win_state
            │       ├── Pivox (EXE) — WinUI 3 app, links PivoxShared + pivox_win_state
            │       └── PivoxActiveX (SHARED/.ocx) — ActiveX, links PivoxShared + pivox_win_state
            └── pivox_win_tests (EXE) — Google Test, links pivox_win_state
```

### Directory.Build.props

Scoped by `$(ProjectName)` condition to avoid affecting unrelated CMake targets:
- **All Windows targets** — `NuGetTargetMoniker=native,Version=v0.0`
- **Pivox** — `RuntimeIdentifiers=win10-x64;win10-arm64`
- **PivoxShared** — `RuntimeIdentifiers=win10-x86;win10-x64;win10-arm64`
- **PivoxActiveX** — `RuntimeIdentifiers=win10-x86`

### Directory.Build.targets

- **Pivox, PivoxShared** — Includes `module.g.cpp` in compilation, sets empty MIDL `OutputDirectory` (CMake workaround)
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

### App + shared library (x64)

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

### App + shared library (arm64)

```bash
mkdir build-win-arm64 && cd build-win-arm64
cmake -G "Visual Studio 18 2026" -A ARM64 ..
cmake --build .
```

### ActiveX control (x86 only, separate build dir)

```bash
mkdir build-activex-x86 && cd build-activex-x86
cmake -G "Visual Studio 18 2026" -A Win32 ..

# Debug
cmake --build . --target PivoxActiveX

# Release
cmake --build . --config Release --target PivoxActiveX

# Register (elevated)
regsvr32 Debug\PivoxActiveX.ocx
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

The shared library and ActiveX control must never use `ms-appx:///` URIs. XAML Islands has no app package context. All resources use `{ThemeResource}`, `{StaticResource}`, inline path data, or programmatic loading.

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

// 3. Create the XAML Island source.
auto xamlSource = winrt::Microsoft::UI::Xaml::Hosting::DesktopWindowXamlSource();

// 4. Initialize with the top-level ancestor window's WindowId.
HWND topLevel = ::GetAncestor(hwnd, GA_ROOT);
auto windowId = winrt::Microsoft::UI::GetWindowIdFromWindow(topLevel);
xamlSource.Initialize(windowId);

// 5. Position the SiteBridge inside the host window.
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

// 6. Set content.
winrt::Microsoft::UI::Xaml::Controls::Button btn;
btn.Content(winrt::box_value(L"Hello XAML Islands!"));
xamlSource.Content(btn);
```

Reposition the bridge on both `WM_SIZE` and `WM_MOVE` using the same coordinate mapping.

#### What does NOT work with WinUI 3

| Approach | Error | Why |
|----------|-------|-----|
| `WindowsXamlManager::InitializeForCurrentThread()` then `Initialize(windowId)` | `RO_E_CLOSED` (0x80000013) | They conflict — WindowsXamlManager takes ownership of the XAML lifecycle |
| `WindowsXamlManager` without `Initialize(windowId)` | `SiteBridge()` returns null | No window association, renderer crashes on internal type activation |
| `IDesktopWindowXamlSourceNative2::AttachToWindow()` | `E_NOINTERFACE` (0x80004002) | UWP-only interface, not implemented on WinUI 3's DesktopWindowXamlSource |
| `SetParent(bridgeHwnd, hostHwnd)` | `RO_E_CLOSED` on `MoveAndResize()` | Reparenting breaks the bridge's internal state |
| `Application::Start()` | Blocks forever | It runs its own message loop; the host process owns the pump |
| `IApplicationFactory::CreateInstance()` | Crash in `ActivateInstance` | Doesn't initialize the type system; internal factories are null |

#### Hosting compiled XAML from external WinRT component DLLs

To load XAML `UserControl`s from a separate WinRT component DLL, three pieces must be in place:

**1. Side-by-side manifest for WinRT activation**

The host DLL needs a manifest file (`<host>.dll.manifest`) next to it registering each WinRT type from the component:

```xml
<?xml version="1.0" encoding="utf-8"?>
<assembly manifestVersion="1.0" xmlns="urn:schemas-microsoft-com:asm.v1"
          xmlns:asmv3="urn:schemas-microsoft-com:asm.v3">
  <assemblyIdentity version="1.0.0.0" name="MyHost"/>
  <asmv3:file name="MyComponent.dll">
    <activatableClass name="MyComponent.MyControl"
        threadingModel="both" xmlns="urn:schemas-microsoft-com:winrt.v1" />
    <activatableClass name="MyComponent.XamlMetaDataProvider"
        threadingModel="both" xmlns="urn:schemas-microsoft-com:winrt.v1" />
  </asmv3:file>
</assembly>
```

The host must activate this manifest at startup using `CreateActCtxW` + `ActivateActCtx`.

**2. WinRT component DLL exports**

The component DLL must export `DllGetActivationFactory` and `DllCanUnloadNow`. CppWinRT's `module.g.cpp` generates `WINRT_GetActivationFactory` and `WINRT_CanUnloadNow` — map them via a `.def` file:

```
EXPORTS
DllCanUnloadNow = WINRT_CanUnloadNow                    PRIVATE
DllGetActivationFactory = WINRT_GetActivationFactory    PRIVATE
```

**3. Merged PRI for XAML resource loading (MRT)**

MRT (Modern Resource Technology) resolves compiled XAML binaries (XBF) via `.pri` files. In unpackaged apps, MRT only discovers `resources.pri` in the process/DLL directory. Component DLLs have their own `.pri` files but MRT ignores them.

**Solution:** Merge all PRIs into a single `resources.pri` at build time:

```cmd
makepri createconfig /cf priconfig.xml /dq en-US /o
makepri new /pr <output_directory> /cf priconfig.xml /of resources.pri /o
```

This scans the directory tree, finds all XBF files and existing `.pri` files, and produces a merged `resources.pri`.

**Note:** `makepri merge` does not exist in modern Windows SDKs despite some documentation suggesting otherwise.

**Required output directory structure:**
```
<output>/
  MyHost.dll                       # Host DLL
  MyHost.dll.manifest              # Activation context
  MyComponent.dll                  # WinRT component
  MyComponent.pri                  # Component's PRI (input to merge)
  resources.pri                    # Merged PRI (output of makepri)
  MyComponent/
    MyControl.xbf                  # Compiled XAML binary
  Microsoft.WindowsAppRuntime.Bootstrap.dll
```

#### XAML content approaches

| Approach | Works? | Tradeoffs |
|----------|--------|-----------|
| **Programmatic C++** | Yes | No resource loading needed. Verbose. |
| **`XamlReader::Load()` from string** | Yes | Runtime XAML parsing. No `x:Name` bindings. No compile-time validation. |
| **Compiled XAML from external DLL** | Yes (with PRI merge) | Best performance. Requires build-time PRI merge and activation manifest. |

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

#### WinRT component DLL configuration

Use the **Windows Runtime Component (C++/WinRT)** Visual Studio template. Key properties:

```xml
<CppWinRTOptimized>true</CppWinRTOptimized>
<CppWinRTRootNamespaceAutoMerge>true</CppWinRTRootNamespaceAutoMerge>
<CppWinRTGenerateWindowsMetadata>true</CppWinRTGenerateWindowsMetadata>
<CppWinRTEnableLegacyCoroutines>false</CppWinRTEnableLegacyCoroutines>
<ApplicationType>Windows Store</ApplicationType>
<WindowsAppSDKSelfContained>false</WindowsAppSDKSelfContained>
```

`SelfContained=false` prevents copying 36 WinUI runtime DLLs to the output — the host's bootstrap loads them from the framework package.

XAML code-behind pattern — do NOT call `InitializeComponent` in the constructor:

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

#### Lifecycle

1. Initialize: Bootstrap → DispatcherQueue → DesktopWindowXamlSource → Initialize(windowId) → SiteBridge → Content
2. Resize/Move: Remap coordinates and call `bridge.MoveAndResize()`
3. Shutdown: Close `DesktopWindowXamlSource` before releasing COM references. Do NOT call `MddBootstrapShutdown` if other instances may still be active.

#### Framework-dependent deployment

The control requires the Windows App SDK runtime on the target machine:
```
winget install Microsoft.WindowsAppRuntime.1.8
```

`Microsoft.WindowsAppRuntime.Bootstrap.dll` must be in the same directory as the host DLL.

#### Framework-dependent deployment

The ActiveX control is **not** self-contained — it requires the Windows App SDK runtime installed on the target machine. `MddBootstrapInitialize` loads the runtime from the MSIX framework package. Install via:
```
winget install Microsoft.WindowsAppRuntime.1.8
```

The `Microsoft.WindowsAppRuntime.Bootstrap.dll` must be in the same directory as the ActiveX DLL, or the bootstrap call will fail.

### XAML Islands lifecycle

XAML Islands must be cleanly shut down before COM release:
1. Close `DesktopWindowXamlSource` first
2. Then release COM references
3. Do NOT call `MddBootstrapShutdown` if other control instances may still be active

The `InPlaceDeactivate` → close sequence handles this. The bootstrap and DispatcherQueue are process/thread-wide and should only be cleaned up when the DLL unloads.

### App.xaml / ApplicationDefinition

The `App.xaml` with `VS_XAML_TYPE "ApplicationDefinition"` is specific to the WinUI 3 app. XAML Islands does NOT use an Application subclass — it initializes the XAML runtime directly via `DesktopWindowXamlSource.Initialize(windowId)`. The ActiveX control must NOT have an `ApplicationDefinition`.

### App.idl is an empty namespace block

`App.idl` must be `namespace Pivox {}` — NOT a runtimeclass. Declaring App as a runtimeclass generates conflicting factory functions that prevent the app from launching.

### App.xaml.cpp does NOT include App.g.cpp

Unlike other windows/pages, `App.xaml.cpp` must not include `App.g.cpp`. Other windows (MainWindow) and pages (LoginPage, RegisterPage) include their respective `.g.cpp`.

### Static variable duplication

Both the app and the shared DLL link `pivox_win_state` (static library). This means each module has its own copy of file-scope statics in WinAuthService.cpp (notably `s_googleOAuthLauncher`). This is safe because:
- `s_googleOAuthLauncher` is set by `GoogleOAuth.cpp` (in the shared DLL)
- `signInWithGoogleAsync` is only called from pages (also in the shared DLL)
- Both accesses use the DLL's copy of the static

The app calls other WinAuthService methods (e.g., `initializeFirebase`, `signOut`) through its own copy of the static lib, which is safe because those methods don't use `s_googleOAuthLauncher`.

### PivoxServices initialization order

`PivoxServices::initialize()` must be called before any shared page is instantiated. The app does this in `App::App()`. The ActiveX control does this in `DoVerb(OLEIVERB_INPLACEACTIVATE)`.

## Suppressed Warnings

All warnings are treated as errors (`/W4 /WX` for compiler, `/WX` for linker). The following are explicitly suppressed:

| Warning | Scope | Reason |
|---------|-------|--------|
| **C4100** (unreferenced parameter) | All MSVC targets | Equivalent to GCC/Clang `-Wno-unused-parameter`. Common in interface implementations and callback signatures where parameters are part of the contract but unused. |
| **C4251** (DLL-interface for class members) | `PivoxServices.h` | `std::shared_ptr` private members in a `__declspec(dllexport)` class. Safe — these members are only accessed through exported methods, never across the DLL boundary directly. |
| **LNK4099** (missing PDB) | All Windows targets | Firebase C++ SDK 13.5.0 pre-built `.lib` files ship without `.pdb` files. Cannot fix without building Firebase from source. |
| **LNK4075** (/INCREMENTAL vs /OPT) | WinUI/CppWinRT targets | NuGet targets inject `/OPT:ICF` which conflicts with CMake's default `/INCREMENTAL` in Debug builds. Harmless — incremental linking is simply disabled. |
| **LNK4078** (multiple .drectve sections) | PivoxActiveX | Custom `.def` file and CppWinRT both inject linker directives. The sections have different attributes but the final exports are correct. |

## Icons Approach

Icons use SVG path data wrapped in XAML `ResourceDictionary` entries, sourced from Microsoft's [fluentui-system-icons](https://github.com/microsoft/fluentui-system-icons) repository.

**Why not FluentIcons.WinUI NuGet:**
- The NuGet package loads icon fonts via `ms-appx:///` URIs
- XAML Islands has no app package context — font loading would fail
- SVG path data in `ResourceDictionary` works in both Application and XAML Islands contexts

Icon definitions go in `shared/Resources/` as `.xaml` `ResourceDictionary` files containing `PathGeometry` or `Path` elements. Both the app and ActiveX control merge these dictionaries.
