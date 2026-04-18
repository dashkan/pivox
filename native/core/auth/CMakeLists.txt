find_package(gRPC CONFIG REQUIRED)

add_library(pivox_auth STATIC
    token_provider.cc
    token_provider_c.cc
    auth_interceptor.cc
)

target_include_directories(pivox_auth PUBLIC
    "${CMAKE_CURRENT_SOURCE_DIR}"
)

target_link_libraries(pivox_auth PUBLIC
    gRPC::grpc++
)

# Tests
find_package(GTest CONFIG REQUIRED)

add_executable(pivox_auth_tests
    token_provider_test.cc
)

target_link_libraries(pivox_auth_tests PRIVATE
    pivox_auth
    GTest::gtest
    GTest::gtest_main
)

add_test(NAME PivoxAuthTests COMMAND pivox_auth_tests)
