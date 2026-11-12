//go:build cgo

#include "native.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdatomic.h>
#include <freerdp/freerdp.h>
#include <freerdp/client.h>
#include <freerdp/client/cmdline.h>
#include <freerdp/client/channels.h>
#include <freerdp/client/cliprdr.h>
#include <freerdp/client/disp.h>
#include <freerdp/gdi/gdi.h>
#include <freerdp/input.h>
#include <freerdp/settings.h>
#include <freerdp/event.h>
#include <freerdp/error.h>
#include <winpr/synch.h>
#include <winpr/sysinfo.h>
#include <winpr/clipboard.h>

extern void goRDPFrame(uintptr_t, void*, int, int, int, int, int, int, int);
extern void goRDPReady(uintptr_t);
extern int goRDPInput(uintptr_t, onrdp_context*);
extern void goRDPClipboard(uintptr_t, void*, int);

struct onrdp_context {
    rdpContext base; /* FreeRDP owns this extended context. */
    uintptr_t handle;
    atomic_bool cancelled;
    HANDLE input_event;
    CliprdrClientContext* clip;
    CRITICAL_SECTION clip_lock;
    BYTE* text;
    size_t text_length;
    BOOL clip_ready;
    DispClientContext* disp;
    CRITICAL_SECTION disp_lock;
    UINT64 max_area;
    UINT64 next_layout;
    UINT32 sent_width, sent_height;
    BOOL disp_ready;
    char error[256];
};

static BOOL begin_paint(rdpContext* base) {
    base->gdi->primary->hdc->hwnd->invalid->null = TRUE;
    base->gdi->primary->hdc->hwnd->ninvalid = 0;
    return TRUE;
}
static void publish(onrdp_context* ctx, int x, int y, int width, int height) {
    rdpGdi* g = ctx->base.gdi;
    if (g && g->primary_buffer)
        goRDPFrame(ctx->handle, g->primary_buffer, g->width, g->height,
                   g->stride, x, y, width, height);
}
static BOOL end_paint(rdpContext* base) {
    HGDI_RGN r = base->gdi->primary->hdc->hwnd->invalid;
    if (!r->null)
        publish((onrdp_context*)base, r->x, r->y, r->w, r->h);
    return TRUE;
}
static BOOL resize(rdpContext* base) {
    UINT32 w = freerdp_settings_get_uint32(base->settings, FreeRDP_DesktopWidth);
    UINT32 h = freerdp_settings_get_uint32(base->settings, FreeRDP_DesktopHeight);
    if (!w || !h || w > 8192 || h > 8192 || !gdi_resize(base->gdi, w, h))
        return FALSE;
    publish((onrdp_context*)base, 0, 0, w, h);
    return TRUE;
}

/* Clipboard bytes are UTF-16LE including their terminating NUL. Only text is
 * advertised; file, image and device redirection remain disabled. */
