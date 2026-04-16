// Bridging header for Swift ↔ C++ interop via Obj-C.
#import "AppStateBridge.h"
#import "ImageEditorBridge.h"

// AI Chat — pure C FFI (no Obj-C wrapper needed).
#include "chat_client_c.h"
