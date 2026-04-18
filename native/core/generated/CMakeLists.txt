# Shared C++ proto bindings generated from api/proto/** via
# `make proto-generate-native` (buf.gen.native.yaml). Generated files live
# in cpp/ — this CMakeLists.txt intentionally sits above that directory so
# `buf generate`'s clean step cannot remove it.
#
# Consumed by shared C++ code (ai_chat/client, future WinRT port).

find_package(protobuf CONFIG REQUIRED)

file(GLOB_RECURSE PIVOX_PROTO_CPP_SOURCES
    CONFIGURE_DEPENDS
    "${CMAKE_CURRENT_SOURCE_DIR}/cpp/*.pb.cc"
)

add_library(pivox_protos_cpp STATIC ${PIVOX_PROTO_CPP_SOURCES})

target_include_directories(pivox_protos_cpp PUBLIC
    "${CMAKE_CURRENT_SOURCE_DIR}/cpp"
)

target_link_libraries(pivox_protos_cpp PUBLIC
    protobuf::libprotobuf
)
