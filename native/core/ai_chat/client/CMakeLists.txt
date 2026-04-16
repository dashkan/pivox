find_package(gRPC CONFIG REQUIRED)
find_package(protobuf CONFIG REQUIRED)

add_library(pivox_ai_chat_client STATIC
    chat_client.cc
    chat_client_c.cc
)

target_include_directories(pivox_ai_chat_client PUBLIC
    "${CMAKE_CURRENT_SOURCE_DIR}"
)

target_link_libraries(pivox_ai_chat_client PUBLIC
    gRPC::grpc++
    protobuf::libprotobuf
)

# Tests
find_package(GTest CONFIG REQUIRED)

add_executable(pivox_ai_chat_client_tests
    chat_client_test.cc
)

target_link_libraries(pivox_ai_chat_client_tests PRIVATE
    pivox_ai_chat_client
    GTest::gtest
    GTest::gtest_main
)

add_test(NAME AiChatClientTests COMMAND pivox_ai_chat_client_tests)
