#include "token_provider.h"

#include <gtest/gtest.h>

#include <atomic>
#include <chrono>
#include <thread>

#include "token_provider_c.h"

namespace {

using pivox::auth::ClearProvider;
using pivox::auth::FetchToken;
using pivox::auth::RegisterProvider;

// Each test runs with a clean provider slate. RegisterProvider is process-
// global, so one test leaking state into the next would produce confusing
// flakes.
class TokenProviderTest : public ::testing::Test {
 protected:
  void TearDown() override { ClearProvider(); }
};

TEST_F(TokenProviderTest, FetchWithoutProviderCompletesWithNullopt) {
  std::optional<std::string> result = std::string("sentinel");
  bool called = false;
  FetchToken([&](std::optional<std::string> t) {
    result = t;
    called = true;
  });
  EXPECT_TRUE(called);
  EXPECT_FALSE(result.has_value());
}

TEST_F(TokenProviderTest, RegisteredProviderReceivesFetchRequests) {
  RegisterProvider([](auto completion) {
    completion(std::string("token-1"));
  });

  std::optional<std::string> got;
  FetchToken([&](auto t) { got = t; });

  ASSERT_TRUE(got.has_value());
  EXPECT_EQ(*got, "token-1");
}

TEST_F(TokenProviderTest, RegisterReplacesPreviousProvider) {
  RegisterProvider([](auto c) { c(std::string("first")); });
  RegisterProvider([](auto c) { c(std::string("second")); });

  std::optional<std::string> got;
  FetchToken([&](auto t) { got = t; });

  ASSERT_TRUE(got.has_value());
  EXPECT_EQ(*got, "second");
}

TEST_F(TokenProviderTest, AsyncProviderDeliversCompletion) {
  // Simulates the Swift side: provider spawns a Task and completes
  // on a different thread some time later.
  RegisterProvider([](auto completion) {
    std::thread([c = std::move(completion)]() mutable {
      std::this_thread::sleep_for(std::chrono::milliseconds(5));
      c(std::string("async-token"));
    }).detach();
  });

  std::atomic<bool> called{false};
  std::optional<std::string> got;
  FetchToken([&](auto t) {
    got = t;
    called.store(true);
  });

  for (int i = 0; i < 500 && !called.load(); ++i) {
    std::this_thread::sleep_for(std::chrono::milliseconds(1));
  }
  ASSERT_TRUE(called.load());
  ASSERT_TRUE(got.has_value());
  EXPECT_EQ(*got, "async-token");
}

TEST_F(TokenProviderTest, ProviderCanReportFailureAsNullopt) {
  RegisterProvider([](auto c) { c(std::nullopt); });

  std::optional<std::string> got = std::string("sentinel");
  FetchToken([&](auto t) { got = t; });

  EXPECT_FALSE(got.has_value());
}

TEST_F(TokenProviderTest, ClearProviderRestoresNulloptBehavior) {
  RegisterProvider([](auto c) { c(std::string("a")); });
  ClearProvider();

  std::optional<std::string> got = std::string("sentinel");
  FetchToken([&](auto t) { got = t; });

  EXPECT_FALSE(got.has_value());
}

// ── C-ABI entry point (used by platform Swift / WinRT) ─────────────

// A provider function in the shape the platform would supply. Reads a
// global token string so the test can vary it between cases.
namespace {
std::string g_c_abi_token;
bool g_c_abi_provider_was_called = false;

extern "C" void CABIProviderDouble(
    void* completion_ctx, pivox_token_completion_fn completion) {
  g_c_abi_provider_was_called = true;
  completion(completion_ctx, g_c_abi_token.c_str());
}

extern "C" void CABIProviderNull(
    void* completion_ctx, pivox_token_completion_fn completion) {
  g_c_abi_provider_was_called = true;
  completion(completion_ctx, nullptr);
}
}  // namespace

TEST_F(TokenProviderTest, CABIRegistrationRoutesThroughToFetchToken) {
  g_c_abi_token = "from-c-abi";
  g_c_abi_provider_was_called = false;
  pivox_auth_register_provider(&CABIProviderDouble);

  std::optional<std::string> got;
  FetchToken([&](auto t) { got = t; });

  EXPECT_TRUE(g_c_abi_provider_was_called);
  ASSERT_TRUE(got.has_value());
  EXPECT_EQ(*got, "from-c-abi");
}

TEST_F(TokenProviderTest, CABIProviderNullMapsToNullopt) {
  g_c_abi_provider_was_called = false;
  pivox_auth_register_provider(&CABIProviderNull);

  std::optional<std::string> got = std::string("sentinel");
  FetchToken([&](auto t) { got = t; });

  EXPECT_TRUE(g_c_abi_provider_was_called);
  EXPECT_FALSE(got.has_value());
}

TEST_F(TokenProviderTest, CABIRegisterNullClearsProvider) {
  pivox_auth_register_provider(&CABIProviderDouble);
  pivox_auth_register_provider(nullptr);

  std::optional<std::string> got = std::string("sentinel");
  FetchToken([&](auto t) { got = t; });

  EXPECT_FALSE(got.has_value());
}

TEST_F(TokenProviderTest, ConcurrentFetchesAllReceiveCompletion) {
  // Verifies the provider is safe to call from multiple threads
  // simultaneously — the interceptor will do this under RPC fan-out.
  RegisterProvider([](auto c) { c(std::string("concurrent")); });

  constexpr int kN = 20;
  std::atomic<int> completions{0};
  std::vector<std::thread> threads;
  threads.reserve(kN);
  for (int i = 0; i < kN; ++i) {
    threads.emplace_back([&]() {
      FetchToken([&](auto t) {
        if (t.has_value() && *t == "concurrent") completions.fetch_add(1);
      });
    });
  }
  for (auto& t : threads) t.join();
  EXPECT_EQ(completions.load(), kN);
}

}  // namespace
