add_library(pivox_core STATIC
    core.cpp
)

target_include_directories(pivox_core PUBLIC
    "${CMAKE_CURRENT_SOURCE_DIR}"
)
