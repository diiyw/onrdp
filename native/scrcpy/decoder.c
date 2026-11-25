// The caller owns the window and input. Each decoder belongs to one video worker.
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <libavcodec/avcodec.h>
#include <libavutil/hwcontext.h>
#include <libswscale/swscale.h>

#ifdef _WIN32
#define EXPORT __declspec(dllexport)
#define HW_TYPE AV_HWDEVICE_TYPE_D3D11VA
#define HW_CODE 3
#elif defined(__APPLE__)
#define EXPORT __attribute__((visibility("default")))
#define HW_TYPE AV_HWDEVICE_TYPE_VIDEOTOOLBOX
#define HW_CODE 4
#else
#define EXPORT __attribute__((visibility("default")))
#define HW_TYPE AV_HWDEVICE_TYPE_VAAPI
#define HW_CODE 2
#endif

struct decoder {
    AVCodecContext *codec;
    AVPacket *packet;
    AVFrame *decoded, *transfer, *rgba;
    SwsContext *scale;
};

EXPORT uint32_t onrdp_scrcpy_abi(void) { return 2; }

EXPORT void onrdp_scrcpy_close(struct decoder *d) {
    if (!d) return;
    avcodec_free_context(&d->codec);
    av_packet_free(&d->packet);
    av_frame_free(&d->decoded);
    av_frame_free(&d->transfer);
    av_frame_free(&d->rgba);
    sws_free_context(&d->scale);
    free(d);
}

EXPORT struct decoder *onrdp_scrcpy_open(void) {
    struct decoder *d = calloc(1, sizeof(*d));
    if (!d) return NULL;
    const AVCodec *codec = avcodec_find_decoder(AV_CODEC_ID_H264);
    d->codec = avcodec_alloc_context3(codec);
    d->packet = av_packet_alloc();
    d->decoded = av_frame_alloc();
    d->transfer = av_frame_alloc();
    d->rgba = av_frame_alloc();
    d->scale = sws_alloc_context();
    if (!codec || !d->codec || !d->packet || !d->decoded || !d->transfer || !d->rgba || !d->scale)
        goto fail;
    d->codec->thread_count = 1;
    d->codec->max_pixels = 8192LL * 8192;
    // FFmpeg selects a software pixel format if the device cannot decode this stream.
    av_hwdevice_ctx_create(&d->codec->hw_device_ctx, HW_TYPE, NULL, NULL, 0);
    d->codec->hwaccel_flags |= AV_HWACCEL_FLAG_ALLOW_PROFILE_MISMATCH;
    if (avcodec_open2(d->codec, codec, NULL) >= 0) return d;
fail:
    onrdp_scrcpy_close(d);
    return NULL;
}

// One scrcpy media packet contains one access unit. Return 0 while buffering,
// a negative FFmpeg error on failure, or the actual decoder on a rendered frame.
// Pixels remain valid until the next call or close. The stride includes padding.
EXPORT int32_t onrdp_scrcpy_decode(struct decoder *d, const uint8_t *data, int32_t len,
                                   uint8_t **pixels, int32_t *width, int32_t *height, int32_t *stride) {
    av_packet_unref(d->packet);
    int ret = av_new_packet(d->packet, len);
    if (ret < 0) return ret;
    memcpy(d->packet->data, data, len);
    ret = avcodec_send_packet(d->codec, d->packet);
    if (ret < 0) return ret;
    AVFrame *src = d->decoded;
    ret = avcodec_receive_frame(d->codec, src);
    if (ret == AVERROR(EAGAIN)) return 0;
    if (ret < 0) return ret;
    int backend = 1;
    if (src->hw_frames_ctx) {
        av_frame_unref(d->transfer);
        ret = av_hwframe_transfer_data(d->transfer, src, 0);
        if (ret < 0) return ret;
        ret = av_frame_copy_props(d->transfer, src);
        if (ret < 0) return ret;
        src = d->transfer;
        backend = HW_CODE;
    }
    AVFrame *dst = d->rgba;
    if (src->width != dst->width || src->height != dst->height) {
        av_frame_unref(dst);
        dst->format = AV_PIX_FMT_RGBA;
        dst->width = src->width;
        dst->height = src->height;
        ret = av_frame_get_buffer(dst, 32);
        if (ret < 0) return ret;
    }
    ret = sws_scale_frame(d->scale, dst, src);
    if (ret < 0) return ret;
    *pixels = dst->data[0];
    *stride = dst->linesize[0];
    *width = dst->width;
    *height = dst->height;
    return backend;
}
