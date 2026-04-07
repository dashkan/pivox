cmake_minimum_required(VERSION 3.23)

# Swift is only used on macOS.
if(APPLE)
    project(Pivox LANGUAGES C CXX Swift)
else()
    project(Pivox LANGUAGES C CXX)
endif()

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# ── Build configurations ──────────────────────────────────────────────
# DebugUITest: Debug + UITEST flag for auth bypass in UI tests.
# Keeps bypass code out of normal Debug and Release builds entirely.
if(CMAKE_GENERATOR MATCHES "Xcode")
    # Add DebugUITest to Xcode's configuration list (inherits Debug settings).
    set(CMAKE_CONFIGURATION_TYPES "Debug;Release;DebugUITest" CACHE STRING "" FORCE)
    # DebugUITest uses the same C/C++/Swift flags as Debug.
    set(CMAKE_C_FLAGS_DEBUGUITEST "${CMAKE_C_FLAGS_DEBUG}")
    set(CMAKE_CXX_FLAGS_DEBUGUITEST "${CMAKE_CXX_FLAGS_DEBUG}")
    set(CMAKE_EXE_LINKER_FLAGS_DEBUGUITEST "${CMAKE_EXE_LINKER_FLAGS_DEBUG}")
    set(CMAKE_Swift_FLAGS_DEBUGUITEST "${CMAKE_Swift_FLAGS_DEBUG}")
elseif(CMAKE_GENERATOR MATCHES "Visual Studio")
    set(CMAKE_CONFIGURATION_TYPES "Debug;Release;DebugUITest" CACHE STRING "" FORCE)
    set(CMAKE_C_FLAGS_DEBUGUITEST "${CMAKE_C_FLAGS_DEBUG}")
    set(CMAKE_CXX_FLAGS_DEBUGUITEST "${CMAKE_CXX_FLAGS_DEBUG}")
    set(CMAKE_EXE_LINKER_FLAGS_DEBUGUITEST "${CMAKE_EXE_LINKER_FLAGS_DEBUG}")
endif()

# Warnings as errors — compiler and linker.
if(MSVC)
    set(CMAKE_C_FLAGS "${CMAKE_C_FLAGS} /W4 /WX /wd4100")
    set(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS} /W4 /WX /wd4100")
    # /WX for linker. Suppressed linker warnings:
    #   4099 — missing PDBs in Firebase C++ SDK pre-built libs
    #   4075 — /INCREMENTAL vs /OPT:ICF conflict (NuGet targets inject /OPT in Debug)
    # See docs/dev/windows-platform-structure.md "Suppressed warnings".
    add_link_options(/WX /ignore:4099 /ignore:4075 /ignore:4078)
else()
    set(CMAKE_C_FLAGS "${CMAKE_C_FLAGS} -Wall -Wextra -Werror -Wno-unused-parameter")
    set(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS} -Wall -Wextra -Werror -Wno-unused-parameter")
endif()

# Swift: treat warnings as errors via Xcode build setting.
if(CMAKE_GENERATOR MATCHES "Xcode")
    set(CMAKE_XCODE_ATTRIBUTE_SWIFT_TREAT_WARNINGS_AS_ERRORS "YES")
    # DEBUG for Swift in Debug/DebugUITest builds.
    # UITEST only in DebugUITest — gates SKIP_AUTH and test-only bypass code.
    set(CMAKE_XCODE_ATTRIBUTE_SWIFT_ACTIVE_COMPILATION_CONDITIONS[variant=Debug] "DEBUG")
    set(CMAKE_XCODE_ATTRIBUTE_SWIFT_ACTIVE_COMPILATION_CONDITIONS[variant=DebugUITest] "DEBUG UITEST")
