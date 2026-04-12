#pragma once

#include <string>
#include <windows.h>

// Deep link structure parsed from pivox:// URLs.
// Supported deep links:
//   pivox://auth/delegate/signin?session=<code>  — delegated auth from plugin
//   pivox://auth/delegate/profile                — open profile in app
//   pivox://auth/delegate/signout                — sign out and exit
struct DeepLink {
    bool isDelegatedAuth = false;  // true for any pivox://auth/delegate/* URL
    std::string action;            // "signin", "profile", "signout"
    std::string sessionCode;       // session code for "signin" action
};

// Parse the deep link from the command line (argv[1]).
DeepLink ParseDeepLink();

// Parse a deep link from a URL string (testable, no command line dependency).
DeepLink ParseDeepLinkFromUrl(const std::wstring& url);
