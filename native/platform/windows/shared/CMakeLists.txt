# ============================================================
# PivoxShared — WinRT component library (DLL + .winmd)
# Shared XAML pages/controls consumed by the WinUI 3 app
# and the ActiveX control (via XAML Islands).
# ============================================================

set(SHARED_TARGET "PivoxShared")
set(SHARED_DIR "${CMAKE_CURRENT_SOURCE_DIR}")

add_library(${SHARED_TARGET} SHARED
    # Service locator
    ${SHARED_DIR}/PivoxServices.cpp

    # Google OAuth (OAuth2Manager coroutine — needs WinRT)
    ${SHARED_DIR}/GoogleOAuth.cpp

    # Drag service (compile-time abstraction: ActiveX manual drag vs native WinUI)
    ${SHARED_DIR}/DragService.cpp

    # LoginPage
    ${SHARED_DIR}/Views/LoginPage.idl
    ${SHARED_DIR}/Views/LoginPage.xaml
    ${SHARED_DIR}/Views/LoginPage.xaml.h
    ${SHARED_DIR}/Views/LoginPage.xaml.cpp

    # RegisterPage
    ${SHARED_DIR}/Views/RegisterPage.idl
    ${SHARED_DIR}/Views/RegisterPage.xaml
    ${SHARED_DIR}/Views/RegisterPage.xaml.h
    ${SHARED_DIR}/Views/RegisterPage.xaml.cpp
)

target_include_directories(${SHARED_TARGET} PRIVATE
    ${SHARED_DIR}
    ${SHARED_DIR}/Views
)
target_include_directories(${SHARED_TARGET} PUBLIC
    ${SHARED_DIR}      # For PivoxServices.h
)

target_compile_definitions(${SHARED_TARGET} PRIVATE PIVOX_SHARED_EXPORTS)

# ActiveX build (x86/Win32): enable desktop APIs and manual drag path.
if(CMAKE_GENERATOR_PLATFORM STREQUAL "Win32")
    target_compile_definitions(${SHARED_TARGET} PRIVATE
        PIVOX_ACTIVEX_HOST
        WINAPI_FAMILY=WINAPI_FAMILY_DESKTOP_APP)
    target_link_libraries(${SHARED_TARGET} PRIVATE ole32 shell32 user32)
endif()

target_link_libraries(${SHARED_TARGET} PRIVATE pivox_win_state crypt32 advapi32)
target_precompile_headers(${SHARED_TARGET} PRIVATE ${SHARED_DIR}/pch.h)

# DEF file exports DllGetActivationFactory/DllCanUnloadNow for WinRT class activation.
target_link_options(${SHARED_TARGET} PRIVATE "/DEF:${SHARED_DIR}/PivoxShared.def")

# WinRT component properties — generates .winmd for type activation.
# ApplicationType "Windows Store" is required for WinUI XAML compilation
# (the NuGet targets gate on it). AppContainerApplication/AppxPackage "false"
# keeps this a regular desktop DLL — no sandbox, no package.
set_target_properties(${SHARED_TARGET} PROPERTIES
    VS_GLOBAL_RootNamespace "Pivox"
    VS_GLOBAL_AppContainerApplication "false"
    VS_GLOBAL_AppxPackage "false"
    VS_GLOBAL_CharacterSet "Unicode"
    VS_GLOBAL_CppWinRTOptimized "true"
    VS_GLOBAL_CppWinRTRootNamespaceAutoMerge "true"
    VS_GLOBAL_CppWinRTComponent "true"
    VS_GLOBAL_UseWinUI "true"
    VS_GLOBAL_ApplicationType "Windows Store"
    VS_GLOBAL_ApplicationTypeRevision "10.0"
    VS_GLOBAL_WindowsTargetPlatformVersion "10.0"
    VS_GLOBAL_WindowsTargetPlatformMinVersion "10.0.17763.0"
    VS_GLOBAL_WindowsPackageType "None"
    VS_GLOBAL_EnablePreviewMsixTooling "true"
)

# NuGet packages — same versions as the app target.
set_property(TARGET ${SHARED_TARGET} PROPERTY VS_PACKAGE_REFERENCES
    "Microsoft.Windows.CppWinRT_2.0.250303.1"
    "Microsoft.WindowsAppSDK_1.8.260317003"
    "Microsoft.Windows.SDK.BuildTools_10.0.26100.7705"
)

# IDL/XAML dependency wiring (same loop pattern as the app).
get_target_property(_SOURCES ${SHARED_TARGET} SOURCES)
foreach(_SOURCE ${_SOURCES})
    cmake_path(GET _SOURCE EXTENSION LAST_ONLY _EXTENSION)
    if(NOT "${_EXTENSION}" STREQUAL ".idl")
        continue()
    endif()
    set(_IDL_SOURCE "${_SOURCE}")
    cmake_path(REMOVE_EXTENSION _SOURCE LAST_ONLY OUTPUT_VARIABLE _BASENAME)
    set(_XAML_SOURCE "${_BASENAME}.xaml")
    if("${_XAML_SOURCE}" IN_LIST _SOURCES)
        set_property(SOURCE "${_IDL_SOURCE}" PROPERTY VS_SETTINGS
            "SubType=Code" "DependentUpon=${_XAML_SOURCE}")
    else()
        set_property(SOURCE "${_IDL_SOURCE}" PROPERTY VS_SETTINGS "SubType=Code")
    endif()
endforeach()

# Release PDBs for debugging.
target_compile_options(${SHARED_TARGET} PRIVATE "$<$<CONFIG:Release>:/Zi>")
target_link_options(${SHARED_TARGET} PRIVATE "$<$<CONFIG:Release>:/DEBUG>")
