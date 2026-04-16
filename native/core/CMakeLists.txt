add_subdirectory(ai_chat/client)
add_subdirectory(ai_elements/markdown)

add_library(pivox_core STATIC
    core.cpp
    image-editor/image_editor_types.cpp
    image-editor/crop_math.cpp
    image-editor/image_editor_engine.cpp
)

target_include_directories(pivox_core PUBLIC
    "${CMAKE_CURRENT_SOURCE_DIR}"
    "${CMAKE_CURRENT_SOURCE_DIR}/image-editor"
)

# MSVC requires _USE_MATH_DEFINES for M_PI in <cmath>.
if(MSVC)
    target_compile_definitions(pivox_core PUBLIC _USE_MATH_DEFINES)
endif()