endif()


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
    add_subdirectory(platform/macos)

    set(FIREBASE_PLIST "${CMAKE_SOURCE_DIR}/configs/firebase/GoogleService-Info.plist")

    # Swift executable (SwiftUI app).
    set(SWIFT_DIR "platform/macos/swift")
    add_executable(${APP_TARGET} MACOSX_BUNDLE
        ${SWIFT_DIR}/App/main.swift
        ${SWIFT_DIR}/App/AppDelegate.swift
        ${SWIFT_DIR}/App/ContentView.swift
        ${SWIFT_DIR}/Auth/AuthService.swift
        ${SWIFT_DIR}/Auth/LoginView.swift
        ${SWIFT_DIR}/Auth/RegisterView.swift
        ${SWIFT_DIR}/Auth/ProfileView.swift
        ${SWIFT_DIR}/ImageEditor/EditView.swift
        ${SWIFT_DIR}/Components/GlassCard.swift
        ${SWIFT_DIR}/Components/GoogleIcon.swift
        ${ASSET_CATALOG}
        ${FIREBASE_PLIST}
    )

    # Asset catalog and Firebase plist go into Resources/.
    set_source_files_properties(${ASSET_CATALOG} ${FIREBASE_PLIST} PROPERTIES
        MACOSX_PACKAGE_LOCATION Resources
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
        # RecaptchaInterop.xcframework is header-only (iOS slices only) — not linked.
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
        XCODE_ATTRIBUTE_PRODUCT_BUNDLE_IDENTIFIER "app.pivox.native"
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
        tests/macos/ImageEditorBridgeTests.swift
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
        tests/macos/ui/ImageEditorUITests.swift
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
# Windows: WinUI 3 App + Shared WinRT Component Library
# ============================================================

if(WIN32)
    # Download Firebase C++ SDK pre-built desktop libraries.
    set(FIREBASE_CPP_VERSION "13.5.0")
    list(APPEND CMAKE_MODULE_PATH "${CMAKE_CURRENT_SOURCE_DIR}/cmake")
    include(DownloadFirebaseCpp)
    DownloadFirebaseCpp("${FIREBASE_CPP_VERSION}" "${CMAKE_SOURCE_DIR}/third_party/firebase-cpp")
    set(FB_CPP_ROOT "${CMAKE_SOURCE_DIR}/third_party/firebase-cpp/firebase_cpp_sdk")

    set(WIN_PLATFORM_DIR "${CMAKE_SOURCE_DIR}/platform/windows")

    # Copy Directory.Build.props/targets into build dir (constraint #1).
    configure_file(
        "${WIN_PLATFORM_DIR}/Directory.Build.props"
        "${CMAKE_BINARY_DIR}/Directory.Build.props" COPYONLY)
    configure_file(
        "${WIN_PLATFORM_DIR}/Directory.Build.targets"
        "${CMAKE_BINARY_DIR}/Directory.Build.targets" COPYONLY)

    # WinAppState library — shared between app, shared library, and tests.
    add_library(pivox_win_state STATIC
        ${WIN_PLATFORM_DIR}/WinAppState.cpp
        ${WIN_PLATFORM_DIR}/WinAuthService.cpp
    )
    target_include_directories(pivox_win_state PUBLIC
        ${WIN_PLATFORM_DIR}
        "${CMAKE_SOURCE_DIR}/core"
    )
    target_link_libraries(pivox_win_state PUBLIC pivox_core)
    target_link_libraries(pivox_win_state PRIVATE crypt32 advapi32)

    # Firebase C++ SDK — always linked on Windows.
    # Architecture-aware: x64 for app, x86 for ActiveX control.
    if(CMAKE_GENERATOR_PLATFORM STREQUAL "Win32")
        set(FB_ARCH "x86")
    else()
        set(FB_ARCH "x64")
    endif()
    target_include_directories(pivox_win_state PUBLIC "${FB_CPP_ROOT}/include")
    target_link_directories(pivox_win_state PUBLIC
        "${FB_CPP_ROOT}/libs/windows/VS2019/MD/${FB_ARCH}/$<CONFIG>")
    target_link_libraries(pivox_win_state PRIVATE firebase_app firebase_auth
        ws2_32 userenv)
    target_compile_definitions(pivox_win_state PUBLIC _CRT_SECURE_NO_WARNINGS)

    # Shared WinRT component library (DLL + .winmd).
    # Must be defined before the app and ActiveX targets — they link against it.
    add_subdirectory(platform/windows/shared)

    # WinUI 3 app executable.
    add_subdirectory(platform/windows/app)

    # ActiveX control (ATL-based, XAML Islands).
    # x86 only — typically built in a separate CMake configure (-A Win32).
    # Included here so it appears in the solution for x64 builds too;
    # the ActiveX target simply won't link on non-x86 architectures.
    add_subdirectory(platform/windows/activex)
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
        tests/core/crop_math_tests.cpp
        tests/core/image_editor_engine_tests.cpp
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
