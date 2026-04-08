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

### WinUI 3 XAML Islands initialization (CRITICAL)

Hosting WinUI 3 content in an ActiveX control via XAML Islands required significant trial and error. The WinUI 3 API differs substantially from the UWP XAML Islands documentation on learn.microsoft.com. This section documents the **exact working pattern** and every dead end encountered.

**Reference implementation:** `D:\activex\ATLProject1` — a clean ATL project with a working WinUI 3 Button hosted via XAML Islands.

#### The working initialization sequence

```cpp
// In WM_CREATE handler (control must have m_bWindowOnly = TRUE):

// 1. Bootstrap Windows App SDK runtime (process-wide, once).
PACKAGE_VERSION minVersion{};
minVersion.Version = WINDOWSAPPSDK_RUNTIME_VERSION_UINT64;
MddBootstrapInitialize(WINDOWSAPPSDK_RELEASE_MAJORMINOR,
    WINDOWSAPPSDK_RELEASE_VERSION_TAG_W, minVersion);

// 2. DispatcherQueue (thread-level, once).
if (!DispatcherQueue::GetForCurrentThread())
    dispatcherController = DispatcherQueueController::CreateOnCurrentThread();

// 3. Create XAML Island — NO WindowsXamlManager.
xamlSource = DesktopWindowXamlSource();

// 4. Initialize with top-level window's WindowId.
HWND topLevel = ::GetAncestor(m_hWnd, GA_ROOT);
auto windowId = GetWindowIdFromWindow(topLevel);
xamlSource.Initialize(windowId);

// 5. Position the SiteBridge — NO SetParent.
auto bridge = xamlSource.SiteBridge();
POINT pt = { 0, 0 };
::ClientToScreen(m_hWnd, &pt);
::ScreenToClient(topLevel, &pt);
RECT rc;
::GetClientRect(m_hWnd, &rc);
bridge.MoveAndResize({ pt.x, pt.y, rc.right, rc.bottom });
bridge.Show();

// 6. Set WinUI content.
Controls::Button btn;
btn.Content(winrt::box_value(L"Hello XAML Islands!"));
xamlSource.Content(btn);
```

#### Dead ends — what does NOT work

| Approach | Error | Why |
|----------|-------|-----|
| `WindowsXamlManager::InitializeForCurrentThread()` before `Initialize(windowId)` | `RO_E_CLOSED` (0x80000013) on `Initialize()` | WindowsXamlManager takes ownership of the XAML runtime lifecycle. Calling `Initialize(windowId)` after it conflicts — the source is already "closed" from WindowsXamlManager's perspective. |
| `WindowsXamlManager` without `Initialize(windowId)` | `SiteBridge()` returns null, crash in `ActivateInstance` during rendering | Without `Initialize(windowId)`, the `DesktopWindowXamlSource` has no window association. SiteBridge is null. Even if content is set, the XAML renderer crashes trying to activate internal types. |
| `IDesktopWindowXamlSourceNative2::AttachToWindow()` | `E_NOINTERFACE` (0x80004002) | This is the **UWP XAML Islands** COM interop interface (`Windows.UI.Xaml`). WinUI 3's `DesktopWindowXamlSource` (`Microsoft.UI.Xaml`) does not implement it. |
| `SetParent(bridgeHwnd, controlHwnd)` | `RO_E_CLOSED` on next `bridge.MoveAndResize()` | Reparenting the SiteBridge's HWND breaks its internal state. The bridge was created as a child of the top-level window by `Initialize(windowId)`. `SetParent` invalidates that relationship. |
| `Application` via `IApplicationFactory::CreateInstance()` | Crash in `ActivateInstance` during rendering | Creating a raw Application object doesn't initialize the XAML type system. The internal activation factories remain null. Standard controls (TextBlock, Button) crash during rendering. |
| `Application::Start()` | Blocks forever | `Start()` runs its own message loop. In an ActiveX control, the host owns the message loop. Never call `Start()`. |
| Windowless control (default ATL) | `m_hWnd` is null | XAML Islands requires a real HWND. Set `m_bWindowOnly = TRUE` in the control constructor. |

#### Build configuration — COM IDL + CppWinRT coexistence

