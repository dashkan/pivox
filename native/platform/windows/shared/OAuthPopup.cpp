#include "pch.h"
#include "OAuthPopup.h"

#include <winrt/Microsoft.UI.Xaml.Controls.h>
#include <winrt/Microsoft.Web.WebView2.Core.h>

namespace Pivox {

void LaunchOAuthPopup(
    const std::wstring& authUrl,
    const std::wstring& callbackScheme,
    OAuthPopupCallback callback)
{
    winrt::Microsoft::UI::Xaml::Window window;
    window.Title(L"Sign In");

    auto webview = winrt::Microsoft::UI::Xaml::Controls::WebView2();

    auto callbackFired = std::make_shared<bool>(false);
    auto cb = std::make_shared<OAuthPopupCallback>(std::move(callback));
    auto authUrlCopy = std::make_shared<std::wstring>(authUrl);

    // Intercept navigation to the redirect URI scheme.
    webview.NavigationStarting([window, callbackScheme, callbackFired, cb](
        auto&&, winrt::Microsoft::Web::WebView2::Core::CoreWebView2NavigationStartingEventArgs const& args) {
        auto uri = args.Uri();
        std::wstring uriStr(uri);

        if (uriStr.find(callbackScheme) == 0) {
            args.Cancel(true);

            std::string authCode;
            std::string error;

            winrt::Windows::Foundation::Uri parsedUri(uri);
            auto query = parsedUri.QueryParsed();
            for (uint32_t i = 0; i < query.Size(); i++) {
                auto entry = query.GetAt(i);
                auto name = winrt::to_string(entry.Name());
                if (name == "code") {
                    authCode = winrt::to_string(entry.Value());
                } else if (name == "error") {
                    error = winrt::to_string(entry.Value());
                }
            }

            if (!*callbackFired) {
                *callbackFired = true;
                if (!authCode.empty()) {
                    (*cb)({ true, authCode, "" });
                } else if (!error.empty()) {
                    (*cb)({ false, "", error });
                } else {
                    (*cb)({ false, "", "No auth code in callback" });
                }
            }

            window.Close();
        }
    });

    // Log WebView2 init status for diagnostics.
    webview.CoreWebView2Initialized([](
        auto&&, winrt::Microsoft::UI::Xaml::Controls::CoreWebView2InitializedEventArgs const& args) {
        if (auto ex = args.Exception()) {
            wchar_t buf[256];
            swprintf_s(buf, 256, L"[OAuthPopup] CoreWebView2 init error: 0x%08X\n",
                static_cast<int32_t>(ex));
            OutputDebugStringW(buf);
        } else {
            OutputDebugStringW(L"[OAuthPopup] CoreWebView2 initialized OK\n");
        }
    });

    // Handle window closed (user cancelled).
    window.Closed([callbackFired, cb](auto&&, auto&&) {
        if (!*callbackFired) {
            *callbackFired = true;
            (*cb)({ false, "", "cancelled" });
        }
    });

    // DEBUG: test if XAML renders at all without WebView2
    winrt::Microsoft::UI::Xaml::Controls::TextBlock testLabel;
    testLabel.Text(L"If you see this, XAML popup works. WebView2 is the problem.");
    testLabel.FontSize(20);
    testLabel.Foreground(winrt::Microsoft::UI::Xaml::Media::SolidColorBrush(winrt::Microsoft::UI::Colors::White()));
    window.Content(testLabel);
    window.AppWindow().Resize({ 500, 700 });
    window.Activate();

    // Redirect WebView2 user data folder to a writable location.
    // Default is next to host EXE which may be read-only (e.g., C:\Program Files).
    static bool s_udfSet = false;
    if (!s_udfSet) {
        PWSTR localAppData = nullptr;
        if (SUCCEEDED(SHGetKnownFolderPath(FOLDERID_LocalAppData, 0, nullptr, &localAppData))) {
            std::wstring udfPath = std::wstring(localAppData) + L"\\Pivox\\WebView2Cache";
            CoTaskMemFree(localAppData);
            SetEnvironmentVariableW(L"WEBVIEW2_USER_DATA_FOLDER", udfPath.c_str());
            OutputDebugStringW((L"[OAuthPopup] UDF set to " + udfPath + L"\n").c_str());
        }
        s_udfSet = true;
    }

    // DEBUG: Test if a simple TextBlock renders in the popup.
    winrt::Microsoft::UI::Xaml::Controls::TextBlock testText;
    testText.Text(L"WebView2 loading...");
    testText.FontSize(24);

    winrt::Microsoft::UI::Xaml::Controls::Grid grid;
    grid.Children().Append(testText);
    grid.Children().Append(webview);

    window.Content(grid);

    OutputDebugStringW(L"[OAuthPopup] Navigating via Source\n");
    webview.Source(winrt::Windows::Foundation::Uri(authUrl));
}

} // namespace Pivox