static UINT announce(onrdp_context* ctx) {
    if (!ctx->clip || !ctx->clip_ready) return CHANNEL_RC_OK;
    CLIPRDR_FORMAT format = { .formatId = CF_UNICODETEXT };
    CLIPRDR_FORMAT_LIST list = { 0 };
    list.common.msgType = CB_FORMAT_LIST;
    list.numFormats = ctx->text ? 1 : 0;
    list.formats = &format;
    return ctx->clip->ClientFormatList(ctx->clip, &list);
}
static UINT clip_ready(CliprdrClientContext* clip, const CLIPRDR_MONITOR_READY* ready) {
    (void)ready;
    onrdp_context* ctx = clip->custom;
    CLIPRDR_GENERAL_CAPABILITY_SET general = { 0 };
    general.capabilitySetType = CB_CAPSTYPE_GENERAL;
    general.capabilitySetLength = CB_CAPSTYPE_GENERAL_LEN;
    general.version = CB_CAPS_VERSION_2;
    general.generalFlags = CB_USE_LONG_FORMAT_NAMES;
    CLIPRDR_CAPABILITIES caps = { 0 };
    caps.cCapabilitiesSets = 1;
    caps.capabilitySets = (CLIPRDR_CAPABILITY_SET*)&general;
    UINT status = clip->ClientCapabilities(clip, &caps);
    if (status != CHANNEL_RC_OK) return status;
    EnterCriticalSection(&ctx->clip_lock);
    ctx->clip_ready = TRUE;
    status = announce(ctx);
    LeaveCriticalSection(&ctx->clip_lock);
    return status;
}
static UINT clip_formats(CliprdrClientContext* clip, const CLIPRDR_FORMAT_LIST* list) {
    CLIPRDR_FORMAT_LIST_RESPONSE response = { 0 };
    response.common.msgFlags = CB_RESPONSE_OK;
    UINT status = clip->ClientFormatListResponse(clip, &response);
    if (status != CHANNEL_RC_OK) return status;
    for (UINT32 n = 0; n < list->numFormats; n++) {
        if (list->formats[n].formatId == CF_UNICODETEXT) {
            CLIPRDR_FORMAT_DATA_REQUEST request = { .requestedFormatId = CF_UNICODETEXT };
            return clip->ClientFormatDataRequest(clip, &request);
        }
    }
    return CHANNEL_RC_OK;
}
static UINT clip_request(CliprdrClientContext* clip, const CLIPRDR_FORMAT_DATA_REQUEST* request) {
    onrdp_context* ctx = clip->custom;
    CLIPRDR_FORMAT_DATA_RESPONSE response = { 0 };
    EnterCriticalSection(&ctx->clip_lock);
    if (request->requestedFormatId == CF_UNICODETEXT && ctx->text) {
        response.common.msgFlags = CB_RESPONSE_OK;
        response.common.dataLen = (UINT32)ctx->text_length;
        response.requestedFormatData = ctx->text;
    } else response.common.msgFlags = CB_RESPONSE_FAIL;
    UINT status = clip->ClientFormatDataResponse(clip, &response);
    LeaveCriticalSection(&ctx->clip_lock);
    return status;
}
static UINT clip_response(CliprdrClientContext* clip, const CLIPRDR_FORMAT_DATA_RESPONSE* response) {
    onrdp_context* ctx = clip->custom;
    if ((response->common.msgFlags & CB_RESPONSE_OK) && response->requestedFormatData &&
        response->common.dataLen <= 16 * 1024 * 1024 && !(response->common.dataLen & 1))
        goRDPClipboard(ctx->handle, (void*)response->requestedFormatData, response->common.dataLen);
    return CHANNEL_RC_OK;
}
static UINT clip_capabilities(CliprdrClientContext* clip, const CLIPRDR_CAPABILITIES* caps) {
    (void)clip; (void)caps; return CHANNEL_RC_OK;
}
static UINT clip_list_response(CliprdrClientContext* clip, const CLIPRDR_FORMAT_LIST_RESPONSE* response) {
    (void)clip; (void)response; return CHANNEL_RC_OK;
}

static UINT display_caps(DispClientContext* disp, UINT32 monitors, UINT32 factor_a, UINT32 factor_b) {
    onrdp_context* ctx = disp->custom;
    const UINT64 limit = 8192ULL * 8192;
    /* Saturate before the third multiply: server-provided UINT32 values
     * must not overflow and accidentally disable the size limit. */
    UINT64 area = (UINT64)factor_a * factor_b;
    area = area > limit ? limit : area;
    area *= monitors;
    EnterCriticalSection(&ctx->disp_lock);
    ctx->max_area = area > limit ? limit : area;
    ctx->disp_ready = ctx->max_area >= 200 * 200;
    ctx->sent_width = ctx->sent_height = 0;
    /* Windows can ignore a layout sent immediately after capabilities. */
    ctx->next_layout = GetTickCount64() + 500;
    LeaveCriticalSection(&ctx->disp_lock);
    return CHANNEL_RC_OK;
}

