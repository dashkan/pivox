// Bridging header for Swift ↔ C++ interop via Obj-C.
#import "AppStateBridge.h"
#import "ImageEditorBridge.h"

// Firebase ID-token provider registration. Pure C ABI — no C++ required.
// Called once at startup (AppDelegate) to install the Swift-side token
// fetcher into the shared gRPC auth interceptor.
#import "token_provider_c.h"

// AIElements markdown parser — shared C++ cmark-gfm wrapper accessed via
// a JSON seam. Swift decodes the returned JSON into typed MarkdownBlock
// values for rendering.
#import "markdown_parser_c.h"

// AIElements syntax highlighter — Rust tree-sitter behind a plain-C
// seam. Shipped as a cdylib embedded in Contents/Frameworks; dyld
// resolves @rpath/libpivox_highlight.dylib at launch via the main
// executable's LD_RUNPATH_SEARCH_PATHS.
#import "pivox_highlight.h"

// AI Chat — typed C++ client via Swift↔C++ interop. ChatClient is a
// shared-reference type; Swift calls its methods directly.
#ifdef __cplusplus
#include "chat_client.h"
#endif
