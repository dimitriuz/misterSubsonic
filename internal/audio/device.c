#include <string.h>
#include <time.h>

#include "miniaudio.h"
#include "shim.h"

/* ---------- device ---------- */

static ma_device g_device;
static ma_context g_context;
static ma_pcm_rb g_ring;
static int g_open;
static volatile int g_paused;
static volatile int g_flush_req;
static volatile uint64_t g_consumed;
static volatile float g_volume = 1.0f;

static void data_cb(ma_device* dev, void* out, const void* in, ma_uint32 frames) {
    (void)dev;
    (void)in;
    float* dst = (float*)out;
    if (g_flush_req) {
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
        __atomic_store_n(&g_flush_req, 0, __ATOMIC_SEQ_CST);
    }
    ma_uint32 done = 0;
    if (!g_paused) {
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
    float vol = g_volume;
    if (vol != 1.0f) {
        for (ma_uint32 i = 0; i < done * 2; i++) {
            dst[i] *= vol;
        }
    }
    if (done < frames) {
        memset(dst + done * 2, 0, (frames - done) * 2 * sizeof(float));
    }
}

int mss_device_open(const char* device_name, int null_backend, uint32_t ring_frames) {
    if (g_open) {
        return MA_INVALID_OPERATION;
    }
    ma_backend null_only[1] = { ma_backend_null };
    ma_result r = ma_context_init(null_backend ? null_only : NULL, null_backend ? 1 : 0, NULL, &g_context);
    if (r != MA_SUCCESS) {
        return r;
    }
    r = ma_pcm_rb_init(ma_format_f32, 2, ring_frames, NULL, NULL, &g_ring);
    if (r != MA_SUCCESS) {
        ma_context_uninit(&g_context);
        return r;
    }
    ma_device_config cfg = ma_device_config_init(ma_device_type_playback);
    cfg.playback.format = ma_format_f32;
    cfg.playback.channels = 2;
    cfg.sampleRate = 48000;
    cfg.periodSizeInMilliseconds = 20;
    cfg.dataCallback = data_cb;
    cfg.noPreSilencedOutputBuffer = MA_TRUE;

    ma_device_info* infos = NULL;
    ma_uint32 count = 0;
    ma_device_id* id = NULL;
    if (device_name != NULL && device_name[0] != '\0' &&
        ma_context_get_devices(&g_context, &infos, &count, NULL, NULL) == MA_SUCCESS) {
        for (ma_uint32 i = 0; i < count; i++) {
            if (strcmp(infos[i].name, device_name) == 0 ||
                (g_context.backend == ma_backend_alsa && strcmp(infos[i].id.alsa, device_name) == 0)) {
                id = &infos[i].id;
                break;
            }
        }
    }
    cfg.playback.pDeviceID = id;

    g_paused = 0;
    g_flush_req = 0;
    g_consumed = 0;
    r = ma_device_init(&g_context, &cfg, &g_device);
    if (r != MA_SUCCESS) {
        ma_pcm_rb_uninit(&g_ring);
        ma_context_uninit(&g_context);
        return r;
    }
    r = ma_device_start(&g_device);
    if (r != MA_SUCCESS) {
        ma_device_uninit(&g_device);
        ma_pcm_rb_uninit(&g_ring);
        ma_context_uninit(&g_context);
        return r;
    }
    g_open = 1;
    return MA_SUCCESS;
}

void mss_device_close(void) {
    if (!g_open) {
        return;
    }
    ma_device_uninit(&g_device);
    ma_pcm_rb_uninit(&g_ring);
    ma_context_uninit(&g_context);
    g_open = 0;
}

uint32_t mss_device_write(const float* frames, uint32_t frame_count) {
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
    return ma_pcm_rb_available_write(&g_ring);
}

uint64_t mss_device_consumed(void) {
    return __atomic_load_n(&g_consumed, __ATOMIC_SEQ_CST);
}

void mss_device_set_paused(int paused) {
    __atomic_store_n(&g_paused, paused, __ATOMIC_SEQ_CST);
}

void mss_device_set_volume(float volume) {
    g_volume = volume;
}

void mss_device_flush(void) {
    __atomic_store_n(&g_flush_req, 1, __ATOMIC_SEQ_CST);
    while (__atomic_load_n(&g_flush_req, __ATOMIC_SEQ_CST)) {
        struct timespec ts = { 0, 1000000 };
        nanosleep(&ts, NULL);
    }
}
