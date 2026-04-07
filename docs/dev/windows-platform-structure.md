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

### Message loop integration

The ActiveX host owns the message loop. The XAML Island's dispatcher must integrate with the host's pump via `IDesktopWindowXamlSourceNative2::PreTranslateMessage`. The `XamlIslandHost::ProcessMessage()` method handles this — the host should call it for each `MSG` before `TranslateMessage`/`DispatchMessage`.

### XAML Islands lifecycle

XAML Islands must be cleanly shut down before COM release:
1. Close `DesktopWindowXamlSource` first
2. Then release COM references
3. Failure to do this causes access violations during DLL unload

The `InPlaceDeactivate` → `XamlIslandHost::Shutdown()` path handles this.

### App.xaml / ApplicationDefinition

The `App.xaml` with `VS_XAML_TYPE "ApplicationDefinition"` is specific to the WinUI 3 app. XAML Islands does NOT use an Application subclass — it initializes the XAML runtime directly via `DesktopWindowXamlSource`. The ActiveX control must NOT have an `ApplicationDefinition`.

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
