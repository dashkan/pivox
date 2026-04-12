#include "pch.h"
#include "OAuthPopup.h"

#include <shlobj.h>
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

    // Handle window closed (user cancelled).
    window.Closed([callbackFired, cb](auto&&, auto&&) {
        if (!*callbackFired) {
            *callbackFired = true;
            (*cb)({ false, "", "cancelled" });
        }
    });

    window.Content(webview);
    window.AppWindow().Resize({ 500, 700 });
    window.Activate();

    // Navigate — WebView2 auto-initializes on first navigation.
    webview.Source(winrt::Windows::Foundation::Uri(authUrl));
}

} // namespace Pivox
