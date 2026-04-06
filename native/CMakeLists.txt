cmake_minimum_required(VERSION 3.23)

# Swift is only used on macOS.
if(APPLE)
    project(Pivox LANGUAGES C CXX Swift)
else()
    project(Pivox LANGUAGES C CXX)
endif()

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)


# ============================================================
# Shared C++ Core
# ============================================================

add_subdirectory(core)


# ============================================================
# Firebase Apple SDK (xcframeworks, macOS + iOS)
# ============================================================

if(APPLE)
    set(FIREBASE_VERSION "12.11.0")
    list(APPEND CMAKE_MODULE_PATH "${CMAKE_CURRENT_SOURCE_DIR}/cmake")
    include(DownloadFirebase)
    DownloadFirebase("${FIREBASE_VERSION}" "${CMAKE_SOURCE_DIR}/third_party/firebase")
endif()


# ============================================================
# macOS: SwiftUI App
# ============================================================

if(APPLE)
    # macOS deployment target — set before any targets.
    set(CMAKE_OSX_DEPLOYMENT_TARGET "15.0" CACHE STRING "Minimum macOS version")

    set(APP_TARGET "Pivox")
    set(EXECUTABLE_NAME "${APP_TARGET}")
    set(PRODUCT_NAME "${APP_TARGET}")

    # Asset catalog — compiled automatically by Xcode generator.
    set(ASSET_CATALOG "${CMAKE_SOURCE_DIR}/platform/macos/Assets.xcassets")

    # Firebase xcframeworks — link to the app and bridge.
    set(FB_ROOT "${CMAKE_SOURCE_DIR}/third_party/firebase/Firebase")
    set(FB_ANALYTICS "${FB_ROOT}/FirebaseAnalytics")
    set(FB_AUTH "${FB_ROOT}/FirebaseAuth")
    set(FB_GOOGLESIGNIN "${FB_ROOT}/GoogleSignIn")

    # Obj-C++ bridge library (wraps C++ core for Swift consumption).
    add_library(pivox_bridge STATIC
        platform/macos/objcpp/MacAppState.mm
        platform/macos/objcpp/AppStateBridge.mm
    )
    target_link_libraries(pivox_bridge PUBLIC pivox_core)
    target_include_directories(pivox_bridge PUBLIC
        "${CMAKE_SOURCE_DIR}/core"
        "${CMAKE_SOURCE_DIR}/platform/macos/objcpp"
    )
    target_link_libraries(pivox_bridge PRIVATE "-framework Security")

    # Swift executable (SwiftUI app).
    add_executable(${APP_TARGET} MACOSX_BUNDLE
        platform/macos/swift/main.swift
        platform/macos/swift/AppDelegate.swift
        platform/macos/swift/ContentView.swift
        platform/macos/swift/LoginView.swift
        platform/macos/swift/RegisterView.swift
        platform/macos/swift/ProfileView.swift
        platform/macos/swift/GlassCard.swift
        platform/macos/swift/GoogleIcon.swift
        platform/macos/swift/AuthService.swift
        ${ASSET_CATALOG}
    )

    # Asset catalog goes into Resources/.
    set_source_files_properties(${ASSET_CATALOG} PROPERTIES
        MACOSX_PACKAGE_LOCATION Resources
    )

    # Copy Firebase config into app bundle Resources/.
    set_target_properties(${APP_TARGET} PROPERTIES
        RESOURCE "${CMAKE_SOURCE_DIR}/configs/firebase/GoogleService-Info.plist"
    )

    target_link_libraries(${APP_TARGET} PRIVATE pivox_bridge
        # Firebase Core (from Analytics bundle)
        "${FB_ANALYTICS}/FirebaseCore.xcframework"
        "${FB_ANALYTICS}/FirebaseCoreInternal.xcframework"
        "${FB_ANALYTICS}/FirebaseInstallations.xcframework"
        "${FB_ANALYTICS}/FBLPromises.xcframework"
        "${FB_ANALYTICS}/GoogleUtilities.xcframework"
        "${FB_ANALYTICS}/nanopb.xcframework"
        # Firebase Auth
        "${FB_AUTH}/FirebaseAuth.xcframework"
        "${FB_AUTH}/FirebaseAuthInterop.xcframework"
        "${FB_AUTH}/FirebaseAppCheckInterop.xcframework"
        "${FB_AUTH}/FirebaseCoreExtension.xcframework"
        "${FB_AUTH}/GTMSessionFetcher.xcframework"
        "${FB_AUTH}/RecaptchaInterop.xcframework"
        # Google Sign-In uses ASWebAuthenticationSession (built-in, no extra SDK).
    )
    target_link_options(${APP_TARGET} PRIVATE "LINKER:-ObjC")

    target_compile_options(${APP_TARGET} PRIVATE
        "SHELL:-import-objc-header ${CMAKE_SOURCE_DIR}/platform/macos/Bridging-Header.h"
    )
    target_include_directories(${APP_TARGET} PRIVATE
        "${CMAKE_SOURCE_DIR}/platform/macos/objcpp"
    )

    set_target_properties(${APP_TARGET} PROPERTIES
        MACOSX_BUNDLE_INFO_PLIST ${CMAKE_SOURCE_DIR}/platform/macos/Info.plist.in
        MACOSX_BUNDLE_GUI_IDENTIFIER "app.pivox.native"
        MACOSX_BUNDLE_BUNDLE_VERSION "0.1.0"
        MACOSX_BUNDLE_SHORT_VERSION_STRING "0.1.0"
        MACOSX_BUNDLE_BUNDLE_NAME "Pivox"
        CMAKE_Swift_LANGUAGE_VERSION 5
        XCODE_ATTRIBUTE_ASSETCATALOG_COMPILER_APPICON_NAME "AppIcon"
        XCODE_ATTRIBUTE_CODE_SIGN_ENTITLEMENTS "${CMAKE_SOURCE_DIR}/platform/macos/Pivox.entitlements"
        XCODE_ATTRIBUTE_CODE_SIGN_IDENTITY "Apple Development"
        XCODE_ATTRIBUTE_DEVELOPMENT_TEAM "FAENDBN66M"
        XCODE_ATTRIBUTE_CODE_SIGN_STYLE "Automatic"
    )

    # For Ninja builds: compile asset catalog manually via actool.
    if(NOT CMAKE_GENERATOR MATCHES "Xcode")
        add_custom_command(
            TARGET ${APP_TARGET} POST_BUILD
            COMMAND xcrun actool
                --compile "${CMAKE_BINARY_DIR}/Pivox.app/Contents/Resources"
                --platform macosx
                --minimum-deployment-target "15.0"
                "${ASSET_CATALOG}"
            COMMENT "Compiling asset catalog..."
            VERBATIM
        )
    endif()

    # ----------------------------------------------------------
    # XCTest unit tests
    # ----------------------------------------------------------
    enable_testing()
    find_package(XCTest REQUIRED)

    xctest_add_bundle(PivoxTests ${APP_TARGET}
        tests/macos/CoreTests.swift
        tests/macos/AuthViewModelTests.swift
        tests/macos/AuthServiceTests.swift
    )

    target_compile_options(PivoxTests PRIVATE
        "SHELL:-import-objc-header ${CMAKE_SOURCE_DIR}/platform/macos/Bridging-Header.h"
    )
    target_include_directories(PivoxTests PRIVATE "${CMAKE_SOURCE_DIR}/platform/macos/objcpp")
    target_link_libraries(PivoxTests PRIVATE pivox_bridge)

    xctest_add_test(PivoxUnitTests PivoxTests)

    # ----------------------------------------------------------
    # XCUITest UI tests
    # ----------------------------------------------------------
    xctest_add_bundle(PivoxUITests ${APP_TARGET}
        tests/macos/ui/SidebarUITests.swift
        tests/macos/ui/AuthUITests.swift
    )

    target_compile_options(PivoxUITests PRIVATE
        "SHELL:-import-objc-header ${CMAKE_SOURCE_DIR}/platform/macos/Bridging-Header.h"
    )
    target_include_directories(PivoxUITests PRIVATE "${CMAKE_SOURCE_DIR}/platform/macos/objcpp")

    # Mark as UI test bundle — XCUITest launches the app itself.
    set_target_properties(PivoxUITests PROPERTIES
        XCODE_ATTRIBUTE_TEST_TARGET_NAME "${APP_TARGET}"
        XCODE_ATTRIBUTE_TEST_HOST ""
        XCODE_PRODUCT_TYPE "com.apple.product-type.bundle.ui-testing"
    )

    xctest_add_test(PivoxUI PivoxUITests)
