#pragma once

#include <string>
#include <functional>

namespace pivox {

struct DelegatedAuthSession {
    std::string code;
    int pollIntervalSeconds = 5;
};

enum class PollResult {
    Pending,
    Ready,
    Expired,
    Error,
};

struct DelegatedAuthPollResponse {
    PollResult result = PollResult::Error;
    std::string customToken;  // Only set when result == Ready.
    std::string error;
};

/// HTTP client for the delegated auth session endpoints.
/// All methods are async (fire_and_forget coroutines with callbacks).
class DelegatedAuthClient {
public:
    /// Returns the backend URL.
    /// Override: set PIVOX_BACKEND_URL environment variable.
    /// Default: https://pivox.ngrok.app
    static std::string backendUrl();

    /// POST /internal/v1/auth:createDelegatedAuthSession
    static void CreateSession(std::function<void(DelegatedAuthSession)> callback);

    /// POST /internal/v1/auth:completeDelegatedAuthSession
    static void CompleteSession(const std::string& code, const std::string& idToken,
                                std::function<void(bool)> callback);

    /// POST /internal/v1/auth:pollDelegatedAuthSession
    static void PollSession(const std::string& code,
                            std::function<void(DelegatedAuthPollResponse)> callback);
};

} // namespace pivox
