# Standalone CMakeLists for the markdown shared library consumed
# by Pivox.Native via P/Invoke. Builds a SHARED library (dylib on
# macOS, DLL on Windows) with cmark-gfm linked statically so
# downstream consumers don't need cmark-gfm installed at runtime.
#
# This is a *copy* of the native/core/ai_elements/markdown sources,
# kept in sync manually. The dotnet/ tree is intentionally
# self-contained so dotnet builds never reach into ../native/.
# See dotnet/CLAUDE.md for the self-containment rule and the
# rationale for the deliberate duplication.

cmake_minimum_required(VERSION 3.20)
project(pivox_markdown LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# cmark-gfm comes from vcpkg; the build script sets
# CMAKE_TOOLCHAIN_FILE to $VCPKG_ROOT/scripts/buildsystems/vcpkg.cmake
# before invoking CMake, so find_package resolves against the
# vcpkg-installed copy. Linking the static variant keeps the
# resulting dylib/DLL self-contained — runtime users don't need a
# separately-installed cmark-gfm.
find_package(cmark-gfm CONFIG REQUIRED)
find_package(cmark-gfm-extensions CONFIG REQUIRED)

add_library(pivox_markdown SHARED
    markdown_parser.cc
    markdown_parser_c.cc
)

target_include_directories(pivox_markdown PUBLIC
    "${CMAKE_CURRENT_SOURCE_DIR}"
)

target_link_libraries(pivox_markdown PRIVATE
    libcmark-gfm_static
    libcmark-gfm-extensions_static
)

# Position-independent code: required so the static cmark-gfm
# can link cleanly into our SHARED library on Linux/macOS. macOS
# clang sets -fPIC by default for SHARED targets but make it
# explicit so the build doesn't drift.
set_target_properties(pivox_markdown PROPERTIES
    POSITION_INDEPENDENT_CODE ON
    # MSVC exports every symbol by default when this is ON. Keeps
    # our markdown_parser_c.h identical to the native/ copy — no
    # __declspec(dllexport) macros needed in the header.
    WINDOWS_EXPORT_ALL_SYMBOLS ON
    # macOS install name relative to @rpath so the .NET host can
    # locate it via the standard runtime resolution path when the
    # dylib is co-located with the managed assembly.
    INSTALL_NAME_DIR "@rpath"
)
