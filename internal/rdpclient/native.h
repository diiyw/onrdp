#ifndef ONRDP_NATIVE_H
#define ONRDP_NATIVE_H
#include <stdint.h>
#include <stddef.h>
typedef struct onrdp_context onrdp_context;
onrdp_context* onrdp_new(uintptr_t handle, const char* host, int port,
    const char* user, const char* password, const char* domain,
    int width, int height, int clipboard, int console, int color_depth, int reduced_effects);
int onrdp_run(onrdp_context* ctx);
void onrdp_abort(onrdp_context* ctx);
void onrdp_wake(onrdp_context* ctx);
void onrdp_free(onrdp_context* ctx);
const char* onrdp_error(onrdp_context* ctx);
int onrdp_input(onrdp_context* ctx, int kind, uint16_t flags, uint16_t code, int x, int y);
int onrdp_clipboard(onrdp_context* ctx, const uint8_t* text, size_t length);
void onrdp_resize(onrdp_context* ctx, int width, int height);
#endif
