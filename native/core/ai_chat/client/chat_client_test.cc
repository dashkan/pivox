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
      client, nullptr,
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
      client, &got_error,
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
      client, nullptr,
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
        client, nullptr,
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
  pivox_ai_chat_client_start_stream(client, nullptr, nullptr, nullptr, nullptr);
  std::this_thread::sleep_for(std::chrono::milliseconds(500));

  pivox_ai_chat_client_cancel(client);
  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, StartStreamNull) {
  // Null client must not crash.
  pivox_ai_chat_client_start_stream(nullptr, nullptr,
      [](void*, const uint8_t*, size_t) {},
      [](void*, const char*) {},
      [](void*) {});
}

// ── Send ────────────────────────────────────────────────────────────

TEST(ChatClientC, SendWithoutStream) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
  ASSERT_NE(client, nullptr);
  uint8_t data[] = {0x01, 0x02, 0x03};
  pivox_ai_chat_client_send(client, data, sizeof(data));  // No-op, no crash.
  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, SendNull) {
  pivox_ai_chat_client_send(nullptr, nullptr, 0);  // Must not crash.
}

TEST(ChatClientC, SendAfterCancel) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
  ASSERT_NE(client, nullptr);

  pivox_ai_chat_client_start_stream(
      client, nullptr,
      [](void*, const uint8_t*, size_t) {},
      [](void*, const char*) {},
      [](void*) {});
  std::this_thread::sleep_for(std::chrono::milliseconds(100));

  pivox_ai_chat_client_cancel(client);

  // Send after cancel — must not crash.
  uint8_t data[] = {0x01};
  pivox_ai_chat_client_send(client, data, sizeof(data));
  pivox_ai_chat_client_destroy(client);
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

// ── Destroy with active stream ──────────────────────────────────────

TEST(ChatClientC, DestroyWithActiveStream) {
  auto* client = pivox_ai_chat_client_create("localhost:99999", "test");
  ASSERT_NE(client, nullptr);

  pivox_ai_chat_client_start_stream(
      client, nullptr,
      [](void*, const uint8_t*, size_t) {},
      [](void*, const char*) {},
      [](void*) {});
  std::this_thread::sleep_for(std::chrono::milliseconds(100));

  // Destroy without explicit cancel — must not crash or leak.
  pivox_ai_chat_client_destroy(client);
}
