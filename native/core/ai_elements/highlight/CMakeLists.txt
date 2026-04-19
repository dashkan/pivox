include(FetchContent)

FetchContent_Declare(
    Corrosion
    GIT_REPOSITORY https://github.com/corrosion-rs/corrosion.git
    GIT_TAG v0.5.2
)
FetchContent_MakeAvailable(Corrosion)

corrosion_import_crate(MANIFEST_PATH "${CMAKE_CURRENT_SOURCE_DIR}/Cargo.toml")

# Bake @rpath into the dylib's LC_ID_DYLIB at compile time (via rustc's
# link-arg). This is the production-standard approach — running
# `install_name_tool -id …` in POST_BUILD invalidates ad-hoc signatures
# and can misalign compact unwind tables. Cargo's default install name
# is just the filename, which is brittle for notarization.
corrosion_add_target_local_rustflags(pivox_highlight
    "-C" "link-arg=-Wl,-install_name,@rpath/libpivox_highlight.dylib"
)

# Wrapper target that bundles the Rust dylib + C header.
add_library(pivox_highlight_wrapper INTERFACE)
target_include_directories(pivox_highlight_wrapper INTERFACE
    "${CMAKE_CURRENT_SOURCE_DIR}/include"
)
target_link_libraries(pivox_highlight_wrapper INTERFACE pivox_highlight)
