// Bridging header for Swift ↔ C++ interop via Obj-C.
#import "AppStateBridge.h"
#import "ImageEditorBridge.h"

// Firebase ID-token provider registration. Pure C ABI — no C++ required.
// Called once at startup (AppDelegate) to install the Swift-side token
// fetcher into the shared gRPC auth interceptor.
#import "token_provider_c.h"

// AI Chat — typed C++ client via Swift↔C++ interop. ChatClient is a
// shared-reference type; Swift calls its methods directly.
#ifdef __cplusplus
#include "chat_client.h"
#endif
