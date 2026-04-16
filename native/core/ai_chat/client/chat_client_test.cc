#include <gtest/gtest.h>

#include <atomic>
#include <chrono>
#include <string>
#include <thread>
#include <vector>

#include "chat_client_c.h"

// These tests validate the C FFI layer and client lifecycle.
// They don't require a running gRPC server — they test creation,
// destruction, and error paths.

TEST(ChatClientC, CreateAndDestroy) {
  // A client pointing at a non-existent server should still create
  // successfully (gRPC channels are lazy).
  auto* client =
      pivox_ai_chat_client_create("localhost:99999", "test-token");
  ASSERT_NE(client, nullptr);
  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, CreateWithNullEndpoint) {
  auto* client = pivox_ai_chat_client_create(nullptr, "test-token");
  EXPECT_EQ(client, nullptr);
}

TEST(ChatClientC, DestroyNull) {
  // Should not crash.
  pivox_ai_chat_client_destroy(nullptr);
}

TEST(ChatClientC, SetAuthToken) {
  auto* client =
      pivox_ai_chat_client_create("localhost:99999", "initial-token");
  ASSERT_NE(client, nullptr);

  // Should not crash or error.
  pivox_ai_chat_client_set_auth_token(client, "refreshed-token");
  pivox_ai_chat_client_set_auth_token(client, "");

  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, CancelWithoutStream) {
  auto* client =
      pivox_ai_chat_client_create("localhost:99999", "test-token");
  ASSERT_NE(client, nullptr);

  // Cancel without starting a stream should not crash.
  pivox_ai_chat_client_cancel(client);

  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, StartStreamNoServer) {
  auto* client =
      pivox_ai_chat_client_create("localhost:99999", "test-token");
  ASSERT_NE(client, nullptr);

  std::atomic<bool> got_error{false};

  pivox_ai_chat_client_start_stream(
      client, &got_error,
      // on_event
      [](void* /*ctx*/, const uint8_t* /*bytes*/, size_t /*size*/) {},
      // on_error
      [](void* ctx, const char* /*msg*/) {
        static_cast<std::atomic<bool>*>(ctx)->store(true);
      },
      // on_complete
      [](void* ctx) {
        // Use the same context for simplicity — check error flag.
      });

  // Give gRPC a moment to fail the connection.
  std::this_thread::sleep_for(std::chrono::milliseconds(500));

  // Either an error or the stream finishes — the client shouldn't hang.
  pivox_ai_chat_client_cancel(client);
  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, SendWithoutStream) {
  auto* client =
      pivox_ai_chat_client_create("localhost:99999", "test-token");
  ASSERT_NE(client, nullptr);

  // Send without a stream should not crash.
  uint8_t data[] = {0x01, 0x02, 0x03};
  pivox_ai_chat_client_send(client, data, sizeof(data));

  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, UnaryCallNoServerFiresError) {
  auto* client =
      pivox_ai_chat_client_create("localhost:99999", "test-token");
  ASSERT_NE(client, nullptr);

  std::atomic<bool> got_error{false};

  uint8_t req[] = {0x0a, 0x05, 0x74, 0x65, 0x73, 0x74};  // dummy bytes

  pivox_ai_chat_unary_call(
      client,
      "/pivox.ai.v1.AiChat/ListConversations",
      req, sizeof(req),
      &got_error,
      // on_response — should NOT be called
      [](void* ctx, const uint8_t* /*bytes*/, size_t /*size*/) {
        // If this fires, the test is wrong.
        FAIL() << "on_response should not be called when no server is running";
      },
      // on_error — MUST be called
      [](void* ctx, const char* msg) {
        static_cast<std::atomic<bool>*>(ctx)->store(true);
      });

  // Wait for the error callback (with deadline from the 30s context timeout,
  // but the connection failure should be fast).
  for (int i = 0; i < 100 && !got_error.load(); i++) {
    std::this_thread::sleep_for(std::chrono::milliseconds(100));
  }

  EXPECT_TRUE(got_error.load()) << "Error callback must fire when server is unreachable";
  pivox_ai_chat_client_destroy(client);
}

TEST(ChatClientC, UnaryCallWithNullClient) {
  // Should not crash.
  uint8_t req[] = {0x01};
  pivox_ai_chat_unary_call(
      nullptr, "/test/Method", req, sizeof(req), nullptr,
      [](void*, const uint8_t*, size_t) {},
      [](void*, const char*) {});
}

TEST(ChatClientC, UnaryCallWithNullMethod) {
  auto* client =
      pivox_ai_chat_client_create("localhost:99999", "test-token");
  ASSERT_NE(client, nullptr);

  // Null method should not crash.
  uint8_t req[] = {0x01};
  pivox_ai_chat_unary_call(
      client, nullptr, req, sizeof(req), nullptr,
      [](void*, const uint8_t*, size_t) {},
      [](void*, const char*) {});

  pivox_ai_chat_client_destroy(client);
}
