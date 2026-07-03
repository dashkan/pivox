# WinRT Shared DLL Lessons Learned

> **Status (2026): legacy / reference.** These lessons come from the
> Native App's Windows build, where `WinAuthService` links the Firebase
> C++ SDK (the "Firebase static duplication" pitfalls below). The Native
> App is now a legacy/reference target and Firebase is no longer the
> Pivox auth system (the cloud is Keycloak-only, `internal/oidc`). The
> DLL/linking lessons remain technically valid for the legacy app; the
> Firebase specifics are historical. See `AGENTS.md` for the current model.

This documents the pitfalls encountered when using a shared WinRT component DLL (`PivoxShared.dll`) between a WinUI 3 app and an ActiveX control. We ultimately chose shared source compilation instead, but this reference captures the knowledge for future use.

## Why We Tried It

The idea was clean separation: shared XAML pages + services in a DLL, consumed by both the app (x64 EXE) and ActiveX plugin (x86 DLL). Standard WinRT component pattern.

## What Worked

- WinRT component DLL with `CppWinRTComponent=true` compiles XAML to XBF, generates winmd, produces PRI
- `DllGetActivationFactory` / `DllCanUnloadNow` exports via DEF file enable WinRT class activation
- `activatableClass` entries in consumer manifests register types for activation
- `ResourceManagerRequested` on Application redirects MRT to the DLL's PRI
- `AppxPriInitialPath` MSBuild property aligns PRI embed path with `RootNamespace` when project name differs

## Pitfalls

### 1. PRI Embed Path Mismatch

**Problem:** XAML compiler generates `InitializeComponent` with URI `ms-appx:///Pivox/LoginPage.xaml` (from `RootNamespace=Pivox`), but PRI embeds XBF at `PivoxShared/LoginPage.xbf` (from project name `PivoxShared`). Results in `0x802B000A` (XAML_E_RESOURCE_NOT_FOUND).

**Fix:** Set `AppxPriInitialPath` to match `RootNamespace`:
```cmake
VS_GLOBAL_AppxPriInitialPath "Pivox"
```

This is an undocumented but critical MSBuild property found in `Microsoft.Build.Msix.Pri.targets`. It defaults to `$(TargetName)` (project name). Gemini hallucinated a non-existent `ControlNameForUserRoot` property — don't use that.

### 2. XamlMetaDataProvider Namespace Conflict

**Problem:** Both the app and the shared DLL in the same namespace (`Pivox`) generate `Pivox.XamlMetaDataProvider`. MIDL fails with MIDL2003 redefinition.

**Fix:** Change the app's `RootNamespace` to something different (e.g., `PivoxApp`). The app's types become `PivoxApp.MainWindow`, shared types stay `Pivox.LoginPage`. Output filename unchanged via `OUTPUT_NAME`.

**With shared source:** Not needed. Each target generates its own provider in its own binary — they never conflict.

### 3. Frame.Navigate Fails in XAML Islands

**Problem:** `Frame.Navigate(TypeName)` crashes with `0x802B000A` in XAML Islands. The XBF resource lookup via `ms-appx://` doesn't work in the XAML Islands resource context.

**Fix:** Use `get_activation_factory` + `ActivateInstance` instead of `Frame.Navigate`. The activation factory goes through the manifest's `activatableClass` entries, which correctly resolve the DLL.

**With shared source:** Not needed in the app (Frame.Navigate works in full WinUI). Still needed in ActiveX (XAML Islands).

### 4. Manifest Required for WinRT Activation

**Problem:** `Frame.Navigate` or `ActivateInstance` for types in the shared DLL causes infinite recursion (App constructor called repeatedly) or null pointer crash in `ActivationAPI::ActivateInstance`.

**Fix:** Both the app and ActiveX need `activatableClass` entries in their manifests pointing to the DLL:
```xml
<file name="PivoxShared.dll">
    <activatableClass name="Pivox.LoginPage" threadingModel="both"
        xmlns="urn:schemas-microsoft-com:winrt.v1" />
</file>
```

**With shared source:** App doesn't need manifest entries (types are in the EXE). ActiveX still needs them (types activated via manifest in XAML Islands).

### 5. DLL Export Complexity

**Problem:** Every class/function accessed across the DLL boundary needs `__declspec(dllexport/dllimport)`. `PIVOX_SHARED_EXPORTS` macro on the DLL, consumers use `dllimport`. Firebase types in headers pull Firebase SDK into every consumer.

**Fix:** Forward-declare Firebase types, export only the public API surface. Tedious and fragile.

**With shared source:** Not needed. No DLL boundary, no exports.

### 6. Firebase Static Duplication

**Problem:** `WinAuthService` in a static lib (`pivox_win_state`) linked by both the EXE and DLL creates two copies of Firebase statics. The app initializes Firebase in its copy, but `LoginPage` in the DLL calls auth through a different copy where Firebase isn't initialized. Results in `assert(notifier)` crash in Firebase SDK.

**Fix:** Move `WinAuthService` to the shared DLL so Firebase statics exist in one place. But then the DLL needs Firebase link libs, and the app can't call `WinAuthService` methods without `dllimport`.

**With shared source:** Not an issue. One binary, one copy of statics.

### 7. PRI Copy Deployment

**Problem:** The shared DLL's PRI must be in the consumer's output directory. CMake's `target_link_libraries` doesn't copy PRI files — only the DLL. MSBuild's `ProjectReference` with `Private=true` copies the DLL but not always the PRI.

**Fix:** Post-build `copy_if_different` command in CMake:
```cmake
add_custom_command(TARGET ${AX_TARGET} POST_BUILD
    COMMAND ${CMAKE_COMMAND} -E copy_if_different
        "$<TARGET_FILE_DIR:PivoxShared>/Pivox.pri"
        "$<TARGET_FILE_DIR:${AX_TARGET}>/Pivox.pri")
```

**With shared source:** PRI generated by the target itself. No copying needed.

### 8. NuGet Package Restore Across Projects

**Problem:** NuGet restore is per-project. The shared DLL needs its own NuGet references. `cmake --build` with `-t:Pivox` triggers NuGet restore on the target project but not always on dependencies. First build fails, second succeeds ("build twice" problem).

**Fix:** Use `msbuild /restore` (which `cmake --build` does automatically) or manually restore before building.

## When to Use a Shared DLL

Despite the complexity, a shared WinRT component DLL is appropriate when:
- Multiple unrelated host processes consume the same component (different vendors, different apps)
- The component needs to be versioned/deployed independently of its consumers
- The component is large and build time for dual compilation is prohibitive
- The component is closed-source and distributed as a binary

For our case — two targets (app + ActiveX) in the same repo with the same source — shared source compilation is simpler and correct.
