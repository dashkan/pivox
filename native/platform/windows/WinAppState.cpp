#include "WinAppState.h"
#include <windows.h>
#include <wincred.h>

static const wchar_t* kRegSubKey = L"Software\\Pivox";
static const wchar_t* kCredTarget = L"Pivox";

// Helper: convert UTF-8 std::string to wide string.
static std::wstring toWide(const std::string& s) {
    if (s.empty()) return {};
    int len = MultiByteToWideChar(CP_UTF8, 0, s.data(), static_cast<int>(s.size()), nullptr, 0);
    std::wstring ws(len, 0);
    MultiByteToWideChar(CP_UTF8, 0, s.data(), static_cast<int>(s.size()), ws.data(), len);
    return ws;
}

// Helper: convert wide string to UTF-8 std::string.
static std::string toUtf8(const std::wstring& ws) {
    if (ws.empty()) return {};
    int len = WideCharToMultiByte(CP_UTF8, 0, ws.data(), static_cast<int>(ws.size()), nullptr, 0, nullptr, nullptr);
    std::string s(len, 0);
    WideCharToMultiByte(CP_UTF8, 0, ws.data(), static_cast<int>(ws.size()), s.data(), len, nullptr, nullptr);
    return s;
}

// Helper: write a DWORD to registry.
static void regWriteDword(const wchar_t* name, DWORD value) {
    HKEY hKey;
    if (RegCreateKeyExW(HKEY_CURRENT_USER, kRegSubKey, 0, nullptr,
            0, KEY_WRITE, nullptr, &hKey, nullptr) == ERROR_SUCCESS) {
        RegSetValueExW(hKey, name, 0, REG_DWORD,
            reinterpret_cast<const BYTE*>(&value), sizeof(value));
        RegCloseKey(hKey);
    }
}

// Helper: read a DWORD from registry. Returns nullopt if not found.
static std::optional<DWORD> regReadDword(const wchar_t* name) {
    HKEY hKey;
    if (RegOpenKeyExW(HKEY_CURRENT_USER, kRegSubKey, 0, KEY_READ, &hKey) != ERROR_SUCCESS) {
        return std::nullopt;
    }
    DWORD value = 0;
    DWORD size = sizeof(value);
    DWORD type = 0;
    LSTATUS status = RegQueryValueExW(hKey, name, nullptr, &type,
        reinterpret_cast<BYTE*>(&value), &size);
    RegCloseKey(hKey);
    if (status != ERROR_SUCCESS || type != REG_DWORD) {
        return std::nullopt;
    }
    return value;
}

// Helper: write a string to registry.
static void regWriteString(const wchar_t* name, const std::wstring& value) {
    HKEY hKey;
    if (RegCreateKeyExW(HKEY_CURRENT_USER, kRegSubKey, 0, nullptr,
            0, KEY_WRITE, nullptr, &hKey, nullptr) == ERROR_SUCCESS) {
        RegSetValueExW(hKey, name, 0, REG_SZ,
            reinterpret_cast<const BYTE*>(value.c_str()),
            static_cast<DWORD>((value.size() + 1) * sizeof(wchar_t)));
        RegCloseKey(hKey);
    }
}

// Helper: read a string from registry.
static std::optional<std::wstring> regReadString(const wchar_t* name) {
    HKEY hKey;
    if (RegOpenKeyExW(HKEY_CURRENT_USER, kRegSubKey, 0, KEY_READ, &hKey) != ERROR_SUCCESS) {
        return std::nullopt;
    }
    DWORD size = 0;
    DWORD type = 0;
    LSTATUS status = RegQueryValueExW(hKey, name, nullptr, &type, nullptr, &size);
    if (status != ERROR_SUCCESS || type != REG_SZ || size == 0) {
        RegCloseKey(hKey);
        return std::nullopt;
    }
    std::wstring value(size / sizeof(wchar_t), 0);
    status = RegQueryValueExW(hKey, name, nullptr, nullptr,
        reinterpret_cast<BYTE*>(value.data()), &size);
    RegCloseKey(hKey);
    if (status != ERROR_SUCCESS) {
        return std::nullopt;
    }
    // Remove trailing null if present.
    if (!value.empty() && value.back() == L'\0') {
        value.pop_back();
    }
    return value;
}