void onrdp_resize(onrdp_context* ctx, int width, int height) {
    if (width < 200 || height < 200 || width > 8192 || height > 8192) return;
    EnterCriticalSection(&ctx->disp_lock);
    UINT64 now = GetTickCount64();
    if (!ctx->disp || !ctx->disp_ready || now < ctx->next_layout) goto done;
    UINT32 w = (UINT32)width & ~1U;
    UINT32 h = (UINT32)height;
    if ((UINT64)w * h > ctx->max_area) {
        /* Find the largest fitting width at the requested aspect ratio.
         * Width must be even; both dimensions must be at least 200. */
        UINT32 low = 0, high = w / 2;
        while (low < high) {
            UINT32 mid = (low + high + 1) / 2;
            UINT32 candidate_h = (UINT32)((UINT64)h * (mid * 2) / w);
            if ((UINT64)(mid * 2) * candidate_h <= ctx->max_area) low = mid;
            else high = mid - 1;
        }
        h = (UINT32)((UINT64)h * (low * 2) / w);
        w = low * 2;
    }
    if (w < 200 || h < 200 || (ctx->sent_width == w && ctx->sent_height == h)) goto done;
    DISPLAY_CONTROL_MONITOR_LAYOUT layout = { 0 };
    layout.Flags = DISPLAY_CONTROL_MONITOR_PRIMARY;
    layout.Width = w;
    layout.Height = h;
    layout.DesktopScaleFactor = 100;
    layout.DeviceScaleFactor = 100;
    UINT result = ctx->disp->SendMonitorLayout(ctx->disp, 1, &layout);
    ctx->next_layout = now + 500;
    if (result == CHANNEL_RC_OK) {
        ctx->sent_width = w;
        ctx->sent_height = h;
    }
done:
    LeaveCriticalSection(&ctx->disp_lock);
}

static void channel_connected(void* context, const ChannelConnectedEventArgs* event) {
    onrdp_context* ctx = context;
    if (!strcmp(event->name, DISP_DVC_CHANNEL_NAME)) {
        EnterCriticalSection(&ctx->disp_lock);
        ctx->disp = event->pInterface;
        ctx->disp->custom = ctx;
        ctx->disp->DisplayControlCaps = display_caps;
        ctx->disp_ready = FALSE;
        LeaveCriticalSection(&ctx->disp_lock);
        return;
    }
    if (strcmp(event->name, CLIPRDR_SVC_CHANNEL_NAME)) return;
    EnterCriticalSection(&ctx->clip_lock);
    CliprdrClientContext* clip = event->pInterface;
    ctx->clip = clip;
    clip->custom = ctx;
    clip->MonitorReady = clip_ready;
    clip->ServerCapabilities = clip_capabilities;
    clip->ServerFormatList = clip_formats;
    clip->ServerFormatListResponse = clip_list_response;
    clip->ServerFormatDataRequest = clip_request;
    clip->ServerFormatDataResponse = clip_response;
    LeaveCriticalSection(&ctx->clip_lock);
}
static void channel_disconnected(void* context, const ChannelDisconnectedEventArgs* event) {
    onrdp_context* ctx = context;
    if (!strcmp(event->name, DISP_DVC_CHANNEL_NAME)) {
        EnterCriticalSection(&ctx->disp_lock);
        ctx->disp = NULL;
        ctx->disp_ready = FALSE;
        LeaveCriticalSection(&ctx->disp_lock);
        return;
    }
    if (strcmp(event->name, CLIPRDR_SVC_CHANNEL_NAME)) return;
    EnterCriticalSection(&ctx->clip_lock);
    ctx->clip = NULL;
    ctx->clip_ready = FALSE;
    LeaveCriticalSection(&ctx->clip_lock);
}
static BOOL pre_connect(freerdp* instance) {
    rdpContext* base = instance->context;
    if (atomic_load(&((onrdp_context*)base)->cancelled)) return FALSE;
    if (PubSub_SubscribeChannelConnected(base->pubSub, channel_connected) < 0 ||
        PubSub_SubscribeChannelDisconnected(base->pubSub, channel_disconnected) < 0)
        return FALSE;
    return TRUE;
}
static BOOL load_channels(freerdp* instance) {
    return freerdp_client_load_addins(instance->context->channels, instance->context->settings);
}
static BOOL post_connect(freerdp* instance) {
    if (!gdi_init(instance, PIXEL_FORMAT_RGBA32)) return FALSE;
    rdpContext* base = instance->context;
    base->update->BeginPaint = begin_paint;
    base->update->EndPaint = end_paint;
    base->update->DesktopResize = resize;
    publish((onrdp_context*)base, 0, 0, base->gdi->width, base->gdi->height);
    return TRUE;
}
static void post_disconnect(freerdp* instance) {
    rdpContext* base = instance->context;
    PubSub_UnsubscribeChannelConnected(base->pubSub, channel_connected);
    PubSub_UnsubscribeChannelDisconnected(base->pubSub, channel_disconnected);
    gdi_free(instance);
}

