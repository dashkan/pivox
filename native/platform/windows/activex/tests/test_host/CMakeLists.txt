# Minimal Win32 test host for the PivoxActiveX control.
# Used for manual testing and automated UIA tests.

add_executable(PivoxTestHost WIN32
    main.cpp
)

target_include_directories(PivoxTestHost PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/../.."  # For PivoxControl_i.h (MIDL generated)
)

target_link_libraries(PivoxTestHost PRIVATE
    ole32 oleaut32 uuid
)
