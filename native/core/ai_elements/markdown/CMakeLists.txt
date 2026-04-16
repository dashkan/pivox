find_package(cmark-gfm CONFIG REQUIRED)
find_package(cmark-gfm-extensions CONFIG REQUIRED)

add_library(pivox_markdown STATIC
    markdown_parser.cc
)

target_include_directories(pivox_markdown PUBLIC
    "${CMAKE_CURRENT_SOURCE_DIR}"
)

target_link_libraries(pivox_markdown PUBLIC
    libcmark-gfm_static
    libcmark-gfm-extensions_static
)

# Tests
find_package(GTest CONFIG REQUIRED)

add_executable(pivox_markdown_tests
    markdown_parser_test.cc
)

target_link_libraries(pivox_markdown_tests PRIVATE
    pivox_markdown
    GTest::gtest
    GTest::gtest_main
)

add_test(NAME MarkdownParserTests COMMAND pivox_markdown_tests)
