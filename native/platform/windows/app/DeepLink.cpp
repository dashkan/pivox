#include "DeepLink.h"

#include <winrt/Windows.Foundation.h>
#include <winrt/Windows.Foundation.Collections.h>
#include <shellapi.h>

DeepLink ParseDeepLinkFromUrl(const std::wstring& url) {
    DeepLink link;
    if (url.empty()) return link;
    try {
        winrt::Windows::Foundation::Uri uri(url);
        auto scheme = winrt::to_string(uri.SchemeName());
        auto host = winrt::to_string(uri.Host());
        auto path = winrt::to_string(uri.Path());
        if (scheme == "pivox" && host == "auth" && path.rfind("/delegate/", 0) == 0) {
            link.isDelegatedAuth = true;
            link.action = path.substr(10); // after "/delegate/"
            if (link.action == "signin") {
                auto query = uri.QueryParsed();
                for (uint32_t i = 0; i < query.Size(); i++) {
                    if (winrt::to_string(query.GetAt(i).Name()) == "session") {
                        link.sessionCode = winrt::to_string(query.GetAt(i).Value());
                        break;
                    }
                }
            }
        }
    } catch (...) {}
    return link;
}

DeepLink ParseDeepLink() {
    int argc = 0;
    LPWSTR* argv = CommandLineToArgvW(GetCommandLineW(), &argc);
    DeepLink link;
    if (argv && argc >= 2) {
        link = ParseDeepLinkFromUrl(argv[1]);
    }
    if (argv) LocalFree(argv);
    return link;
}
