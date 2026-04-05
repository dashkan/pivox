#pragma once

#import <Foundation/Foundation.h>

/// Obj-C bridge exposing AppState to Swift.
/// Wraps the C++ MacAppState so Swift doesn't need C++ interop.
@interface AppStateBridge : NSObject

+ (instancetype)shared;

// Window state
- (void)saveWindowX:(int)x y:(int)y width:(int)width height:(int)height;
- (BOOL)hasWindowState;
- (int)windowX;
- (int)windowY;
- (int)windowWidth;
- (int)windowHeight;

// Generic key-value
- (void)saveString:(NSString*)value forKey:(NSString*)key;
- (nullable NSString*)loadStringForKey:(NSString*)key;
- (void)saveBool:(BOOL)value forKey:(NSString*)key;
- (BOOL)loadBoolForKey:(NSString*)key;
- (BOOL)hasBoolForKey:(NSString*)key;

// Secure storage (Keychain)
- (void)saveSecure:(NSString*)value forKey:(NSString*)key;
- (nullable NSString*)loadSecureForKey:(NSString*)key;
- (void)deleteSecureForKey:(NSString*)key;

@end
