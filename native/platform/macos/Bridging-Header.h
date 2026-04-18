// Bridging header for Swift ↔ C++ interop via Obj-C.
#import "AppStateBridge.h"
#import "ImageEditorBridge.h"

// AI Chat — typed C++ client via Swift↔C++ interop. ChatClient is a
// shared-reference type; Swift calls its methods directly.
#ifdef __cplusplus
#include "chat_client.h"
#endif
