# ============================================================
# PivoxActiveX — ATL-based ActiveX user control
# Hosts WinUI 3 content from PivoxShared via XAML Islands.
# x86 only. Built in a separate CMake configure (-A Win32).
# ============================================================

set(AX_TARGET "PivoxActiveX")
set(AX_DIR "${CMAKE_CURRENT_SOURCE_DIR}")

# PivoxControl.idl is COM IDL. Process it with native MIDL to avoid
# CppWinRT/WindowsAppSDK injecting WinRT references that conflict
# with COM base types (IUnknown, _FLAGGED_WORD_BLOB redefinitions).
set(AX_MIDL_OUT "${CMAKE_CURRENT_BINARY_DIR}/midl_out")
file(MAKE_DIRECTORY ${AX_MIDL_OUT})

add_custom_command(
    OUTPUT
        ${AX_MIDL_OUT}/PivoxActiveX_i.h
        ${AX_MIDL_OUT}/PivoxActiveX_i.c
        ${AX_MIDL_OUT}/PivoxActiveX.tlb
    COMMAND midl
        /W1 /nologo
        /char signed
        /env win32
        /h "${AX_MIDL_OUT}/PivoxActiveX_i.h"
        /iid "${AX_MIDL_OUT}/PivoxActiveX_i.c"
        /tlb "${AX_MIDL_OUT}/PivoxActiveX.tlb"
        /proxy nul /dlldata nul
        "${AX_DIR}/PivoxControl.idl"
    DEPENDS "${AX_DIR}/PivoxControl.idl"
    COMMENT "Processing COM IDL: PivoxControl.idl"
    VERBATIM
)

add_library(${AX_TARGET} SHARED
    # ATL module + DLL exports
    ${AX_DIR}/PivoxActiveX.cpp
    ${AX_DIR}/dllmain.cpp
    ${AX_DIR}/dllmain.h
    ${AX_DIR}/framework.h
    ${AX_DIR}/targetver.h
    ${AX_DIR}/pch.cpp
    ${AX_DIR}/pch.h

    # ActiveX control
    ${AX_DIR}/PivoxControl.cpp
    ${AX_DIR}/PivoxControl.h
    ${AX_DIR}/_IPivoxControlEvents_CP.h

    # XAML Islands + drag
    ${AX_DIR}/XamlIslandHost.cpp
    ${AX_DIR}/XamlIslandHost.h
    ${AX_DIR}/DragSource.cpp
    ${AX_DIR}/DragSource.h

    # Resources
    ${AX_DIR}/PivoxActiveX.rc
    ${AX_DIR}/resource.h
    ${AX_DIR}/PivoxControl.def
    ${AX_DIR}/PivoxActiveX.rgs
    ${AX_DIR}/PivoxControl.rgs

    # MIDL-generated files
    ${AX_MIDL_OUT}/PivoxActiveX_i.h
    ${AX_MIDL_OUT}/PivoxActiveX_i.c
)

# _i.c is plain C — skip the C++ PCH.
set_source_files_properties(${AX_MIDL_OUT}/PivoxActiveX_i.c PROPERTIES
    LANGUAGE CXX
    SKIP_PRECOMPILE_HEADERS TRUE
)

target_include_directories(${AX_TARGET} PRIVATE
    ${AX_DIR}
    ${AX_MIDL_OUT}   # PivoxActiveX_i.h
)

# Resource compiler needs the MIDL output directory to find PivoxActiveX.tlb.
set_source_files_properties(${AX_DIR}/PivoxActiveX.rc PROPERTIES
    VS_SETTINGS "AdditionalIncludeDirectories=${AX_MIDL_OUT}"
)

# Bootstrap lib for MddBootstrapInitialize (XAML Islands in unpackaged apps).
set(WASDK_FOUNDATION "Microsoft.WindowsAppSDK.Foundation")
set(WASDK_FOUNDATION_VER "1.8.260222000")
set(WASDK_BOOTSTRAP_LIB
    "$ENV{USERPROFILE}/.nuget/packages/${WASDK_FOUNDATION}/${WASDK_FOUNDATION_VER}/lib/native/${FB_ARCH}/Microsoft.WindowsAppRuntime.Bootstrap.lib")
target_link_libraries(${AX_TARGET} PRIVATE PivoxShared pivox_win_state crypt32 advapi32
    "${WASDK_BOOTSTRAP_LIB}")
target_precompile_headers(${AX_TARGET} PRIVATE ${AX_DIR}/pch.h)

# Use .def file for DLL exports. Embed manifest for XAML Islands support.
set_target_properties(${AX_TARGET} PROPERTIES
    LINK_FLAGS "/DEF:\"${AX_DIR}/PivoxControl.def\" /MANIFEST:EMBED /MANIFESTINPUT:\"${AX_DIR}/PivoxActiveX.manifest\""
)

# CppWinRT provides WinRT projection headers (winrt/Microsoft.UI.Xaml.h etc.)
# but must not process our COM IDL — that's handled by the custom MIDL above.
set_target_properties(${AX_TARGET} PROPERTIES
    VS_GLOBAL_RootNamespace "PivoxActiveX"
    VS_GLOBAL_CppWinRTOptimized "true"
    VS_GLOBAL_WindowsTargetPlatformVersion "10.0"
    VS_GLOBAL_WindowsTargetPlatformMinVersion "10.0.17763.0"
)

# NuGet packages — CppWinRT for WinRT headers, WindowsAppSDK for XAML Islands.
set_property(TARGET ${AX_TARGET} PROPERTY VS_PACKAGE_REFERENCES
    "Microsoft.Windows.CppWinRT_2.0.250303.1"
    "Microsoft.WindowsAppSDK_1.8.260317003"
    "Microsoft.Windows.SDK.BuildTools_10.0.26100.7705"
)

# Release PDBs for debugging.
target_compile_options(${AX_TARGET} PRIVATE "$<$<CONFIG:Release>:/Zi>")
target_link_options(${AX_TARGET} PRIVATE "$<$<CONFIG:Release>:/DEBUG>")

# PivoxShared.dll is auto-copied to the ActiveX output directory by MSBuild
# via Private=true in Directory.Build.targets.

# ActiveX control tests.
add_subdirectory(tests)
