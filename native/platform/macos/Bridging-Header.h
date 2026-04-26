// Bridging header for Swift ↔ C interop.
//
// All gRPC communication with Pivox cloud is Swift-native (grpc-swift-2);
// no C++ bridge involved. What lives here are pure-C/Obj-C++ seams to
// shared C/Rust libraries that don't fit into Swift directly:
//
//   - AppStateBridge / ImageEditorBridge: Obj-C++ seams to the C++ image
//     editor engine.
//   - markdown_parser_c: shared C++ cmark-gfm wrapper accessed via a
//     plain-C JSON seam. Swift decodes the JSON into typed
//     MarkdownBlock values for rendering.
//   - pivox_highlight: Rust tree-sitter highlighter behind a plain-C
//     seam. Shipped as a cdylib embedded in Contents/Frameworks; dyld
//     resolves @rpath/libpivox_highlight.dylib at launch via the main
//     executable's LD_RUNPATH_SEARCH_PATHS.

#import "AppStateBridge.h"
#import "ImageEditorBridge.h"
#import "markdown_parser_c.h"
#import "pivox_highlight.h"
