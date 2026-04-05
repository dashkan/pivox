#import "AppStateBridge.h"
#import "MacAppState.h"

@implementation AppStateBridge {
    pivox::MacAppState _state;
}

+ (instancetype)shared {
    static AppStateBridge* instance = nil;
    static dispatch_once_t onceToken;
    dispatch_once(&onceToken, ^{
        instance = [[AppStateBridge alloc] init];
    });
    return instance;
}

// ---------------------------------------------------------------------------
// Window state
// ---------------------------------------------------------------------------

- (void)saveWindowX:(int)x y:(int)y width:(int)width height:(int)height {
    pivox::WindowState ws;
    ws.x = x;
    ws.y = y;
    ws.width = width;
    ws.height = height;
    _state.saveWindowState(ws);
}

- (BOOL)hasWindowState {
    return _state.loadWindowState().has_value();
}

- (int)windowX {
    auto ws = _state.loadWindowState();
    return ws ? ws->x : 0;
}

- (int)windowY {
    auto ws = _state.loadWindowState();
    return ws ? ws->y : 0;
}

- (int)windowWidth {
    auto ws = _state.loadWindowState();
    return ws ? ws->width : 1280;
}

- (int)windowHeight {
    auto ws = _state.loadWindowState();
    return ws ? ws->height : 800;
}

// ---------------------------------------------------------------------------
// Generic key-value
// ---------------------------------------------------------------------------

- (void)saveString:(NSString*)value forKey:(NSString*)key {
    _state.saveString(std::string([key UTF8String]), std::string([value UTF8String]));
}

- (nullable NSString*)loadStringForKey:(NSString*)key {
    auto result = _state.loadString(std::string([key UTF8String]));
    if (!result) return nil;
    return [NSString stringWithUTF8String:result->c_str()];
}

- (void)saveBool:(BOOL)value forKey:(NSString*)key {
    _state.saveBool(std::string([key UTF8String]), value);
}

- (BOOL)loadBoolForKey:(NSString*)key {
    auto result = _state.loadBool(std::string([key UTF8String]));
    return result.value_or(false);
}

- (BOOL)hasBoolForKey:(NSString*)key {
    return _state.loadBool(std::string([key UTF8String])).has_value();
}

// ---------------------------------------------------------------------------
// Secure storage
// ---------------------------------------------------------------------------

- (void)saveSecure:(NSString*)value forKey:(NSString*)key {
    _state.saveSecure(std::string([key UTF8String]), std::string([value UTF8String]));
}

- (nullable NSString*)loadSecureForKey:(NSString*)key {
    auto result = _state.loadSecure(std::string([key UTF8String]));
    if (!result) return nil;
    return [NSString stringWithUTF8String:result->c_str()];
}

- (void)deleteSecureForKey:(NSString*)key {
    _state.deleteSecure(std::string([key UTF8String]));
}

@end
