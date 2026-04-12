#include "pch.h"
#include "DelegatedAuthClient.h"

#include <winrt/Windows.Web.Http.h>
#include <winrt/Windows.Web.Http.Headers.h>
#include <winrt/Windows.Data.Json.h>
#include <winrt/Windows.Storage.Streams.h>
#include <chrono>

namespace pivox {

static void DebugLog(const char* msg) {
    auto now = std::chrono::system_clock::now();
    auto ms = std::chrono::duration_cast<std::chrono::milliseconds>(now.time_since_epoch()).count() % 100000;
    char buf[512];
    snprintf(buf, sizeof(buf), "[DelegatedAuth %05lld] %s\n", static_cast<long long>(ms), msg);
    OutputDebugStringA(buf);
}

std::string DelegatedAuthClient::backendUrl() {
    wchar_t buf[512] = {};
    DWORD len = GetEnvironmentVariableW(L"PIVOX_BACKEND_URL", buf, 512);
    if (len > 0 && len < 512) {
        int utf8Len = WideCharToMultiByte(CP_UTF8, 0, buf, len, nullptr, 0, nullptr, nullptr);
        std::string url(utf8Len, 0);
        WideCharToMultiByte(CP_UTF8, 0, buf, len, url.data(), utf8Len, nullptr, nullptr);
        return url;
    }
    return "https://pivox.ngrok.app";
}

void DelegatedAuthClient::CreateSession(std::function<void(DelegatedAuthSession)> callback) {
    [](auto cb) -> winrt::fire_and_forget {
        try {
            DebugLog("createSession: sending request");
            auto httpClient = winrt::Windows::Web::Http::HttpClient();
            auto url = backendUrl() + "/internal/v1/auth:createDelegatedAuthSession";

            auto response = co_await httpClient.PostAsync(
                winrt::Windows::Foundation::Uri(winrt::to_hstring(url)),
                winrt::Windows::Web::Http::HttpStringContent(
                    L"{}",
                    winrt::Windows::Storage::Streams::UnicodeEncoding::Utf8,
                    L"application/json"));

            DelegatedAuthSession session;
            auto status = static_cast<int>(response.StatusCode());
            if (!response.IsSuccessStatusCode()) {
                char buf[128];
                snprintf(buf, sizeof(buf), "createSession: failed HTTP %d", status);
                DebugLog(buf);
                cb(session);
                co_return;
            }

            auto body = co_await response.Content().ReadAsStringAsync();
            auto json = winrt::Windows::Data::Json::JsonObject::Parse(body);
            if (json.HasKey(L"code")) {
                session.code = winrt::to_string(json.GetNamedString(L"code"));
            }
            DebugLog(("createSession: OK, code=" + session.code).c_str());
            if (json.HasKey(L"pollInterval")) {
                session.pollIntervalSeconds = static_cast<int>(json.GetNamedNumber(L"pollInterval"));
            }
            cb(session);
        } catch (...) {
            OutputDebugStringA("[DelegatedAuth] createSession exception\n");
            cb({});
        }
    }(std::move(callback));
}

void DelegatedAuthClient::CompleteSession(const std::string& code, const std::string& idToken,
                                           std::function<void(bool)> callback) {
    [](auto code, auto idToken, auto cb) -> winrt::fire_and_forget {
        try {
            DebugLog(("completeSession: sending for code=" + code).c_str());
            auto httpClient = winrt::Windows::Web::Http::HttpClient();
            auto url = backendUrl() + "/internal/v1/auth:completeDelegatedAuthSession";

            auto jsonBody = winrt::Windows::Data::Json::JsonObject();
            jsonBody.SetNamedValue(L"code",
                winrt::Windows::Data::Json::JsonValue::CreateStringValue(winrt::to_hstring(code)));

            auto content = winrt::Windows::Web::Http::HttpStringContent(
                jsonBody.Stringify(),
                winrt::Windows::Storage::Streams::UnicodeEncoding::Utf8,
                L"application/json");

            auto request = winrt::Windows::Web::Http::HttpRequestMessage(
                winrt::Windows::Web::Http::HttpMethod::Post(),
                winrt::Windows::Foundation::Uri(winrt::to_hstring(url)));
            request.Content(content);
            request.Headers().Authorization(
                winrt::Windows::Web::Http::Headers::HttpCredentialsHeaderValue(
                    L"Bearer", winrt::to_hstring(idToken)));

            auto response = co_await httpClient.SendRequestAsync(request);
            auto status = static_cast<int>(response.StatusCode());
            if (!response.IsSuccessStatusCode()) {
                char buf[128];
                snprintf(buf, sizeof(buf), "completeSession: failed HTTP %d", status);
                DebugLog(buf);
                cb(false);
                co_return;
            }
            DebugLog("completeSession: OK");
            cb(true);
        } catch (...) {
            OutputDebugStringA("[DelegatedAuth] completeSession exception\n");
            cb(false);
        }
    }(code, idToken, std::move(callback));
}

void DelegatedAuthClient::PollSession(const std::string& code,
                                       std::function<void(DelegatedAuthPollResponse)> callback) {
    [](auto code, auto cb) -> winrt::fire_and_forget {
        try {
            DebugLog("pollSession: sending request");
            auto httpClient = winrt::Windows::Web::Http::HttpClient();
            auto url = backendUrl() + "/internal/v1/auth:pollDelegatedAuthSession";

            auto jsonBody = winrt::Windows::Data::Json::JsonObject();
            jsonBody.SetNamedValue(L"code",
                winrt::Windows::Data::Json::JsonValue::CreateStringValue(winrt::to_hstring(code)));

            auto content = winrt::Windows::Web::Http::HttpStringContent(
                jsonBody.Stringify(),
                winrt::Windows::Storage::Streams::UnicodeEncoding::Utf8,
                L"application/json");

            auto response = co_await httpClient.PostAsync(
                winrt::Windows::Foundation::Uri(winrt::to_hstring(url)),
                content);

            DelegatedAuthPollResponse result;

            auto httpStatus = static_cast<int>(response.StatusCode());

            if (response.StatusCode() == winrt::Windows::Web::Http::HttpStatusCode::NotFound) {
                DebugLog("pollSession: 404 expired");
                result.result = PollResult::Expired;
                cb(result);
                co_return;
            }

            if (!response.IsSuccessStatusCode()) {
                result.result = PollResult::Error;
                result.error = "HTTP " + std::to_string(httpStatus);
                char buf[128];
                snprintf(buf, sizeof(buf), "pollSession: failed HTTP %d", httpStatus);
                DebugLog(buf);
                cb(result);
                co_return;
            }

            auto body = co_await response.Content().ReadAsStringAsync();
            auto json = winrt::Windows::Data::Json::JsonObject::Parse(body);

            if (json.HasKey(L"customToken")) {
                result.result = PollResult::Ready;
                result.customToken = winrt::to_string(json.GetNamedString(L"customToken"));
                DebugLog("pollSession: READY, got custom token");
            } else if (json.HasKey(L"status")) {
                auto status = winrt::to_string(json.GetNamedString(L"status"));
                result.result = (status == "pending") ? PollResult::Pending : PollResult::Error;
                DebugLog(("pollSession: " + status).c_str());
            }
            cb(result);
        } catch (...) {
            OutputDebugStringA("[DelegatedAuth] pollSession exception\n");
            cb({ PollResult::Error, "", "exception" });
        }
    }(code, std::move(callback));
}

} // namespace pivox
