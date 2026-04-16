#ifndef PIVOX_AI_CHAT_CLIENT_C_H
#define PIVOX_AI_CHAT_CLIENT_C_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct PivoxAiChatClient PivoxAiChatClient;

PivoxAiChatClient* pivox_ai_chat_client_create(const char* endpoint,
                                                const char* auth_token);
void pivox_ai_chat_client_destroy(PivoxAiChatClient* client);
void pivox_ai_chat_client_set_auth_token(PivoxAiChatClient* client,
                                          const char* auth_token);

// Callback types.
typedef void (*pivox_ai_chat_on_event)(void* ctx, const uint8_t* bytes,
                                       size_t size);
typedef void (*pivox_ai_chat_on_error)(void* ctx, const char* msg);
typedef void (*pivox_ai_chat_on_complete)(void* ctx);

// Opens a server-streaming call. request_bytes is the serialized
// ClientEvent. Responses stream back via on_event.
void pivox_ai_chat_client_start_stream(PivoxAiChatClient* client,
                                        const uint8_t* request_bytes,
                                        size_t request_size,
                                        void* ctx,
                                        pivox_ai_chat_on_event on_event,
                                        pivox_ai_chat_on_error on_error,
                                        pivox_ai_chat_on_complete on_complete);

// Cancels the in-flight stream.
void pivox_ai_chat_client_cancel(PivoxAiChatClient* client);

// Unary RPC.
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
