#include "pch.h"
#include "DelegatedLoginPage.xaml.h"
#include "DelegatedLoginPage.g.cpp"
#include "PivoxServices.h"
#include "DelegatedAuthClient.h"

namespace winrt::Pivox::implementation
{
    DelegatedLoginPage::DelegatedLoginPage()
    {
        InitializeComponent();
    }

    void DelegatedLoginPage::OnSignIn(IInspectable const&, Microsoft::UI::Xaml::RoutedEventArgs const&)
    {
        StartDelegatedAuth();
    }

    void DelegatedLoginPage::SetLoading(bool loading)
    {
        SignInButton().IsEnabled(!loading);
        SignInSpinner().IsActive(loading);
        SignInSpinner().Visibility(loading
            ? Microsoft::UI::Xaml::Visibility::Visible
            : Microsoft::UI::Xaml::Visibility::Collapsed);
        SignInText().Text(loading ? L"" : L"Sign In with Pivox");
    }

    void DelegatedLoginPage::ShowError(const std::string& message)
    {
        ErrorText().Text(winrt::to_hstring(message));
        ErrorText().Opacity(1);
    }

    // Delegated auth flow:
    //   1. Create a session on the backend → get code + poll interval
    //   2. Launch the Pivox app via deep link with the session code
    //   3. Poll the backend until the app completes the session
    //   4. Sign in with the custom token → AuthStateListener navigates away
    void DelegatedLoginPage::StartDelegatedAuth()
    {
        SetLoading(true);
        ErrorText().Opacity(0);
        SignInText().Text(L"Connecting...");

        auto weakThis = get_weak();

        // Step 1: Create session on the backend.
        OutputDebugStringA("[PivoxActiveX] Starting delegated auth flow\n");
        pivox::DelegatedAuthClient::CreateSession(
            [weakThis](pivox::DelegatedAuthSession session) {
                if (session.code.empty()) {
                    OutputDebugStringA("[PivoxActiveX] ERROR: Failed to create session\n");
                    if (auto s = weakThis.get()) {
                        s->SetLoading(false);
                        s->ShowError("Failed to create auth session. Is the backend running?");
                    }
                    return;
                }

                // Step 2: Launch the Pivox app for authentication.
                OutputDebugStringA(("[PivoxActiveX] Session created: " + session.code
                    + ", launching app\n").c_str());
                auto deepLink = L"pivox://auth/delegate/signin?session=" + winrt::to_hstring(session.code);
                ::ShellExecuteW(nullptr, L"open", deepLink.c_str(),
                    nullptr, nullptr, SW_SHOWNORMAL);

                if (auto s = weakThis.get()) {
                    s->SignInText().Text(L"Waiting for sign in...");

                    // Step 3: Poll for completion using a timer.
                    auto dispatcher = s->DispatcherQueue();
                    auto timer = dispatcher.CreateTimer();
                    timer.Interval(std::chrono::seconds(session.pollIntervalSeconds));
                    auto code = session.code;
                    auto polling = std::make_shared<bool>(false);

                    OutputDebugStringA(("[PivoxActiveX] Polling every "
                        + std::to_string(session.pollIntervalSeconds) + "s\n").c_str());

                    timer.Tick([weakThis, timer, code, polling](auto&&, auto&&) {
                        if (*polling) return;  // Previous poll still in flight.
                        *polling = true;
                        pivox::DelegatedAuthClient::PollSession(code,
                            [weakThis, timer, polling](pivox::DelegatedAuthPollResponse poll) {
                                *polling = false;

                                if (poll.result == pivox::PollResult::Ready) {
                                    // Step 4: Sign in with the custom token.
                                    timer.Stop();
                                    OutputDebugStringA("[PivoxActiveX] Got custom token, signing in\n");

                                    if (auto s = weakThis.get()) {
                                        auto dispatcher = s->DispatcherQueue();
                                        pivox::PivoxServices::authService()->signInWithCustomTokenAsync(
                                            poll.customToken,
                                            [dispatcher, weakThis](pivox::AuthResult result) {
                                                dispatcher.TryEnqueue([weakThis, result]() {
                                                    if (auto s = weakThis.get()) {
                                                        s->SetLoading(false);
                                                        if (result.ok()) {
                                                            OutputDebugStringA("[PivoxActiveX] Signed in successfully\n");
                                                        } else {
                                                            OutputDebugStringA(("[PivoxActiveX] Sign-in failed: "
                                                                + result.errorMessage + "\n").c_str());
                                                            s->ShowError(result.errorMessage);
                                                        }
                                                        // AuthStateListener handles navigation.
                                                    }
                                                });
                                            });
                                    }
                                } else if (poll.result == pivox::PollResult::Expired) {
                                    timer.Stop();
                                    OutputDebugStringA("[PivoxActiveX] Session expired\n");
                                    if (auto s = weakThis.get()) {
                                        s->SetLoading(false);
                                        s->ShowError("Sign-in session expired. Please try again.");
                                    }
                                } else if (poll.result == pivox::PollResult::Error) {
                                    timer.Stop();
                                    OutputDebugStringA(("[PivoxActiveX] Poll error: "
                                        + poll.error + "\n").c_str());
                                    if (auto s = weakThis.get()) {
                                        s->SetLoading(false);
                                        s->ShowError("Sign-in failed: " + poll.error);
                                    }
                                }
                                // PollResult::Pending — timer continues polling.
                            });
                    });
                    timer.Start();
                }
            });
    }
}
