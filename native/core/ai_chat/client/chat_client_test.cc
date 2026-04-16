#include <gtest/gtest.h>

#include <atomic>
#include <chrono>
#include <string>
#include <thread>
#include <vector>

#include "chat_client_c.h"

// ── Creation / destruction ──────────────────────────────────────────

TEST(ChatClientC, CreateAndDestroy) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test-token");
  ASSERT_NE(client, nullptr);
  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, CreateWithNullEndpoint) {
  EXPECT_EQ(pivox_ai_chat_client_create(nullptr, "test-token"), nullptr);
}

TEST(ChatClientC, CreateWithNullToken) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", nullptr);
  ASSERT_NE(client, nullptr);  // Empty token is valid — unauthenticated.
  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, DestroyNull) {
  pivox_ai_chat_client_destroy(nullptr);  // Must not crash.
}

// ── Auth token ──────────────────────────────────────────────────────

TEST(ChatClientC, SetAuthToken) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "initial");
  ASSERT_NE(client, nullptr);
  pivox_ai_chat_client_set_auth_token(client, "refreshed");
  pivox_ai_chat_client_set_auth_token(client, "");
  pivox_ai_chat_client_set_auth_token(client, nullptr);  // Null = clear.
  pivox_ai_chat_client_destroy(client);
}

// Dummy request bytes for stream calls.
static uint8_t dummy_req[] = {0x0a, 0x01, 0x00};

// ── Cancel ──────────────────────────────────────────────────────────

TEST(ChatClientC, CancelWithoutStream) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
  ASSERT_NE(client, nullptr);
  pivox_ai_chat_client_cancel(client);  // No-op, must not crash.
  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, CancelNull) {
  pivox_ai_chat_client_cancel(nullptr);  // Must not crash.
}

TEST(ChatClientC, DoubleCancelDoesNotCrash) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
  ASSERT_NE(client, nullptr);
  // Start a stream, then cancel twice.
  pivox_ai_chat_client_start_stream(
      client, dummy_req, sizeof(dummy_req), nullptr,
      [](void*, const uint8_t*, size_t) {},
      [](void*, const char*) {},
      [](void*) {});
  std::this_thread::sleep_for(std::chrono::milliseconds(100));
  pivox_ai_chat_client_cancel(client);
  pivox_ai_chat_client_cancel(client);  // Second cancel must not crash.
  pivox_ai_chat_client_destroy(client);
}

// ── Bidi stream ─────────────────────────────────────────────────────

TEST(ChatClientC, StreamNoServerFiresError) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
  ASSERT_NE(client, nullptr);

  std::atomic<bool> got_error{false};

  pivox_ai_chat_client_start_stream(
      client, dummy_req, sizeof(dummy_req), &got_error,
      [](void*, const uint8_t*, size_t) {},
      [](void* ctx, const char* msg) {
        static_cast<std::atomic<bool>*>(ctx)->store(true);
      },
      [](void* ctx) {});

  // Wait for gRPC to detect the unreachable server.
  for (int i = 0; i < 50 && !got_error.load(); i++) {
    std::this_thread::sleep_for(std::chrono::milliseconds(100));
  }

  EXPECT_TRUE(got_error.load()) << "on_error must fire when server is unreachable";
  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, StreamCancelIsSafe) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
  ASSERT_NE(client, nullptr);

  pivox_ai_chat_client_start_stream(
      client, dummy_req, sizeof(dummy_req), nullptr,
      [](void*, const uint8_t*, size_t) {},
      [](void*, const char*) {},
      [](void*) {});

  std::this_thread::sleep_for(std::chrono::milliseconds(100));
  pivox_ai_chat_client_cancel(client);
  // Must not crash or hang.
  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, StreamStartNewReplacesPrevious) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
  ASSERT_NE(client, nullptr);

  auto start_stream = [&]() {
    pivox_ai_chat_client_start_stream(
      client, dummy_req, sizeof(dummy_req), nullptr,
        [](void*, const uint8_t*, size_t) {},
        [](void*, const char*) {},
        [](void*) {});
  };

  start_stream();
  std::this_thread::sleep_for(std::chrono::milliseconds(100));
  start_stream();  // Replaces the first stream.
  std::this_thread::sleep_for(std::chrono::milliseconds(100));

  // Must not crash or hang.
  pivox_ai_chat_client_cancel(client);
  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, StreamNullCallbacks) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
  ASSERT_NE(client, nullptr);

  // Null callbacks must not crash.
  pivox_ai_chat_client_start_stream(client, dummy_req, sizeof(dummy_req), nullptr, nullptr, nullptr, nullptr);
  std::this_thread::sleep_for(std::chrono::milliseconds(500));

  pivox_ai_chat_client_cancel(client);
  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, StartStreamNull) {
  // Null client must not crash.
  pivox_ai_chat_client_start_stream(nullptr, nullptr, 0, nullptr,
      [](void*, const uint8_t*, size_t) {},
      [](void*, const char*) {},
      [](void*) {});
}




// ── Unary RPC ───────────────────────────────────────────────────────

