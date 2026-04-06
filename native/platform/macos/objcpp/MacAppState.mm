#import "MacAppState.h"
#import <Foundation/Foundation.h>
#import <Security/Security.h>

static NSString* const kWindowX = @"pivox.window.x";
static NSString* const kWindowY = @"pivox.window.y";
static NSString* const kWindowWidth = @"pivox.window.width";
static NSString* const kWindowHeight = @"pivox.window.height";

static NSString* const kKeychainService = @"app.pivox.native";

namespace pivox {

// ---------------------------------------------------------------------------
// Window state — NSUserDefaults
// ---------------------------------------------------------------------------

void MacAppState::saveWindowState(const WindowState& state) {
    NSUserDefaults* defaults = [NSUserDefaults standardUserDefaults];
    [defaults setInteger:state.x forKey:kWindowX];
    [defaults setInteger:state.y forKey:kWindowY];
    [defaults setInteger:state.width forKey:kWindowWidth];
    [defaults setInteger:state.height forKey:kWindowHeight];
}

std::optional<WindowState> MacAppState::loadWindowState() {
    NSUserDefaults* defaults = [NSUserDefaults standardUserDefaults];

    // Check if any window state was ever saved.
    if ([defaults objectForKey:kWindowWidth] == nil) {
        return std::nullopt;
    }

    WindowState state;
    state.x = static_cast<int>([defaults integerForKey:kWindowX]);
    state.y = static_cast<int>([defaults integerForKey:kWindowY]);
    state.width = static_cast<int>([defaults integerForKey:kWindowWidth]);
    state.height = static_cast<int>([defaults integerForKey:kWindowHeight]);

    // Sanity check — don't restore absurd sizes.
    if (state.width < 200 || state.height < 200) {
        return std::nullopt;
    }

    return state;
}

// ---------------------------------------------------------------------------
// Generic key-value — NSUserDefaults
// ---------------------------------------------------------------------------

void MacAppState::saveString(const std::string& key, const std::string& value) {
    NSUserDefaults* defaults = [NSUserDefaults standardUserDefaults];
    NSString* nsKey = [NSString stringWithUTF8String:key.c_str()];
    NSString* nsValue = [NSString stringWithUTF8String:value.c_str()];
    [defaults setObject:nsValue forKey:nsKey];
}

std::optional<std::string> MacAppState::loadString(const std::string& key) {
    NSUserDefaults* defaults = [NSUserDefaults standardUserDefaults];
    NSString* nsKey = [NSString stringWithUTF8String:key.c_str()];
    NSString* value = [defaults stringForKey:nsKey];
    if (value == nil) {
        return std::nullopt;
    }
    return std::string([value UTF8String]);
}

void MacAppState::saveBool(const std::string& key, bool value) {
    NSUserDefaults* defaults = [NSUserDefaults standardUserDefaults];
    NSString* nsKey = [NSString stringWithUTF8String:key.c_str()];
    [defaults setBool:value forKey:nsKey];
}

std::optional<bool> MacAppState::loadBool(const std::string& key) {
    NSUserDefaults* defaults = [NSUserDefaults standardUserDefaults];
    NSString* nsKey = [NSString stringWithUTF8String:key.c_str()];
    if ([defaults objectForKey:nsKey] == nil) {
        return std::nullopt;
    }
    return [defaults boolForKey:nsKey];
}

// ---------------------------------------------------------------------------
// Secure storage — Keychain Services
// ---------------------------------------------------------------------------

void MacAppState::saveSecure(const std::string& key, const std::string& value) {
    // Delete existing item first (update = delete + add).
    deleteSecure(key);

    NSData* data = [NSData dataWithBytes:value.data() length:value.size()];
    NSString* account = [NSString stringWithUTF8String:key.c_str()];

    NSDictionary* query = @{
        (__bridge id)kSecClass: (__bridge id)kSecClassGenericPassword,
        (__bridge id)kSecAttrService: kKeychainService,
        (__bridge id)kSecAttrAccount: account,
        (__bridge id)kSecValueData: data,
        (__bridge id)kSecAttrAccessible: (__bridge id)kSecAttrAccessibleWhenUnlocked,
    };

    SecItemAdd((__bridge CFDictionaryRef)query, nil);
}

std::optional<std::string> MacAppState::loadSecure(const std::string& key) {
    NSString* account = [NSString stringWithUTF8String:key.c_str()];

    NSDictionary* query = @{
        (__bridge id)kSecClass: (__bridge id)kSecClassGenericPassword,
        (__bridge id)kSecAttrService: kKeychainService,
        (__bridge id)kSecAttrAccount: account,
        (__bridge id)kSecReturnData: @YES,
        (__bridge id)kSecMatchLimit: (__bridge id)kSecMatchLimitOne,
    };

    CFTypeRef result = nil;
    OSStatus status = SecItemCopyMatching((__bridge CFDictionaryRef)query, &result);

    if (status != errSecSuccess || result == nil) {
        return std::nullopt;
    }

    NSData* data = (__bridge_transfer NSData*)result;
    return std::string(static_cast<const char*>(data.bytes), data.length);
}

void MacAppState::deleteSecure(const std::string& key) {
    NSString* account = [NSString stringWithUTF8String:key.c_str()];

    NSDictionary* query = @{
        (__bridge id)kSecClass: (__bridge id)kSecClassGenericPassword,
        (__bridge id)kSecAttrService: kKeychainService,
        (__bridge id)kSecAttrAccount: account,
    };

    SecItemDelete((__bridge CFDictionaryRef)query);
}

} // namespace pivox
