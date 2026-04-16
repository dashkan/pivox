include(FetchContent)

FetchContent_Declare(
    Corrosion
    GIT_REPOSITORY https://github.com/corrosion-rs/corrosion.git
    GIT_TAG v0.5.2
)
FetchContent_MakeAvailable(Corrosion)

corrosion_import_crate(MANIFEST_PATH "${CMAKE_CURRENT_SOURCE_DIR}/Cargo.toml")

# Wrapper target that bundles the Rust static lib + C header.
add_library(pivox_highlight_wrapper INTERFACE)
target_include_directories(pivox_highlight_wrapper INTERFACE
    "${CMAKE_CURRENT_SOURCE_DIR}/include"
)
target_link_libraries(pivox_highlight_wrapper INTERFACE pivox-highlight)