An ATL ActiveX project has COM IDL (for the control's interfaces/coclass). Adding CppWinRT NuGet causes CppWinRT to hijack MIDL processing, trying to generate `.winmd` from COM IDL and run `mdmerge`. This fails because COM IDL produces `.tlb`, not `.winmd`.

**Solution — three MSBuild properties in the project's `<PropertyGroup Label="Globals">`:**

```xml
<!-- Consume WinRT APIs but don't generate .winmd from COM IDL -->
<CppWinRTOptimized>true</CppWinRTOptimized>
<CppWinRTProjectStyle>None</CppWinRTProjectStyle>
<CppWinRTModernIDL>false</CppWinRTModernIDL>
```

| Property | Effect |
|----------|--------|
| `CppWinRTProjectStyle=None` | Skips `mdmerge` — no .winmd merge step |
| `CppWinRTModernIDL=false` | Prevents CppWinRT from injecting `/reference` flags and `/nomidl` into MIDL. COM IDL is processed by standard MIDL, producing `.tlb`, `_i.h`, `_i.c`. |
| No `CppWinRTEnabled` | Let the NuGet package set this automatically. Setting it explicitly changes `CppWinRTPath` and breaks the `cppwinrt.exe` tool path. |
| No `UseWinUI` | The ActiveX project has no XAML files. `UseWinUI` triggers XAML compilation which conflicts with COM IDL processing. |

#### SiteBridge positioning

The `SiteBridge` is a child of the **top-level window** (set by `Initialize(windowId)`), not the control's HWND. To position XAML content inside the ActiveX control, map the control's client coordinates to the top-level window's client coordinates:

```cpp
POINT pt = { 0, 0 };
::ClientToScreen(m_hWnd, &pt);          // Control → screen
::ScreenToClient(topLevelHwnd, &pt);     // Screen → top-level client
bridge.MoveAndResize({ pt.x, pt.y, width, height });
```

Update this mapping in `WM_SIZE` to handle control resize.

#### XAML content approaches — what works and what doesn't

**Three approaches to setting XAML content in the island:**

| Approach | Works? | Notes |
|----------|--------|-------|
| **Programmatic C++** (`Button btn; btn.Content(...)`) | **Yes** | No resource loading needed. Framework package provides all types. |
| **`XamlReader::Load()` from string** | **Yes** | Parses XAML at runtime. Buttons, TextBlocks, SVG Path icons all render. No PRI/XBF needed. |
| **Compiled XAML UserControl from external DLL** | **No** | Fails with `0x802B000A` (XAML parse). MRT can't find the XBF in the PRI. |

**`XamlReader::Load()` is the recommended approach for XAML Islands content.** It avoids the MRT resource loading problem entirely while still allowing declarative XAML markup. Example:

```cpp
auto content = winrt::Microsoft::UI::Xaml::Markup::XamlReader::Load(
    LR"(<StackPanel xmlns="http://schemas.microsoft.com/winfx/2006/xaml/presentation"
                    Background="#1E1E1E" Padding="20" Spacing="16">
        <TextBlock Text="Hello!" FontSize="24" Foreground="White" />
        <Button Content="Click Me!" />
        <Viewbox Width="48" Height="48">
            <Canvas Width="24" Height="24">
                <Path Fill="White" Data="M12 2C6.477..." />
            </Canvas>
        </Viewbox>
    </StackPanel>)");
xamlSource.Content(content.as<UIElement>());
```

#### WinRT component activation from external DLLs

Plain WinRT runtimeclasses (non-XAML) activate successfully from external DLLs via manifest-based activation context:

1. **Side-by-side manifest** (`ATLProject1.dll.manifest`) next to the ActiveX DLL with `activatableClass` entries
2. **`CreateActCtxW` + `ActivateActCtx`** in the control's `OnCreate` to activate the manifest
3. **`DllGetActivationFactory`** exported from the WinRT component DLL (via `.def` file mapping `WINRT_GetActivationFactory`)
4. **`winrt::get_activation_factory<IActivationFactory>(className)`** to get the factory
5. **`factory.ActivateInstance<T>()`** to create instances

**Manifest format:**
```xml
<asmv3:file name="TestControls.dll">
  <activatableClass name="TestControls.Greeter"
      threadingModel="both" xmlns="urn:schemas-microsoft-com:winrt.v1" />
</asmv3:file>
```

**Known issue:** The activation context cookie becomes invalid after the control is destroyed and recreated. Second insert in the test container fails with `CLASS_NOT_REGISTERED`. Fix: re-create the activation context on each `OnCreate`, or make it process-lifetime.

#### Compiled XAML from external DLLs (SOLVED — PRI merging)

Loading a compiled XAML `UserControl` from a WinRT component DLL initially fails with `0x802B000A` because MRT (Modern Resource Technology) can't find the XBF resources.

**Root cause:** In XAML Islands without an Application lifecycle, MRT only looks at `resources.pri` in the process/DLL directory. Component DLLs have their own `.pri` files but MRT doesn't discover them automatically.

**Solution — merge PRIs at build time using `makepri`:**

```cmd
cd /d <output_directory>
makepri createconfig /cf priconfig.xml /dq en-US /o
makepri new /pr . /cf priconfig.xml /of resources.pri /o
```

This scans the output directory, finds all XBF resources and existing `.pri` files, and produces a merged `resources.pri`. MRT discovers this automatically.

**Required directory structure (proven working):**
```
Debug/
  ATLProject1.dll              # ActiveX control (host)
  ATLProject1.dll.manifest     # Activation context manifest
  TestControls.dll             # WinRT component with XAML UserControl
  TestControls.pri             # Component's PRI (input to merge)
  resources.pri                # Merged PRI (output of makepri)
  TestControls/
    TestPanel.xbf              # Compiled XAML binary
  Microsoft.WindowsAppRuntime.Bootstrap.dll
```

**PRI resource mapping (from `makepri dump`):**
```xml
<ResourceMap name="TestControls">
  <ResourceMapSubtree name="Files">
    <ResourceMapSubtree name="TestControls">
      <NamedResource name="TestPanel.xbf"
          uri="ms-resource://TestControls/Files/TestControls/TestPanel.xbf">
        <Value>TestControls\TestPanel.xbf</Value>
      </NamedResource>
    </ResourceMapSubtree>
  </ResourceMapSubtree>
</ResourceMap>
```

**For CMake:** Add a post-build step that runs `makepri createconfig` + `makepri new` to merge all component PRIs into `resources.pri`.

**Note:** `makepri merge` does not exist in modern SDKs despite documentation suggesting otherwise. Use `makepri new /pr . /cf priconfig.xml /of resources.pri /o` instead.

#### WinRT component DLL project configuration

The WinRT component DLL (TestControls) uses the **Windows Runtime Component (C++/WinRT)** VS template with these modifications:

**NuGet packages:**
- `Microsoft.Windows.CppWinRT` (latest)
- `Microsoft.WindowsAppSDK` (1.8.x) — for `Microsoft.UI.Xaml` namespace

**Key properties (in `<PropertyGroup Label="Globals">`):**
```xml
<CppWinRTOptimized>true</CppWinRTOptimized>
<CppWinRTRootNamespaceAutoMerge>true</CppWinRTRootNamespaceAutoMerge>
<CppWinRTGenerateWindowsMetadata>true</CppWinRTGenerateWindowsMetadata>
<CppWinRTEnableLegacyCoroutines>false</CppWinRTEnableLegacyCoroutines>
<AppContainerApplication>true</AppContainerApplication>
<ApplicationType>Windows Store</ApplicationType>
<WindowsAppSDKSelfContained>false</WindowsAppSDKSelfContained>
```

**DLL exports (`.def` file):**
```
EXPORTS
DllCanUnloadNow = WINRT_CanUnloadNow                    PRIVATE
DllGetActivationFactory = WINRT_GetActivationFactory    PRIVATE
```

**XAML code-behind pattern (from VS wizard):**
```cpp
// TestPanel.h — NO InitializeComponent in constructor
TestPanel() { }

// TestPanel.cpp — conditional include for generated code
#include "pch.h"
#include "TestPanel.h"
#if __has_include("TestPanel.g.cpp")
#include "TestPanel.g.cpp"
#endif
```

## CMake porting plan

When porting the proven pattern from `D:\activex\ATLProject1` back to the Pivox CMake project:

### ActiveX target (PivoxActiveX)

1. **Remove custom MIDL command** — use MSBuild-native MIDL with `CppWinRTProjectStyle=None` and `CppWinRTModernIDL=false` via `VS_GLOBAL_*` properties
2. **Add `MddBootstrapInitialize`** in `OnCreate` with `std::once_flag` for process-wide init
3. **Add `DispatcherQueueController::CreateOnCurrentThread`** guarded by `GetForCurrentThread()` check
4. **Use `DesktopWindowXamlSource` + `Initialize(windowId)`** — NO `WindowsXamlManager`
5. **Position via `SiteBridge().MoveAndResize()`** with coordinate mapping — NO `SetParent`
6. **Use `XamlReader::Load()`** for XAML content from strings
7. **Link `Microsoft.WindowsAppRuntime.Bootstrap.lib`** for bootstrap API
8. **Embed manifest** with `activatableClass` entries for PivoxShared types
9. **Copy `Microsoft.WindowsAppRuntime.Bootstrap.dll`** to output via MSBuild `Private=true`
10. **Handle `WM_SIZE` and `WM_MOVE`** — call `UpdateBridgePosition()` on both

### Shared library target (PivoxShared)

1. **Keep current CMake configuration** — `CppWinRTComponent=true`, `UseWinUI=true`, `ApplicationType "Windows Store"`
2. **Export `DllGetActivationFactory`/`DllCanUnloadNow`** via `.def` file mapping `WINRT_*` symbols
3. **Plain runtimeclasses** (Greeter-style) work via manifest activation
4. **Compiled XAML UserControls** need MRT solution (TBD) or `XamlReader::Load()` workaround

### Key CMake properties mapping

| MSBuild Property | CMake Equivalent |
|-----------------|------------------|
| `CppWinRTProjectStyle=None` | `VS_GLOBAL_CppWinRTProjectStyle "None"` |
| `CppWinRTModernIDL=false` | `VS_GLOBAL_CppWinRTModernIDL "false"` |
| `CppWinRTEnableLegacyCoroutines=false` | `VS_GLOBAL_CppWinRTEnableLegacyCoroutines "false"` |
| `WindowsAppSDKSelfContained=false` | `VS_GLOBAL_WindowsAppSDKSelfContained "false"` |
| `AppContainerApplication=false` | `VS_GLOBAL_AppContainerApplication "false"` |
| Manifest embedding | `LINK_FLAGS "/MANIFEST:EMBED /MANIFESTINPUT:..."` |

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
