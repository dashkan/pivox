cmake_minimum_required(VERSION 4.0)

# ── vcpkg overlay ports ─────────────────────────────────────────────
# We ship a local vcpkg port overlay at `native/vcpkg-ports/` for packages
# that need patches we don't want to monkey-patch at the CMake layer.
#
# Currently overlaid:
#   abseil — absl/time emits `-Wl,-framework,CoreFoundation` in its
#            INTERFACE_LINK_LIBRARIES. The clang driver parses `-Wl,X,Y,Z`;
#            swiftc does not, so any Swift target that transitively
#            consumes absl::time (e.g. via gRPC) fails to link under the
#            Ninja generator. Our patch applies absl's OWN planned fix
#            early — switching to the CMake 3.24+ neutral form
#            `$<LINK_LIBRARY:FRAMEWORK,CoreFoundation>`, which both driver
#            parsers accept.
#
#            Upstream TODO (will let us delete our overlay once merged):
#            https://github.com/abseil/abseil-cpp/blob/master/absl/time/CMakeLists.txt
#            grep for TODO(#1495)
#
# When upstream lands the fix AND vcpkg picks it up, delete
# `native/vcpkg-ports/abseil/` and remove this comment block.
#
# Consumers: pass `-DVCPKG_OVERLAY_PORTS=${sourceDir}/vcpkg-ports` to cmake
# (already wired in CMakePresets.json).
# ────────────────────────────────────────────────────────────────────

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
    add_link_options(/WX /ignore:4099 /ignore:4075 /ignore:4078 /ignore:4042)
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

    # Swift executable (SwiftUI app). All .swift under platform/macos/swift
    # is picked up recursively. CONFIGURE_DEPENDS re-triggers CMake when
    # files appear/disappear.
    set(SWIFT_DIR "platform/macos/swift")
    file(GLOB_RECURSE APP_SWIFT_SOURCES
        CONFIGURE_DEPENDS
        "${CMAKE_SOURCE_DIR}/${SWIFT_DIR}/*.swift"
    )

    add_executable(${APP_TARGET} MACOSX_BUNDLE
        ${APP_SWIFT_SOURCES}
        ${ASSET_CATALOG}
        ${FIREBASE_PLIST}
    )

    # Asset catalog and Firebase plist go into Resources/.
    set_source_files_properties(${ASSET_CATALOG} ${FIREBASE_PLIST} PROPERTIES
        MACOSX_PACKAGE_LOCATION Resources
    )

    # PivoxModels — built via SwiftPM (`swift build`) so transitive Swift
    # AND C dependencies (SwiftProtobuf, grpc-swift-2, NIO, BoringSSL via
    # swift-nio-ssl) are all resolved. The package emits a single dylib
    # bundling its own code plus all transitive deps; the app links it
    # and embeds it under Contents/Frameworks/ at build time. Static
    # linking would require each transitive dep as its own .a, which
    # SwiftPM doesn't expose.
    set(PIVOX_MODELS_PKG "${CMAKE_SOURCE_DIR}/platform/macos/swift-packages/PivoxModels")
    set(PIVOX_MODELS_BUILD_DIR "${PIVOX_MODELS_PKG}/.build/arm64-apple-macosx/release")
    set(PIVOX_MODELS_MODULES_DIR "${PIVOX_MODELS_BUILD_DIR}/Modules")
    set(PIVOX_MODELS_DYLIB "${PIVOX_MODELS_BUILD_DIR}/libPivoxModels.dylib")

    # Build at configure time so the dylib + .swiftmodule files exist
    # before CMake's -I/-L paths get evaluated.
    execute_process(
        COMMAND swift build -c release
        WORKING_DIRECTORY "${PIVOX_MODELS_PKG}"
        RESULT_VARIABLE PIVOX_MODELS_BUILD_RESULT
    )
    if(NOT PIVOX_MODELS_BUILD_RESULT EQUAL 0)
        message(FATAL_ERROR "swift build of PivoxModels failed")
    endif()

    # Custom target so build-time changes to PivoxModels sources (e.g.
    # after `make proto-generate-native`) propagate via xcodebuild too.
    add_custom_target(PivoxModelsSwiftBuild
        COMMAND swift build -c release
        WORKING_DIRECTORY "${PIVOX_MODELS_PKG}"
        USES_TERMINAL
        COMMENT "Building PivoxModels (SwiftPM)"
    )

    target_link_libraries(${APP_TARGET} PRIVATE
        pivox_bridge pivox_markdown pivox_highlight_wrapper
        "${PIVOX_MODELS_DYLIB}"
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
    add_dependencies(${APP_TARGET} PivoxModelsSwiftBuild)
    target_link_options(${APP_TARGET} PRIVATE "LINKER:-ObjC")

    # SwiftPM emits a `module.modulemap` per C module under
    # .build/<arch>/release/<Module>.build/. The Swift compiler needs
    # one `-Xcc -fmodule-map-file=<path>` per modulemap so it can
    # resolve transitive `@import CNIOAtomics`-style imports embedded
    # in NIO's binary swiftmodule files. SHELL: keeps each pair on the
    # same command line as Xcode passes them through.
    # SwiftPM emits/locates module.modulemap files in two places: it
    # generates them under .build/<arch>/release/<Module>.build/ for C
    # modules whose package didn't ship one, and it consumes the
    # original modulemap from the checkout for packages that did
    # (swift-atomics, swift-nio-ssl). We need both on the swiftc
    # search path; deduplicate by module name to avoid "redefinition"
    # errors from `<X>-tool.build/` plugin variants.
    file(GLOB_RECURSE PIVOX_MODELS_C_MODULEMAPS_RAW
        "${PIVOX_MODELS_PKG}/.build/arm64-apple-macosx/release/*.build/module.modulemap"
        "${PIVOX_MODELS_PKG}/.build/checkouts/*/Sources/*/include/module.modulemap"
    )
    set(PIVOX_MODELS_MODULEMAP_FLAGS "")
    set(_seen_modules "")
    foreach(mm ${PIVOX_MODELS_C_MODULEMAPS_RAW})
        # Extract the module name from the modulemap's first `module X` line.
        file(STRINGS "${mm}" _first_module_line REGEX "^module +[A-Za-z_][A-Za-z0-9_]*" LIMIT_COUNT 1)
        if(_first_module_line MATCHES "^module +([A-Za-z_][A-Za-z0-9_]*)")
            set(_module_name "${CMAKE_MATCH_1}")
            if(NOT _module_name IN_LIST _seen_modules)
                list(APPEND _seen_modules "${_module_name}")
                list(APPEND PIVOX_MODELS_MODULEMAP_FLAGS
                    "SHELL:$<$<COMPILE_LANGUAGE:Swift>:-Xcc -fmodule-map-file=${mm}>"
                )
                # Also add the modulemap's parent dir as -Xcc -I so the
                # umbrella header references resolve.
                get_filename_component(_mm_dir "${mm}" DIRECTORY)
                list(APPEND PIVOX_MODELS_MODULEMAP_FLAGS
                    "SHELL:$<$<COMPILE_LANGUAGE:Swift>:-Xcc -I${_mm_dir}>"
                )
            endif()
        endif()
    endforeach()

    target_compile_options(${APP_TARGET} PRIVATE
        "SHELL:-import-objc-header ${CMAKE_SOURCE_DIR}/platform/macos/Bridging-Header.h"
        # `-I` so `import PivoxModels` (and any other modules SwiftPM
        # built — SwiftProtobuf, GRPCCore, etc.) resolves against
        # SwiftPM's emitted .swiftmodule files.
        "$<$<COMPILE_LANGUAGE:Swift>:-I${PIVOX_MODELS_MODULES_DIR}>"
        ${PIVOX_MODELS_MODULEMAP_FLAGS}
    )
    target_include_directories(${APP_TARGET} PRIVATE
        "${CMAKE_SOURCE_DIR}/platform/macos/objcpp"
        "${CMAKE_SOURCE_DIR}/core/ai_elements/highlight/include"
    )

    # Embed libPivoxModels.dylib alongside libpivox_highlight.dylib in
    # Contents/Frameworks/. The post-build step below already handles
    # Rust highlight; this adds the SwiftPM-built dylib to the same
    # destination + sign step.

    set_target_properties(${APP_TARGET} PROPERTIES
        MACOSX_BUNDLE_INFO_PLIST ${CMAKE_SOURCE_DIR}/platform/macos/Info.plist.in
        MACOSX_BUNDLE_GUI_IDENTIFIER "app.pivox.native"
        MACOSX_BUNDLE_BUNDLE_VERSION "0.1.0"
        MACOSX_BUNDLE_SHORT_VERSION_STRING "0.1.0"
        MACOSX_BUNDLE_BUNDLE_NAME "Pivox"
        CMAKE_Swift_LANGUAGE_VERSION 6
        # Required so XCTest bundles can `@testable import Pivox` for
        # unit tests that reach into the Swift module's internal types.
        XCODE_ATTRIBUTE_ENABLE_TESTABILITY "YES"
        XCODE_ATTRIBUTE_ASSETCATALOG_COMPILER_APPICON_NAME "AppIcon"
        # Pivox brand accent (violet). Applies only when the user's
        # macOS Accent color is set to "Multicolor"; any specific user
        # accent choice (Blue, Pink, etc.) always wins per HIG.
        XCODE_ATTRIBUTE_ASSETCATALOG_COMPILER_GLOBAL_ACCENT_COLOR_NAME "AccentColor"
        XCODE_ATTRIBUTE_CODE_SIGN_ENTITLEMENTS "${CMAKE_SOURCE_DIR}/platform/macos/Pivox.entitlements"
        XCODE_ATTRIBUTE_CODE_SIGN_IDENTITY "Apple Development"
        XCODE_ATTRIBUTE_DEVELOPMENT_TEAM "FAENDBN66M"
        XCODE_ATTRIBUTE_CODE_SIGN_STYLE "Automatic"
        XCODE_ATTRIBUTE_PRODUCT_BUNDLE_IDENTIFIER "app.pivox.native"
        # Runtime search path for embedded dylibs. The Rust highlight
        # crate ships as a cdylib (see core/ai_elements/highlight/);
        # its LC_ID_DYLIB is @rpath-tagged at compile time, and dyld
        # resolves @rpath via this search path at launch. Required to
        # dodge the 3-personality compact-unwind ceiling by moving
        # Rust's _rust_eh_personality out of the main executable image.
        XCODE_ATTRIBUTE_LD_RUNPATH_SEARCH_PATHS "@executable_path/../Frameworks"
    )

    # Embed the Rust highlight dylib + the SwiftPM-built PivoxModels
    # dylib into Contents/Frameworks/. Both already carry the right
    # install_name (`@rpath/lib<name>.dylib`) at link time — SwiftPM
    # sets it for libPivoxModels.dylib, Corrosion sets it for
    # libpivox_highlight.dylib — so no install_name_tool rewrite is
    # needed. Sign each copied dylib; do NOT re-sign the main app
    # executable here (Xcode's own codesign step at the end of the
    # build target handles the bundle signature correctly, including
    # nested xctest bundles in Contents/PlugIns/).
    set(PIVOX_HIGHLIGHT_DYLIB_SRC
        "${CMAKE_BINARY_DIR}/core/ai_elements/highlight/libpivox_highlight.dylib")
    set(PIVOX_APP_FRAMEWORKS_DIR "$<TARGET_BUNDLE_CONTENT_DIR:${APP_TARGET}>/Frameworks")
    add_custom_command(TARGET ${APP_TARGET} POST_BUILD
        COMMAND ${CMAKE_COMMAND} -E make_directory "${PIVOX_APP_FRAMEWORKS_DIR}"
        COMMAND ${CMAKE_COMMAND} -E copy_if_different
            "${PIVOX_HIGHLIGHT_DYLIB_SRC}"
            "${PIVOX_APP_FRAMEWORKS_DIR}/"
        COMMAND ${CMAKE_COMMAND} -E copy_if_different
            "${PIVOX_MODELS_DYLIB}"
            "${PIVOX_APP_FRAMEWORKS_DIR}/"
        COMMAND codesign --force --sign -
            "${PIVOX_APP_FRAMEWORKS_DIR}/libpivox_highlight.dylib"
        COMMAND codesign --force --sign -
            "${PIVOX_APP_FRAMEWORKS_DIR}/libPivoxModels.dylib"
        COMMENT "Embedding + signing Rust highlight + PivoxModels dylibs"
        VERBATIM
    )
    add_dependencies(${APP_TARGET} cargo-build_pivox_highlight)


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
        tests/macos/AuthServiceTests.swift
        tests/macos/AuthViewModelTests.swift
        tests/macos/CoreTests.swift
        tests/macos/DelegatedAuthTests.swift
        tests/macos/ImageEditorBridgeTests.swift
    )
    # Copy conversation fixture into the test bundle's Resources so
    # `Bundle(for:).url(forResource:)` can find it at test runtime.
    set_source_files_properties(
        tests/macos/fixtures_conversation_oldest.json
        PROPERTIES MACOSX_PACKAGE_LOCATION Resources
    )
    target_sources(PivoxTests PRIVATE
        tests/macos/fixtures_conversation_oldest.json
    )

    target_compile_options(PivoxTests PRIVATE
        "SHELL:-import-objc-header ${CMAKE_SOURCE_DIR}/platform/macos/Bridging-Header.h"
        "$<$<COMPILE_LANGUAGE:Swift>:-I${PIVOX_MODELS_MODULES_DIR}>"
        ${PIVOX_MODELS_MODULEMAP_FLAGS}
    )
    target_include_directories(PivoxTests PRIVATE
        "${CMAKE_SOURCE_DIR}/platform/macos/objcpp"
        "${CMAKE_SOURCE_DIR}/core/ai_elements/markdown"
        "${CMAKE_SOURCE_DIR}/core/ai_elements/highlight/include"
    )
    target_link_libraries(PivoxTests PRIVATE
        pivox_bridge pivox_markdown pivox_highlight_wrapper
        "${PIVOX_MODELS_DYLIB}"
    )
    add_dependencies(PivoxTests PivoxModelsSwiftBuild)

    xctest_add_test(PivoxUnitTests PivoxTests)

    # ----------------------------------------------------------
    # XCUITest UI tests
    # ----------------------------------------------------------
    xctest_add_bundle(PivoxUITests ${APP_TARGET}
        tests/macos/ui/SidebarUITests.swift
        tests/macos/ui/AuthUITests.swift
        tests/macos/ui/DelegatedAuthUITests.swift
        tests/macos/ui/ImageEditorUITests.swift
    )

    target_compile_options(PivoxUITests PRIVATE
        "SHELL:-import-objc-header ${CMAKE_SOURCE_DIR}/platform/macos/Bridging-Header.h"
    )
    target_include_directories(PivoxUITests PRIVATE
        "${CMAKE_SOURCE_DIR}/platform/macos/objcpp"
    )

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

    # Shared Windows source files — compiled directly into each target.
    # No shared DLL. Each target gets its own copy of all statics.
    set(WIN_SHARED_DIR "${WIN_PLATFORM_DIR}/shared")
    set(WIN_SHARED_SOURCES
        ${WIN_PLATFORM_DIR}/WinAppState.cpp
        ${WIN_PLATFORM_DIR}/WinAuthService.cpp
        ${WIN_SHARED_DIR}/PivoxServices.cpp
        ${WIN_SHARED_DIR}/OAuthPopup.cpp
        ${WIN_SHARED_DIR}/GoogleOAuth.cpp
        ${WIN_SHARED_DIR}/DelegatedAuthClient.cpp
        ${WIN_SHARED_DIR}/DragService.cpp
        ${WIN_SHARED_DIR}/Views/LoginPage.idl
        ${WIN_SHARED_DIR}/Views/LoginPage.xaml
        ${WIN_SHARED_DIR}/Views/LoginPage.xaml.h
        ${WIN_SHARED_DIR}/Views/LoginPage.xaml.cpp
        ${WIN_SHARED_DIR}/Views/DelegatedLoginPage.idl
        ${WIN_SHARED_DIR}/Views/DelegatedLoginPage.xaml
        ${WIN_SHARED_DIR}/Views/DelegatedLoginPage.xaml.h
        ${WIN_SHARED_DIR}/Views/DelegatedLoginPage.xaml.cpp
        ${WIN_SHARED_DIR}/Views/RegisterPage.idl
        ${WIN_SHARED_DIR}/Views/RegisterPage.xaml
        ${WIN_SHARED_DIR}/Views/RegisterPage.xaml.h
        ${WIN_SHARED_DIR}/Views/RegisterPage.xaml.cpp
        ${WIN_SHARED_DIR}/Views/MainPage.idl
        ${WIN_SHARED_DIR}/Views/MainPage.xaml
        ${WIN_SHARED_DIR}/Views/MainPage.xaml.h
        ${WIN_SHARED_DIR}/Views/MainPage.xaml.cpp
        ${WIN_SHARED_DIR}/Views/TestPage.idl
        ${WIN_SHARED_DIR}/Views/TestPage.xaml
        ${WIN_SHARED_DIR}/Views/TestPage.xaml.h
        ${WIN_SHARED_DIR}/Views/TestPage.xaml.cpp
    )

    # Common include directories for Windows targets.
    set(WIN_INCLUDE_DIRS
        ${WIN_PLATFORM_DIR}
        ${WIN_SHARED_DIR}
        ${WIN_SHARED_DIR}/Views
        "${CMAKE_SOURCE_DIR}/core"
        "${FB_CPP_ROOT}/include"
    )

    # Firebase C++ SDK — architecture-aware.
    if(CMAKE_GENERATOR_PLATFORM STREQUAL "Win32")
        set(FB_ARCH "x86")
    else()
        set(FB_ARCH "x64")
    endif()
    set(FB_LINK_DIR "${FB_CPP_ROOT}/libs/windows/VS2019/MD/${FB_ARCH}/$<CONFIG>")

    # Common link libraries for Windows targets.
    set(WIN_LINK_LIBS pivox_core crypt32 advapi32 firebase_app firebase_auth ws2_32 userenv)

    option(PIVOX_BUILD_ACTIVEX "Build ActiveX plugin (x86 only)" OFF)

    if(PIVOX_BUILD_ACTIVEX)
        if(NOT CMAKE_GENERATOR_PLATFORM STREQUAL "Win32")
            message(FATAL_ERROR "PIVOX_BUILD_ACTIVEX requires -A Win32")
        endif()
        add_subdirectory(platform/windows/activex)
    else()
        add_subdirectory(platform/windows/app)
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
        tests/core/crop_math_tests.cpp
        tests/core/image_editor_engine_tests.cpp
    )
    target_link_libraries(pivox_core_tests PRIVATE pivox_core GTest::gtest_main)

    include(GoogleTest)
    gtest_discover_tests(pivox_core_tests DISCOVERY_MODE PRE_TEST)

    # TODO: Windows integration tests need updating after shared-source refactor.
    # Tests reference removed pivox_win_state target and AuthStatus enum.

    # Windows delegated auth tests (deep link parsing).
    # Requires WinRT for URI parsing. Only built on Windows.
    if(WIN32)
        add_executable(pivox_delegated_auth_tests
            tests/windows/delegated_auth_tests.cpp
            ${CMAKE_SOURCE_DIR}/platform/windows/app/DeepLink.cpp
        )
        target_include_directories(pivox_delegated_auth_tests PRIVATE
            ${CMAKE_SOURCE_DIR}/platform/windows/app
        )
        target_link_libraries(pivox_delegated_auth_tests PRIVATE
            GTest::gtest_main windowsapp runtimeobject
        )
        set_target_properties(pivox_delegated_auth_tests PROPERTIES
            VS_GLOBAL_CppWinRTOptimized "true"
        )
        target_compile_definitions(pivox_delegated_auth_tests PRIVATE
            _SILENCE_ALL_CXX17_DEPRECATION_WARNINGS
        )
        gtest_discover_tests(pivox_delegated_auth_tests DISCOVERY_MODE PRE_TEST)
    endif()
endif()
