#include <string.h>
#include <time.h>

#include "miniaudio.h"
#include "shim.h"

/* ---------- device ---------- */

static ma_device g_device;
static ma_context g_context;
static ma_pcm_rb g_ring;
static int g_open; /* read and written with __atomic */
static volatile int g_paused;
static volatile int g_flush_req;
static volatile uint64_t g_consumed;
static volatile float g_volume = 1.0f;
static volatile int g_lost;     /* the device stopped without being asked */
static volatile int g_stopping; /* a stop we asked for is under way */

/* Set at open, read by the callback. */
static ma_format g_format = ma_format_f32; /* what the device takes */
static uint32_t g_rate;
static uint32_t g_buffer_frames; /* ALSA's buffer: frames still to be heard after the ring is empty */
static int g_ctx_ready;          /* the context outlives reopens: initializing it costs ~250 ms on a MiSTer */
static int g_ctx_null;           /* the context is the null backend's */

/* Discards everything in the ring, counting it as consumed. Only the ring's
   consumer may call it: the callback, or the producer once the callback has
   stopped for good. */
static void drain_ring(void) {
    ma_uint32 avail = ma_pcm_rb_available_read(&g_ring);
    while (avail > 0) {
        ma_uint32 n = avail;
        void* p;
        if (ma_pcm_rb_acquire_read(&g_ring, &n, &p) != MA_SUCCESS || n == 0) {
            break;
        }
        ma_pcm_rb_commit_read(&g_ring, n);
        __atomic_add_fetch(&g_consumed, n, __ATOMIC_SEQ_CST);
        avail -= n;
    }
}

/* fill writes frames of float output to dst: ring audio, then silence. */
static void fill(float* dst, ma_uint32 frames) {
    if (__atomic_load_n(&g_flush_req, __ATOMIC_SEQ_CST)) {
        drain_ring();
        __atomic_store_n(&g_flush_req, 0, __ATOMIC_SEQ_CST);
    }
    ma_uint32 done = 0;
    if (!__atomic_load_n(&g_paused, __ATOMIC_SEQ_CST)) {
        while (done < frames) {
            ma_uint32 n = frames - done;
            void* p;
            if (ma_pcm_rb_acquire_read(&g_ring, &n, &p) != MA_SUCCESS || n == 0) {
                break;
            }
            memcpy(dst + done * 2, p, n * 2 * sizeof(float));
            ma_pcm_rb_commit_read(&g_ring, n);
            done += n;
        }
        __atomic_add_fetch(&g_consumed, done, __ATOMIC_SEQ_CST);
    }
    float vol;
    __atomic_load(&g_volume, &vol, __ATOMIC_RELAXED);
    if (vol != 1.0f) {
        for (ma_uint32 i = 0; i < done * 2; i++) {
            dst[i] *= vol;
        }
    }
    if (done < frames) {
        memset(dst + done * 2, 0, (frames - done) * 2 * sizeof(float));
    }
}

#define CONVERT_FRAMES 512

/* Float samples to the device's format, scaled by 2^(bits-1) and clamped:
   exact for samples that came from that many bits. bits 0: float, copied.
   s24 is packed, 3 bytes little-endian. */
void mss_convert_samples(void* dst, int bits, const float* src, uint32_t count) {
    unsigned char* d = (unsigned char*)dst;
    if (bits == 0) {
        memcpy(dst, src, (size_t)count * sizeof(float));
        return;
    }
    double scale = (double)(1u << (bits - 1));
    if (bits == 32) {
        scale = 2147483648.0;
    }
    double hi = scale - 1.0;
    for (uint32_t i = 0; i < count; i++) {
        double v = (double)src[i] * scale;
        v = v < -scale ? -scale : (v > hi ? hi : v);
        int32_t s = (int32_t)v;
        if (bits == 32) {
            memcpy(d + (size_t)i * 4, &s, 4);
        } else if (bits == 24) {
            d[i * 3] = (unsigned char)(s & 0xff);
            d[i * 3 + 1] = (unsigned char)((s >> 8) & 0xff);
            d[i * 3 + 2] = (unsigned char)((s >> 16) & 0xff);
        } else {
            int16_t h = (int16_t)s;
            memcpy(d + (size_t)i * 2, &h, 2);
        }
    }
}

static void data_cb(ma_device* dev, void* out, const void* in, ma_uint32 frames) {
    (void)dev;
    (void)in;
    if (g_format == ma_format_f32) {
        fill((float*)out, frames);
        return;
    }
    /* A native device: fill floats, then convert to its format with our own
       conversion: scaling by a power of two (not miniaudio's 2^n-1) keeps 16-
       and 24-bit samples exact. */
    static float buf[CONVERT_FRAMES * 2];
    ma_uint32 bps = ma_get_bytes_per_sample(g_format) * 2;
    unsigned char* dst = (unsigned char*)out;
    while (frames > 0) {
        ma_uint32 n = frames < CONVERT_FRAMES ? frames : CONVERT_FRAMES;
        fill(buf, n);
        mss_convert_samples(dst, g_format == ma_format_s32 ? 32 : g_format == ma_format_s24 ? 24 : g_format == ma_format_s16 ? 16 : 0, buf, (uint32_t)n * 2);
        dst += n * bps;
        frames -= n;
    }
}