TEST(ChatClientC, UnaryCallNoServerFiresError) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
  ASSERT_NE(client, nullptr);

  std::atomic<bool> got_error{false};
  uint8_t req[] = {0x0a, 0x05, 0x74, 0x65, 0x73, 0x74};

  pivox_ai_chat_unary_call(
      client, "/pivox.ai.v1.AiChat/ListConversations",
      req, sizeof(req), &got_error,
      [](void*, const uint8_t*, size_t) {
        FAIL() << "on_response must not fire when server is unreachable";
      },
      [](void* ctx, const char*) {
        static_cast<std::atomic<bool>*>(ctx)->store(true);
      });

  for (int i = 0; i < 100 && !got_error.load(); i++) {
    std::this_thread::sleep_for(std::chrono::milliseconds(100));
  }

  EXPECT_TRUE(got_error.load());
  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, UnaryCallWithNullClient) {
  uint8_t req[] = {0x01};
  pivox_ai_chat_unary_call(nullptr, "/test/Method", req, sizeof(req), nullptr,
      [](void*, const uint8_t*, size_t) {},
      [](void*, const char*) {});
}

TEST(ChatClientC, UnaryCallWithNullMethod) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
  ASSERT_NE(client, nullptr);
  uint8_t req[] = {0x01};
  pivox_ai_chat_unary_call(client, nullptr, req, sizeof(req), nullptr,
      [](void*, const uint8_t*, size_t) {},
      [](void*, const char*) {});
  pivox_ai_chat_client_destroy(client);
}

// ── Retry exhaustion ────────────────────────────────────────────────

TEST(ChatClientC, RetryExhaustedFiresError) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
  ASSERT_NE(client, nullptr);

  std::atomic<int> error_count{0};

  pivox_ai_chat_client_start_stream(
      client, dummy_req, sizeof(dummy_req), &error_count,
      [](void*, const uint8_t*, size_t) {},
      [](void* ctx, const char*) {
        static_cast<std::atomic<int>*>(ctx)->fetch_add(1);
      },
      [](void*) {});

  for (int i = 0; i < 100 && error_count.load() == 0; i++) {
    std::this_thread::sleep_for(std::chrono::milliseconds(100));
  }

  EXPECT_EQ(error_count.load(), 1)
      << "on_error must fire exactly once after retries exhausted";
  pivox_ai_chat_client_destroy(client);
}

// ── Unary call orphan on destroy ────────────────────────────────────

TEST(ChatClientC, DestroyDuringUnaryCall) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
  ASSERT_NE(client, nullptr);

  uint8_t req[] = {0x0a, 0x05, 0x74, 0x65, 0x73, 0x74};
  pivox_ai_chat_unary_call(
      client, "/pivox.ai.v1.AiChat/ListConversations",
      req, sizeof(req), nullptr,
      [](void*, const uint8_t*, size_t) {},
      [](void*, const char*) {});

  std::this_thread::sleep_for(std::chrono::milliseconds(10));
  pivox_ai_chat_client_destroy(client);
}

// ── Destroy with active stream ──────────────────────────────────────

TEST(ChatClientC, DestroyWithActiveStream) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
  ASSERT_NE(client, nullptr);

  pivox_ai_chat_client_start_stream(
      client, dummy_req, sizeof(dummy_req), nullptr,
      [](void*, const uint8_t*, size_t) {},
      [](void*, const char*) {},
      [](void*) {});
  std::this_thread::sleep_for(std::chrono::milliseconds(100));

  // Destroy without explicit cancel — must not crash or leak.
  pivox_ai_chat_client_destroy(client);
}

// ── Retry thread teardown safety ────────────────────────────────────

// When a stream fails transiently, ChatClient spawns a retry_thread_ that
// sleeps on cv_ for up to 500ms * 2^(retry-1). Destroy must notify the cv
// and join the retry thread promptly rather than waiting the full delay.
TEST(ChatClientC, ShutdownInterruptsRetrySleep) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
  ASSERT_NE(client, nullptr);

  std::atomic<int> errors{0};
  pivox_ai_chat_client_start_stream(
      client, dummy_req, sizeof(dummy_req), &errors,
      [](void*, const uint8_t*, size_t) {},
      [](void* ctx, const char*) {
        static_cast<std::atomic<int>*>(ctx)->fetch_add(1);
      },
      [](void*) {});

  // Let gRPC surface the first connection failure so the retry thread
  // enters its wait_for sleep. The first retry delay is ~500ms, so we
  // have a comfortable window where the thread is guaranteed parked.
  std::this_thread::sleep_for(std::chrono::milliseconds(200));

  auto t0 = std::chrono::steady_clock::now();
  pivox_ai_chat_client_destroy(client);
  auto elapsed = std::chrono::steady_clock::now() - t0;

  // Destroy must not wait the full retry delay. If cv_.notify_all() fails
  // to wake the parked thread, destroy would block for >=500ms.
  EXPECT_LT(elapsed, std::chrono::milliseconds(400))
      << "destroy must interrupt retry_thread_ sleep via cv.notify_all()";
}

// ── Cancel/destroy race stress ──────────────────────────────────────

// Exercises the path where the reactor's OnDone and ChatClient::Shutdown
// race via cb_mu_ + mu_. Any lock-ordering bug or missed null-check on
// callbacks surfaces as a crash within the 50 iterations below.
TEST(ChatClientC, CancelDestroyRaceStress) {
  for (int i = 0; i < 50; i++) {
    auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
    ASSERT_NE(client, nullptr);

    pivox_ai_chat_client_start_stream(
        client, dummy_req, sizeof(dummy_req), nullptr,
        [](void*, const uint8_t*, size_t) {},
        [](void*, const char*) {},
        [](void*) {});

    // Cancel from another thread at an unpredictable moment relative to
    // the gRPC failure path firing OnDone.
    std::thread canceler([client]() {
      std::this_thread::sleep_for(std::chrono::microseconds(500));
      pivox_ai_chat_client_cancel(client);
    });

    std::this_thread::sleep_for(std::chrono::microseconds(300));
    canceler.join();
    pivox_ai_chat_client_destroy(client);
  }
}
