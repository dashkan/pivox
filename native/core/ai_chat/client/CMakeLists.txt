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
    pivox_auth
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

# Tests for this client are deferred: the previous C-API tests were
# retired when chat_client_c.{h,cc} was removed. New tests need to be
# written against the typed C++ API. Auth / interceptor is covered in
# native/core/auth tests.
