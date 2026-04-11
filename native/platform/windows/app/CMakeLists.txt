# ============================================================
# Pivox — WinUI 3 application executable
# ============================================================

set(WIN_TARGET "PivoxApp")
set(WIN_APP_DIR "${CMAKE_CURRENT_SOURCE_DIR}")

add_executable(${WIN_TARGET} WIN32
    # App
    ${WIN_APP_DIR}/App.idl
    ${WIN_APP_DIR}/App.xaml
    ${WIN_APP_DIR}/App.xaml.h
    ${WIN_APP_DIR}/App.xaml.cpp
    # MainWindow
    ${WIN_APP_DIR}/MainWindow.idl
    ${WIN_APP_DIR}/MainWindow.xaml
    ${WIN_APP_DIR}/MainWindow.xaml.h
    ${WIN_APP_DIR}/MainWindow.xaml.cpp
)

target_include_directories(${WIN_TARGET} PRIVATE ${WIN_APP_DIR})
target_link_libraries(${WIN_TARGET} PRIVATE PivoxShared pivox_win_state crypt32 advapi32)
target_precompile_headers(${WIN_TARGET} PRIVATE ${WIN_APP_DIR}/pch.h)

# App.xaml is the ApplicationDefinition (constraint #4).
set_property(SOURCE ${WIN_APP_DIR}/App.xaml
    PROPERTY VS_XAML_TYPE "ApplicationDefinition")

# WinUI 3 target properties (constraint #9).
set_target_properties(${WIN_TARGET} PROPERTIES
    OUTPUT_NAME "Pivox"
    VS_GLOBAL_RootNamespace "PivoxApp"
    VS_GLOBAL_AppContainerApplication "false"
    VS_GLOBAL_AppxPackage "false"
    VS_GLOBAL_CppWinRTOptimized "true"
    VS_GLOBAL_CppWinRTRootNamespaceAutoMerge "true"
    VS_GLOBAL_UseWinUI "true"
    VS_GLOBAL_ApplicationType "Windows Store"
    VS_GLOBAL_ApplicationTypeRevision "10.0"
    VS_GLOBAL_WindowsTargetPlatformVersion "10.0"
    VS_GLOBAL_WindowsTargetPlatformMinVersion "10.0.17763.0"
    VS_GLOBAL_WindowsPackageType "None"
    VS_GLOBAL_EnablePreviewMsixTooling "true"
    VS_GLOBAL_WindowsAppSDKSelfContained "true"
)

# NuGet packages (constraint #8).
set_property(TARGET ${WIN_TARGET} PROPERTY VS_PACKAGE_REFERENCES
    "Microsoft.Windows.CppWinRT_2.0.250303.1"
    "Microsoft.WindowsAppSDK_1.8.260317003"
    "Microsoft.Windows.SDK.BuildTools_10.0.26100.7705"
)

# IDL/XAML dependency wiring (constraint #5).
get_target_property(_SOURCES ${WIN_TARGET} SOURCES)
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

# Release PDBs for debugging (constraint #10).
target_compile_options(${WIN_TARGET} PRIVATE "$<$<CONFIG:Release>:/Zi>")
target_link_options(${WIN_TARGET} PRIVATE "$<$<CONFIG:Release>:/DEBUG>")
