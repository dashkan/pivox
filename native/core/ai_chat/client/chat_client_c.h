#ifndef PIVOX_AI_CHAT_CLIENT_C_H
#define PIVOX_AI_CHAT_CLIENT_C_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct PivoxAiChatClient PivoxAiChatClient;

// Creates a new chat client connected to the given gRPC endpoint.
// Returns NULL on failure.
PivoxAiChatClient* pivox_ai_chat_client_create(const char* endpoint,
                                                const char* auth_token);

// Destroys the client and releases all resources.
void pivox_ai_chat_client_destroy(PivoxAiChatClient* client);

// Updates the auth token (e.g. after Firebase refresh).
void pivox_ai_chat_client_set_auth_token(PivoxAiChatClient* client,
                                          const char* auth_token);

// Callback types for stream events.
typedef void (*pivox_ai_chat_on_event)(void* ctx, const uint8_t* bytes,
                                       size_t size);
typedef void (*pivox_ai_chat_on_error)(void* ctx, const char* msg);
typedef void (*pivox_ai_chat_on_complete)(void* ctx);

// Opens a bidi stream. Callbacks fire on the main queue (macOS) or
// dispatcher queue (Windows). The ctx pointer is passed through to
// every callback — caller manages its lifetime.
void pivox_ai_chat_client_start_stream(PivoxAiChatClient* client, void* ctx,
                                        pivox_ai_chat_on_event on_event,
                                        pivox_ai_chat_on_error on_error,
                                        pivox_ai_chat_on_complete on_complete);

// Sends a serialized ClientEvent to the server.
void pivox_ai_chat_client_send(PivoxAiChatClient* client,
                                const uint8_t* bytes, size_t size);

// Cancels the in-flight stream.
void pivox_ai_chat_client_cancel(PivoxAiChatClient* client);

// --- Unary RPC helpers ---
// Each takes a serialized request, calls the RPC, and delivers the
// serialized response (or error) via callback.

typedef void (*pivox_ai_chat_on_response)(void* ctx, const uint8_t* bytes,
                                           size_t size);
typedef void (*pivox_ai_chat_on_rpc_error)(void* ctx, const char* msg);

void pivox_ai_chat_unary_call(PivoxAiChatClient* client, const char* method,
                               const uint8_t* request_bytes,
                               size_t request_size, void* ctx,
                               pivox_ai_chat_on_response on_response,
                               pivox_ai_chat_on_rpc_error on_error);

#ifdef __cplusplus
}
#endif

#endif // PIVOX_AI_CHAT_CLIENT_C_H