namespace pivox {

// ---------------------------------------------------------------------------
// Window state — Registry
// ---------------------------------------------------------------------------

void WinAppState::saveWindowState(const WindowState& state) {
    regWriteDword(L"window.x", static_cast<DWORD>(state.x));
    regWriteDword(L"window.y", static_cast<DWORD>(state.y));
    regWriteDword(L"window.width", static_cast<DWORD>(state.width));
    regWriteDword(L"window.height", static_cast<DWORD>(state.height));
}

std::optional<WindowState> WinAppState::loadWindowState() {
    auto w = regReadDword(L"window.width");
    if (!w.has_value()) {
        return std::nullopt;
    }

    WindowState state;
    state.x = static_cast<int>(regReadDword(L"window.x").value_or(0));
    state.y = static_cast<int>(regReadDword(L"window.y").value_or(0));
    state.width = static_cast<int>(w.value());
    state.height = static_cast<int>(regReadDword(L"window.height").value_or(800));

    // Sanity check — don't restore absurd sizes.
    if (state.width < 200 || state.height < 200) {
        return std::nullopt;
    }

    return state;
}

// ---------------------------------------------------------------------------
// Generic key-value — Registry
// ---------------------------------------------------------------------------

void WinAppState::saveString(const std::string& key, const std::string& value) {
    regWriteString(toWide(key).c_str(), toWide(value));
}

std::optional<std::string> WinAppState::loadString(const std::string& key) {
    auto ws = regReadString(toWide(key).c_str());
    if (!ws.has_value()) {
        return std::nullopt;
    }
    return toUtf8(ws.value());
}

void WinAppState::saveBool(const std::string& key, bool value) {
    regWriteDword(toWide(key).c_str(), value ? 1 : 0);
}

std::optional<bool> WinAppState::loadBool(const std::string& key) {
    auto val = regReadDword(toWide(key).c_str());
    if (!val.has_value()) {
        return std::nullopt;
    }
    return val.value() != 0;
}

// ---------------------------------------------------------------------------
// Secure storage — Windows Credential Manager (CredWrite/CredRead)
// ---------------------------------------------------------------------------

void WinAppState::saveSecure(const std::string& key, const std::string& value) {
    std::wstring targetName = std::wstring(kCredTarget) + L":" + toWide(key);

    CREDENTIALW cred = {};
    cred.Type = CRED_TYPE_GENERIC;
    cred.TargetName = const_cast<LPWSTR>(targetName.c_str());
    cred.CredentialBlobSize = static_cast<DWORD>(value.size());
    cred.CredentialBlob = reinterpret_cast<LPBYTE>(const_cast<char*>(value.data()));
    cred.Persist = CRED_PERSIST_LOCAL_MACHINE;

    CredWriteW(&cred, 0);
}

std::optional<std::string> WinAppState::loadSecure(const std::string& key) {
    std::wstring targetName = std::wstring(kCredTarget) + L":" + toWide(key);

    PCREDENTIALW pCred = nullptr;
    if (!CredReadW(targetName.c_str(), CRED_TYPE_GENERIC, 0, &pCred)) {
        return std::nullopt;
    }

    std::string result(reinterpret_cast<const char*>(pCred->CredentialBlob),
                       pCred->CredentialBlobSize);
    CredFree(pCred);
    return result;
}

void WinAppState::deleteSecure(const std::string& key) {
    std::wstring targetName = std::wstring(kCredTarget) + L":" + toWide(key);
    CredDeleteW(targetName.c_str(), CRED_TYPE_GENERIC, 0);
}

// ---------------------------------------------------------------------------
// Protocol handler registration
// ---------------------------------------------------------------------------

void WinAppState::registerProtocolHandler() {
    // Get path to current executable.
    wchar_t exePath[MAX_PATH];
    GetModuleFileNameW(nullptr, exePath, MAX_PATH);

    // Register pivox:// URL scheme at HKCU\Software\Classes\pivox.
    HKEY hKey;
    if (RegCreateKeyExW(HKEY_CURRENT_USER, L"Software\\Classes\\pivox", 0, nullptr,
            0, KEY_WRITE, nullptr, &hKey, nullptr) == ERROR_SUCCESS) {
        const wchar_t* desc = L"Pivox OAuth Callback";
        RegSetValueExW(hKey, nullptr, 0, REG_SZ,
            reinterpret_cast<const BYTE*>(desc),
            static_cast<DWORD>((wcslen(desc) + 1) * sizeof(wchar_t)));
        const wchar_t* urlProtocol = L"";
        RegSetValueExW(hKey, L"URL Protocol", 0, REG_SZ,
            reinterpret_cast<const BYTE*>(urlProtocol), sizeof(wchar_t));
        RegCloseKey(hKey);
    }

    // Set the default icon.
    std::wstring iconKey = L"Software\\Classes\\pivox\\DefaultIcon";
    if (RegCreateKeyExW(HKEY_CURRENT_USER, iconKey.c_str(), 0, nullptr,
            0, KEY_WRITE, nullptr, &hKey, nullptr) == ERROR_SUCCESS) {
        std::wstring iconPath = std::wstring(exePath) + L",0";
        RegSetValueExW(hKey, nullptr, 0, REG_SZ,
            reinterpret_cast<const BYTE*>(iconPath.c_str()),
            static_cast<DWORD>((iconPath.size() + 1) * sizeof(wchar_t)));
        RegCloseKey(hKey);
    }

    // Set the command to launch the app with the URL.
    std::wstring cmdKey = L"Software\\Classes\\pivox\\shell\\open\\command";
    if (RegCreateKeyExW(HKEY_CURRENT_USER, cmdKey.c_str(), 0, nullptr,
            0, KEY_WRITE, nullptr, &hKey, nullptr) == ERROR_SUCCESS) {
        std::wstring cmd = std::wstring(L"\"") + exePath + L"\" \"%1\"";
        RegSetValueExW(hKey, nullptr, 0, REG_SZ,
            reinterpret_cast<const BYTE*>(cmd.c_str()),
            static_cast<DWORD>((cmd.size() + 1) * sizeof(wchar_t)));
        RegCloseKey(hKey);
    }
}

} // namespace pivox