static void notify_cb(const ma_device_notification* n) {
    if (n->type == ma_device_notification_type_stopped &&
        !__atomic_load_n(&g_stopping, __ATOMIC_SEQ_CST)) {
        __atomic_store_n(&g_lost, 1, __ATOMIC_SEQ_CST);
    }
}

static void sleep_ms(long ms) {
    struct timespec ts = { ms / 1000, (ms % 1000) * 1000000L };
    nanosleep(&ts, NULL);
}

/* open_device opens a device; the counters, pause and volume carry over.
   native: name is an ALSA name used as is, the device runs at rate exactly
   in its most precise format, or the open fails with MSS_RATE_UNSUPPORTED. */
static int open_device(const char* name, int null_backend, uint32_t rate, uint32_t ring_frames, int native) {
    /* The most precise formats first: 24-bit music stays 24-bit. */
    static const ma_format native_formats[] = { ma_format_s32, ma_format_s24, ma_format_f32, ma_format_s16 };
    ma_backend null_only[1] = { ma_backend_null };
    ma_result r;
    if (g_ctx_ready && g_ctx_null != null_backend) {
        ma_context_uninit(&g_context);
        g_ctx_ready = 0;
    }
    if (!g_ctx_ready) {
        r = ma_context_init(null_backend ? null_only : NULL, null_backend ? 1 : 0, NULL, &g_context);
        if (r != MA_SUCCESS) {
            return r;
        }
        g_ctx_ready = 1;
        g_ctx_null = null_backend;
    }
    ma_device_config cfg = ma_device_config_init(ma_device_type_playback);
    cfg.playback.format = ma_format_f32;
    cfg.playback.channels = 2;
    cfg.sampleRate = rate;
    cfg.periodSizeInMilliseconds = 20;
    cfg.dataCallback = data_cb;
    cfg.notificationCallback = notify_cb;
    cfg.noPreSilencedOutputBuffer = MA_TRUE;

    ma_device_info* infos = NULL;
    ma_uint32 count = 0;
    ma_device_id named;
    ma_device_id* id = NULL;
    if (native) {
        /* Used as is: miniaudio opens an id without a leading ':' unchanged
           (no dmix), and doesn't look it up. */
        memset(&named, 0, sizeof named);
        strncpy(named.alsa, name, sizeof named.alsa - 1);
        id = &named;
    } else if (name != NULL && name[0] != '\0' &&
               ma_context_get_devices(&g_context, &infos, &count, NULL, NULL) == MA_SUCCESS) {
        for (ma_uint32 i = 0; i < count; i++) {
            if (strcmp(infos[i].name, name) == 0 ||
                (g_context.backend == ma_backend_alsa && strcmp(infos[i].id.alsa, name) == 0)) {
                id = &infos[i].id;
                break;
            }
        }
    }
    cfg.playback.pDeviceID = id;

    r = ma_pcm_rb_init(ma_format_f32, 2, ring_frames, NULL, NULL, &g_ring);
    if (r != MA_SUCCESS) {
        return r;
    }
    if (native) {
        /* miniaudio falls back to a format of its own choosing (16-bit
           first) when the one asked for is missing, so try ours in order
           and keep the first the device really takes. A device that runs
           at another rate than asked doesn't take this rate at all. */
        r = MA_FORMAT_NOT_SUPPORTED;
        for (size_t i = 0; i < sizeof native_formats / sizeof native_formats[0]; i++) {
            cfg.playback.format = native_formats[i];
            r = ma_device_init(&g_context, &cfg, &g_device);
            if (r != MA_SUCCESS) {
                break;
            }
            if (g_device.playback.internalSampleRate != rate) {
                ma_device_uninit(&g_device);
                r = MSS_RATE_UNSUPPORTED;
                break;
            }
            if (g_device.playback.internalFormat == native_formats[i]) {
                break;
            }
            ma_device_uninit(&g_device);
            r = MA_FORMAT_NOT_SUPPORTED;
        }
        g_format = cfg.playback.format;
    } else {
        g_format = ma_format_f32;
        r = ma_device_init(&g_context, &cfg, &g_device);
    }
    if (r != MA_SUCCESS) {
        ma_pcm_rb_uninit(&g_ring);
        return r;
    }
    g_rate = rate;
    g_buffer_frames = g_device.playback.internalPeriodSizeInFrames * g_device.playback.internalPeriods;
    __atomic_store_n(&g_lost, 0, __ATOMIC_SEQ_CST);
    __atomic_store_n(&g_stopping, 0, __ATOMIC_SEQ_CST);
    r = ma_device_start(&g_device);
    if (r != MA_SUCCESS) {
        ma_device_uninit(&g_device);
        ma_pcm_rb_uninit(&g_ring);
        return r;
    }
    __atomic_store_n(&g_open, 1, __ATOMIC_SEQ_CST);
    return MA_SUCCESS;
}