onrdp_context* onrdp_new(uintptr_t handle, const char* host, int port,
    const char* user, const char* password, const char* domain,
    int width, int height, int clipboard, int console, int color_depth, int reduced_effects) {
    freerdp* instance = freerdp_new();
    if (!instance) return NULL;
    instance->ContextSize = sizeof(onrdp_context);
    if (!freerdp_context_new(instance)) { freerdp_free(instance); return NULL; }
    onrdp_context* ctx = (onrdp_context*)instance->context;
    ctx->handle = handle;
    atomic_init(&ctx->cancelled, FALSE);
    InitializeCriticalSection(&ctx->clip_lock);
    InitializeCriticalSection(&ctx->disp_lock);
    ctx->input_event = CreateEvent(NULL, TRUE, FALSE, NULL);
    if (!ctx->input_event) goto fail;
    instance->PreConnect = pre_connect;
    instance->LoadChannels = load_channels;
    instance->PostConnect = post_connect;
    instance->PostDisconnect = post_disconnect;
    rdpSettings* s = ctx->base.settings;
#define SET(kind, name, value) if (!freerdp_settings_set_##kind(s, FreeRDP_##name, value)) goto fail
    SET(string, ServerHostname, host);
    SET(uint32, ServerPort, port);
    SET(string, Username, user);
    SET(string, Password, password);
    SET(string, Domain, domain);
    SET(uint32, DesktopWidth, width);
    SET(uint32, DesktopHeight, height);
    SET(uint32, ColorDepth, color_depth);
    SET(bool, CompressionEnabled, TRUE);
    SET(uint32, PerformanceFlags, reduced_effects ?
        PERF_DISABLE_WALLPAPER | PERF_DISABLE_FULLWINDOWDRAG |
        PERF_DISABLE_MENUANIMATIONS | PERF_DISABLE_CURSOR_SHADOW : PERF_FLAG_NONE);
    SET(bool, DisableWallpaper, reduced_effects);
    SET(bool, DisableFullWindowDrag, reduced_effects);
    SET(bool, DisableMenuAnims, reduced_effects);
    SET(bool, DisableCursorShadow, reduced_effects);
    SET(bool, AllowFontSmoothing, color_depth == 32);
    SET(uint32, TcpConnectTimeout, 10000);
    SET(bool, IgnoreCertificate, TRUE); /* Same certificate policy as the former backend. */
    SET(bool, SupportGraphicsPipeline, FALSE);
    SET(bool, SupportDisplayControl, TRUE);
    SET(bool, DynamicResolutionUpdate, TRUE);
    SET(bool, RedirectClipboard, clipboard);
    SET(bool, ConsoleSession, console);
    SET(bool, AudioPlayback, FALSE);
    SET(bool, AudioCapture, FALSE);
    SET(bool, RedirectDrives, FALSE);
    SET(bool, RedirectPrinters, FALSE);
    SET(bool, RedirectSmartCards, FALSE);
    SET(bool, RedirectSerialPorts, FALSE);
    SET(bool, RedirectParallelPorts, FALSE);
#undef SET
    return ctx;
fail:
    onrdp_free(ctx);
    return NULL;
}
int onrdp_run(onrdp_context* ctx) {
    freerdp* instance = ctx->base.instance;
    if (freerdp_connect(instance)) {
        goRDPReady(ctx->handle);
        while (!freerdp_shall_disconnect_context(&ctx->base)) {
            /* Reset before draining input so an enqueue during the drain keeps
             * the wait signalled. Input never waits for the next 16 ms poll. */
            ResetEvent(ctx->input_event);
            if (!goRDPInput(ctx->handle, ctx)) break;
            HANDLE events[MAXIMUM_WAIT_OBJECTS];
            DWORD n = freerdp_get_event_handles(&ctx->base, events, MAXIMUM_WAIT_OBJECTS - 1);
            if (!n || n >= MAXIMUM_WAIT_OBJECTS) break;
            events[n++] = ctx->input_event;
            if (WaitForMultipleObjects(n, events, FALSE, 16) == WAIT_FAILED ||
                !freerdp_check_event_handles(&ctx->base)) break;
        }
    }
    UINT32 code = freerdp_get_last_error(&ctx->base);
    UINT32 info = freerdp_error_info(instance);
    snprintf(ctx->error, sizeof(ctx->error), "%s (0x%08x)",
        info ? freerdp_get_error_info_string(info) : (code ? freerdp_get_last_error_string(code) : "Remote session disconnected"),
        info ? info : code);
    freerdp_disconnect(instance);
    /* Deliberate logoff, session takeover and authentication failures must
     * remain visible instead of reconnecting and stealing the session back. */
    return (!info || info == ERRINFO_PEER_DISCONNECTED) &&
        (code == FREERDP_ERROR_SUCCESS || code == FREERDP_ERROR_CONNECT_TRANSPORT_FAILED ||
         code == FREERDP_ERROR_CONNECT_FAILED || code == FREERDP_ERROR_DNS_NAME_NOT_FOUND);
}
void onrdp_wake(onrdp_context* ctx) {
    SetEvent(ctx->input_event);
}
void onrdp_abort(onrdp_context* ctx) {
    atomic_store(&ctx->cancelled, TRUE);
    freerdp_abort_connect_context(&ctx->base);
}
const char* onrdp_error(onrdp_context* ctx) { return ctx->error; }
void onrdp_free(onrdp_context* ctx) {
    if (!ctx) return;
    freerdp* instance = ctx->base.instance;
    free(ctx->text);
    if (ctx->input_event) CloseHandle(ctx->input_event);
    DeleteCriticalSection(&ctx->clip_lock);
    DeleteCriticalSection(&ctx->disp_lock);
    freerdp_context_free(instance);
    freerdp_free(instance);
}
int onrdp_input(onrdp_context* ctx, int kind, uint16_t flags, uint16_t code, int x, int y) {
    rdpInput* input = ctx->base.input;
    switch (kind) {
        case 1: return freerdp_input_send_keyboard_event(input, flags, (UINT8)code);
        case 2: return freerdp_input_send_unicode_keyboard_event(input, flags, code);
        case 3: return freerdp_input_send_mouse_event(input, flags, x, y);
        default: return FALSE;
    }
}
int onrdp_clipboard(onrdp_context* ctx, const uint8_t* text, size_t length) {
    if (length > 16 * 1024 * 1024) return FALSE;
    BYTE* copy = calloc(1, length + 2);
    if (!copy) return FALSE;
    if (length) memcpy(copy, text, length);
    EnterCriticalSection(&ctx->clip_lock);
    free(ctx->text);
    ctx->text = copy;
    ctx->text_length = length + 2;
    UINT status = announce(ctx);
    LeaveCriticalSection(&ctx->clip_lock);
    return status == CHANNEL_RC_OK;
}

void onrdp_init(void) {
    freerdp_register_addin_provider(freerdp_channels_load_static_addin_entry, 0);
}