endif()


# ============================================================
# Windows: WinUI 3 App (added by Windows Claude via SSH)
# ============================================================

if(WIN32)
    # Download Firebase C++ SDK pre-built desktop libraries.
    set(FIREBASE_CPP_VERSION "13.5.0")
    list(APPEND CMAKE_MODULE_PATH "${CMAKE_CURRENT_SOURCE_DIR}/cmake")
    include(DownloadFirebaseCpp)
    DownloadFirebaseCpp("${FIREBASE_CPP_VERSION}" "${CMAKE_SOURCE_DIR}/third_party/firebase-cpp")
    set(FB_CPP_ROOT "${CMAKE_SOURCE_DIR}/third_party/firebase-cpp/firebase_cpp_sdk")

    set(WIN_TARGET "Pivox")
    set(WIN_PLATFORM_DIR "${CMAKE_SOURCE_DIR}/platform/windows")

    # Copy Directory.Build.props/targets into build dir (constraint #1).
    configure_file(
        "${WIN_PLATFORM_DIR}/Directory.Build.props"
        "${CMAKE_BINARY_DIR}/Directory.Build.props" COPYONLY)
    configure_file(
        "${WIN_PLATFORM_DIR}/Directory.Build.targets"
        "${CMAKE_BINARY_DIR}/Directory.Build.targets" COPYONLY)

    add_executable(${WIN_TARGET} WIN32
        # App
        ${WIN_PLATFORM_DIR}/App.idl
        ${WIN_PLATFORM_DIR}/App.xaml
        ${WIN_PLATFORM_DIR}/App.xaml.h
        ${WIN_PLATFORM_DIR}/App.xaml.cpp
        # MainWindow
        ${WIN_PLATFORM_DIR}/MainWindow.idl
        ${WIN_PLATFORM_DIR}/MainWindow.xaml
        ${WIN_PLATFORM_DIR}/MainWindow.xaml.h
        ${WIN_PLATFORM_DIR}/MainWindow.xaml.cpp
        # LoginPage
        ${WIN_PLATFORM_DIR}/LoginPage.idl
        ${WIN_PLATFORM_DIR}/LoginPage.xaml
        ${WIN_PLATFORM_DIR}/LoginPage.xaml.h
        ${WIN_PLATFORM_DIR}/LoginPage.xaml.cpp
        # RegisterPage
        ${WIN_PLATFORM_DIR}/RegisterPage.idl
        ${WIN_PLATFORM_DIR}/RegisterPage.xaml
        ${WIN_PLATFORM_DIR}/RegisterPage.xaml.h
        ${WIN_PLATFORM_DIR}/RegisterPage.xaml.cpp
    )

    target_include_directories(${WIN_TARGET} PRIVATE ${WIN_PLATFORM_DIR})
    target_link_libraries(${WIN_TARGET} PRIVATE pivox_win_state crypt32 advapi32)
    target_precompile_headers(${WIN_TARGET} PRIVATE ${WIN_PLATFORM_DIR}/pch.h)

    # App.xaml is the ApplicationDefinition (constraint #4).
    set_property(SOURCE ${WIN_PLATFORM_DIR}/App.xaml
        PROPERTY VS_XAML_TYPE "ApplicationDefinition")

    # WinUI 3 target properties (constraint #9).
    set_target_properties(${WIN_TARGET} PROPERTIES
        VS_GLOBAL_RootNamespace "Pivox"
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

    # WinAppState library — shared between app and tests.
    add_library(pivox_win_state STATIC
        ${WIN_PLATFORM_DIR}/WinAppState.cpp
        ${WIN_PLATFORM_DIR}/WinAuthService.cpp
    )
    target_include_directories(pivox_win_state PUBLIC
        ${WIN_PLATFORM_DIR}
        "${CMAKE_SOURCE_DIR}/core"
    )
    target_link_libraries(pivox_win_state PUBLIC pivox_core)
    target_link_libraries(pivox_win_state PRIVATE crypt32 advapi32 winhttp)

    # Firebase C++ SDK — link auth + app libraries.
    if(EXISTS "${FB_CPP_ROOT}")
        target_include_directories(pivox_win_state PUBLIC "${FB_CPP_ROOT}/include")
        target_link_directories(pivox_win_state PUBLIC
            "${FB_CPP_ROOT}/libs/windows/VS2019/MD/x64/$<CONFIG>")
        target_link_libraries(pivox_win_state PRIVATE firebase_app firebase_auth
            ws2_32 userenv)
        target_compile_definitions(pivox_win_state PUBLIC
            PIVOX_HAS_FIREBASE=1 _CRT_SECURE_NO_WARNINGS)
    endif()
endif()


# ============================================================
# Shared C++ Tests (Google Test via vcpkg)
# ============================================================

find_package(GTest CONFIG)
if(GTest_FOUND)
    enable_testing()

    add_executable(pivox_core_tests
        tests/core/core_tests.cpp
        tests/core/auth_validation_tests.cpp
        tests/core/app_state_tests.cpp
        tests/core/auth_state_tests.cpp
    )
    target_link_libraries(pivox_core_tests PRIVATE pivox_core GTest::gtest_main)

    include(GoogleTest)
    gtest_discover_tests(pivox_core_tests DISCOVERY_MODE PRE_TEST)

    # Windows-specific integration tests (WinAppState with real Registry/PasswordVault).
    if(WIN32)
        add_executable(pivox_win_tests
            tests/windows/win_app_state_tests.cpp
            tests/windows/win_auth_service_tests.cpp
        )
        target_link_libraries(pivox_win_tests PRIVATE pivox_win_state GTest::gtest_main)
        gtest_discover_tests(pivox_win_tests DISCOVERY_MODE PRE_TEST)
    endif()
endif()
