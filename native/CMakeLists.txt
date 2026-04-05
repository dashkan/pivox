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
# macOS: SwiftUI App
# ============================================================

if(APPLE)
    # macOS deployment target — set before any targets.
    set(CMAKE_OSX_DEPLOYMENT_TARGET "14.0" CACHE STRING "Minimum macOS version")

    set(APP_TARGET "Pivox")
    set(EXECUTABLE_NAME "${APP_TARGET}")
    set(PRODUCT_NAME "${APP_TARGET}")

    # Asset catalog — compiled automatically by Xcode generator.
    set(ASSET_CATALOG "${CMAKE_SOURCE_DIR}/platform/macos/Assets.xcassets")

    add_executable(${APP_TARGET} MACOSX_BUNDLE
        platform/macos/swift/main.swift
        platform/macos/swift/AppDelegate.swift
        platform/macos/swift/ContentView.swift
        platform/macos/swift/LoginView.swift
        platform/macos/swift/RegisterView.swift
        platform/macos/swift/ProfileView.swift
        platform/macos/swift/GlassCard.swift
        platform/macos/swift/GoogleIcon.swift
        ${ASSET_CATALOG}
    )

    # Asset catalog goes into Resources/.
    set_source_files_properties(${ASSET_CATALOG} PROPERTIES
        MACOSX_PACKAGE_LOCATION Resources
    )

    target_link_libraries(${APP_TARGET} PRIVATE pivox_core)

    target_compile_options(${APP_TARGET} PRIVATE
        "SHELL:-import-objc-header ${CMAKE_SOURCE_DIR}/platform/macos/Bridging-Header.h"
    )

    set_target_properties(${APP_TARGET} PROPERTIES
        MACOSX_BUNDLE_INFO_PLIST ${CMAKE_SOURCE_DIR}/platform/macos/Info.plist.in
        MACOSX_BUNDLE_GUI_IDENTIFIER "app.pivox.desktop"
        MACOSX_BUNDLE_BUNDLE_VERSION "0.1.0"
        MACOSX_BUNDLE_SHORT_VERSION_STRING "0.1.0"
        MACOSX_BUNDLE_BUNDLE_NAME "Pivox"
        CMAKE_Swift_LANGUAGE_VERSION 5
        XCODE_ATTRIBUTE_ASSETCATALOG_COMPILER_APPICON_NAME "AppIcon"
    )

    # For Ninja builds: compile asset catalog manually via actool.
    if(NOT CMAKE_GENERATOR MATCHES "Xcode")
        add_custom_command(
            TARGET ${APP_TARGET} POST_BUILD
            COMMAND xcrun actool
                --compile "${CMAKE_BINARY_DIR}/Pivox.app/Contents/Resources"
                --platform macosx
                --minimum-deployment-target "14.0"
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
    )

    target_compile_options(PivoxTests PRIVATE
        "SHELL:-import-objc-header ${CMAKE_SOURCE_DIR}/platform/macos/Bridging-Header.h"
    )

    xctest_add_test(PivoxUnitTests PivoxTests)
endif()


# ============================================================
# Windows: WinUI 3 App (added by Windows Claude via SSH)
# ============================================================

if(WIN32)
    # TODO: Windows targets will be added here by the Windows Claude agent.
    # See docs/dev/cross-platform-workflow.md for the SSH workflow.
    message(STATUS "Windows targets not yet configured — run the Windows build agent.")
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
    )
    target_link_libraries(pivox_core_tests PRIVATE pivox_core GTest::gtest_main)

    include(GoogleTest)
    gtest_discover_tests(pivox_core_tests DISCOVERY_MODE PRE_TEST)
endif()
