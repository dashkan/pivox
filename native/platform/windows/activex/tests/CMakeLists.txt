# ActiveX control tests.

find_package(GTest CONFIG)
if(GTest_FOUND)
    set(AX_TEST_DIR "${CMAKE_CURRENT_SOURCE_DIR}")

    add_executable(pivox_activex_tests
        ${AX_TEST_DIR}/test_com_interfaces.cpp
        ${AX_TEST_DIR}/test_drag_formats.cpp
    )

    target_include_directories(pivox_activex_tests PRIVATE
        "${CMAKE_CURRENT_SOURCE_DIR}/.."   # For PivoxControl_i.h (MIDL generated)
    )

    target_link_libraries(pivox_activex_tests PRIVATE
        GTest::gtest_main ole32 oleaut32 uuid
    )

    include(GoogleTest)
    gtest_discover_tests(pivox_activex_tests DISCOVERY_MODE PRE_TEST)

    # Test host application.
    add_subdirectory(test_host)
endif()
