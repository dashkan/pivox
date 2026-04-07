# Obj-C++ bridge library — wraps C++ core for Swift consumption.
# macOS-only. Windows calls C++ directly.

add_library(pivox_bridge STATIC
    objcpp/MacAppState.mm
    objcpp/AppStateBridge.mm
    objcpp/ImageEditorBridge.mm
)

# Enable ARC for all Obj-C++ bridge files.
target_compile_options(pivox_bridge PRIVATE "-fobjc-arc")

target_link_libraries(pivox_bridge PUBLIC pivox_core)
target_include_directories(pivox_bridge PUBLIC
    "${CMAKE_SOURCE_DIR}/core"
    "${CMAKE_SOURCE_DIR}/core/image-editor"
    "${CMAKE_SOURCE_DIR}/platform/macos/objcpp"
)
target_link_libraries(pivox_bridge PRIVATE "-framework Security")