/* close_device stops and frees the device. tail: first let ALSA's buffer
   play out, so the last frames handed over are heard. What is left in the
   ring counts as consumed. */
static void close_device(int tail) {
    if (tail && g_rate > 0 && !__atomic_load_n(&g_lost, __ATOMIC_SEQ_CST)) {
        sleep_ms((long)g_buffer_frames * 1000 / g_rate + 20);
    }
    __atomic_store_n(&g_stopping, 1, __ATOMIC_SEQ_CST);
    __atomic_store_n(&g_open, 0, __ATOMIC_SEQ_CST);
    ma_device_uninit(&g_device);
    drain_ring();
    ma_pcm_rb_uninit(&g_ring);
}

int mss_device_open(const char* device_name, int null_backend, uint32_t rate, uint32_t ring_frames, int native) {
    if (__atomic_load_n(&g_open, __ATOMIC_SEQ_CST)) {
        return MA_INVALID_OPERATION;
    }
    g_paused = 0;
    g_flush_req = 0;
    g_consumed = 0;
    return open_device(device_name, null_backend, rate, ring_frames, native);
}

int mss_device_reopen(const char* device_name, int null_backend, uint32_t rate, uint32_t ring_frames, int native, int tail) {
    if (__atomic_load_n(&g_open, __ATOMIC_SEQ_CST)) {
        close_device(tail);
    }
    g_flush_req = 0;
    return open_device(device_name, null_backend, rate, ring_frames, native);
}

void mss_device_close(void) {
    if (__atomic_load_n(&g_open, __ATOMIC_SEQ_CST)) {
        close_device(0);
    }
    if (g_ctx_ready) {
        ma_context_uninit(&g_context);
        g_ctx_ready = 0;
    }
}

uint32_t mss_device_rate(void) { return g_rate; }

int mss_device_format_bits(void) {
    return (int)ma_get_bytes_per_sample(g_format) * 8;
}

int mss_device_lost(void) { return __atomic_load_n(&g_lost, __ATOMIC_SEQ_CST); }

/* After mss_device_close the ring and device are gone: every call below is
   a no-op then (write takes nothing, flush does nothing). */

uint32_t mss_device_write(const float* frames, uint32_t frame_count) {
    if (!__atomic_load_n(&g_open, __ATOMIC_SEQ_CST)) {
        return 0;
    }
    uint32_t done = 0;
    while (done < frame_count) {
        ma_uint32 n = frame_count - done;
        void* p;
        if (ma_pcm_rb_acquire_write(&g_ring, &n, &p) != MA_SUCCESS || n == 0) {
            break;
        }
        memcpy(p, frames + done * 2, n * 2 * sizeof(float));
        ma_pcm_rb_commit_write(&g_ring, n);
        done += n;
    }
    return done;
}

uint32_t mss_device_space(void) {
    if (!__atomic_load_n(&g_open, __ATOMIC_SEQ_CST)) {
        return 0;
    }
    return ma_pcm_rb_available_write(&g_ring);
}

uint64_t mss_device_consumed(void) {
    return __atomic_load_n(&g_consumed, __ATOMIC_SEQ_CST);
}

void mss_device_set_paused(int paused) {
    __atomic_store_n(&g_paused, paused, __ATOMIC_SEQ_CST);
}

void mss_device_set_volume(float volume) {
    __atomic_store(&g_volume, &volume, __ATOMIC_RELAXED);
}

/* The callback is not running and won't run again unless we restart it. */
static int device_stopped(void) {
    ma_device_state st = ma_device_get_state(&g_device);
    return st == ma_device_state_stopped || st == ma_device_state_uninitialized;
}

void mss_device_flush(void) {
    if (!__atomic_load_n(&g_open, __ATOMIC_SEQ_CST)) {
        return;
    }
    if (device_stopped()) {
        drain_ring();
        return;
    }
    __atomic_store_n(&g_flush_req, 1, __ATOMIC_SEQ_CST);
    /* The callback normally clears the request within one period (20 ms).
       Wait at most 200 ms so a stalled or stopped device can't hang us. */
    for (int i = 0; i < 200 && __atomic_load_n(&g_flush_req, __ATOMIC_SEQ_CST); i++) {
        sleep_ms(1);
    }
    if (__atomic_load_n(&g_flush_req, __ATOMIC_SEQ_CST)) {
        if (device_stopped()) {
            drain_ring(); /* no consumer left: the single producer may drain */
        }
        __atomic_store_n(&g_flush_req, 0, __ATOMIC_SEQ_CST);
    }
}

/* Test hook: stops the device callback as a failed or unplugged device would. */
void mss_device_stop_for_test(void) {
    if (__atomic_load_n(&g_open, __ATOMIC_SEQ_CST)) {
        ma_device_stop(&g_device);
    }
}
