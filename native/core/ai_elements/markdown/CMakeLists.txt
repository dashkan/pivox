find_package(cmark-gfm CONFIG REQUIRED)
find_package(cmark-gfm-extensions CONFIG REQUIRED)

file(GLOB_RECURSE PIVOX_MARKDOWN_SOURCES CONFIGURE_DEPENDS
    "${CMAKE_CURRENT_SOURCE_DIR}/*.cc"
)
list(FILTER PIVOX_MARKDOWN_SOURCES EXCLUDE REGEX "_test\\.cc$")

add_library(pivox_markdown STATIC ${PIVOX_MARKDOWN_SOURCES})

target_include_directories(pivox_markdown PUBLIC
    "${CMAKE_CURRENT_SOURCE_DIR}"
)

target_link_libraries(pivox_markdown PUBLIC
    libcmark-gfm_static
    libcmark-gfm-extensions_static
)

# Tests
find_package(GTest CONFIG REQUIRED)

file(GLOB_RECURSE PIVOX_MARKDOWN_TESTS CONFIGURE_DEPENDS
    "${CMAKE_CURRENT_SOURCE_DIR}/*_test.cc"
)

add_executable(pivox_markdown_tests ${PIVOX_MARKDOWN_TESTS})

target_link_libraries(pivox_markdown_tests PRIVATE
    pivox_markdown
    GTest::gtest
    GTest::gtest_main
)

add_test(NAME MarkdownParserTests COMMAND pivox_markdown_tests)
