find_package(gRPC CONFIG REQUIRED)
find_package(protobuf CONFIG REQUIRED)

add_library(pivox_ai_chat_client STATIC
    chat_client.cc
)

target_include_directories(pivox_ai_chat_client PUBLIC
    "${CMAKE_CURRENT_SOURCE_DIR}"
)

target_link_libraries(pivox_ai_chat_client PUBLIC
    gRPC::grpc++
    protobuf::libprotobuf
    pivox_protos_cpp
)

# On Apple, the bridge (chat_client_apple.cc) consumes PivoxModels-Swift.h —
# the C++ header Swift emits when PivoxModels is built with interop mode.
# Xcode places it under PivoxModels.build/$(CONFIG)/DerivedSources/.
# The target dependency on PivoxModels is wired from native/CMakeLists.txt
# after PivoxModels is defined (it's created later in the top-level file).
if(APPLE)
    # PUBLIC so consumers (the app target's bridging header) can also resolve
    # PivoxModels-Swift.h when it's included transitively from chat_client.h.
    target_include_directories(pivox_ai_chat_client PUBLIC
        "${CMAKE_BINARY_DIR}/build/PivoxModels.build/$<CONFIG>/DerivedSources"
    )
endif()

# Bridge generated code (.h.inc / .cc.inc produced by
# protoc-gen-pivox-cpp-bridge). PUBLIC because chat_client.h #includes
# the .h.inc, so any consumer that compiles chat_client.h (including the
# Swift app target's Bridging-Header) needs this path.
target_include_directories(pivox_ai_chat_client PUBLIC
    "${CMAKE_SOURCE_DIR}/core/generated/bridge"
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
