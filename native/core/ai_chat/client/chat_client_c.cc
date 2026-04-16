#include "chat_client_c.h"

#include "chat_client.h"

struct PivoxAiChatClient {
  std::unique_ptr<pivox::ai_chat::ChatClient> impl;
};

PivoxAiChatClient* pivox_ai_chat_client_create(const char* endpoint,
                                                const char* auth_token) {
  if (!endpoint) return nullptr;

  auto* client = new (std::nothrow) PivoxAiChatClient();
  if (!client) return nullptr;

  client->impl = std::make_unique<pivox::ai_chat::ChatClient>(
      endpoint, auth_token ? auth_token : "");
  return client;
}

void pivox_ai_chat_client_destroy(PivoxAiChatClient* client) {
  delete client;
}

void pivox_ai_chat_client_set_auth_token(PivoxAiChatClient* client,
                                          const char* auth_token) {
  if (!client) return;
  client->impl->SetAuthToken(auth_token ? auth_token : "");
}

void pivox_ai_chat_client_start_stream(PivoxAiChatClient* client,
                                        const uint8_t* request_bytes,
                                        size_t request_size,
                                        void* ctx,
                                        pivox_ai_chat_on_event on_event,
                                        pivox_ai_chat_on_error on_error,
                                        pivox_ai_chat_on_complete on_complete) {
  if (!client) return;

  client->impl->StartStream(
      request_bytes, request_size,
      [ctx, on_event](const uint8_t* bytes, size_t size) {
        if (on_event) on_event(ctx, bytes, size);
      },
      [ctx, on_error](const std::string& msg) {
        if (on_error) on_error(ctx, msg.c_str());
      },
      [ctx, on_complete]() {
        if (on_complete) on_complete(ctx);
      });
}

void pivox_ai_chat_client_cancel(PivoxAiChatClient* client) {
  if (!client) return;
  client->impl->Cancel();
}

void pivox_ai_chat_unary_call(PivoxAiChatClient* client, const char* method,
                               const uint8_t* request_bytes,
                               size_t request_size, void* ctx,
                               pivox_ai_chat_on_response on_response,
                               pivox_ai_chat_on_rpc_error on_error) {
  if (!client || !method) return;

  client->impl->UnaryCall(
      method, request_bytes, request_size,
      [ctx, on_response](const uint8_t* bytes, size_t size) {
        if (on_response) on_response(ctx, bytes, size);
      },
      [ctx, on_error](const std::string& msg) {
        if (on_error) on_error(ctx, msg.c_str());
      });
}
