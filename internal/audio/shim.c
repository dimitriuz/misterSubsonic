#include <stdlib.h>

#include "miniaudio.h"
#include "speex_resampler.h"
#include "shim.h"
#include "_cgo_export.h"

/* ---------- decoder ---------- */

struct mss_decoder {
    ma_decoder dec;
    uintptr_t handle;
};

static ma_result on_read(ma_decoder* dec, void* buf, size_t n, size_t* nread) {
    mss_decoder* d = (mss_decoder*)dec->pUserData;
    size_t got = 0;
    int rc = mssGoRead(d->handle, buf, n, &got);
    if (nread != NULL) {
        *nread = got;
    }
    if (rc != 0) {
        return MA_ERROR;
    }
    if (got == 0 && n > 0) {
        return MA_AT_END;
    }
    return MA_SUCCESS;
}

static ma_result on_seek(ma_decoder* dec, ma_int64 off, ma_seek_origin origin) {
    mss_decoder* d = (mss_decoder*)dec->pUserData;
    int whence = origin == ma_seek_origin_start ? 0 : (origin == ma_seek_origin_current ? 1 : 2);
    return mssGoSeek(d->handle, off, whence) == 0 ? MA_SUCCESS : MA_ERROR;
}

mss_decoder* mss_decoder_open(uintptr_t handle, int format, int* result) {
    mss_decoder* d = (mss_decoder*)calloc(1, sizeof(*d));
    if (d == NULL) {
        *result = MA_OUT_OF_MEMORY;
        return NULL;
    }
    d->handle = handle;
    ma_decoder_config cfg = ma_decoder_config_init(ma_format_f32, 2, 0);
    switch (format) {
    case MSS_FMT_FLAC: cfg.encodingFormat = ma_encoding_format_flac; break;
    case MSS_FMT_MP3:  cfg.encodingFormat = ma_encoding_format_mp3;  break;
    case MSS_FMT_WAV:  cfg.encodingFormat = ma_encoding_format_wav;  break;
    default:           cfg.encodingFormat = ma_encoding_format_unknown; break;
    }
    ma_result r = ma_decoder_init(on_read, on_seek, d, &cfg, &d->dec);
    *result = r;
    if (r != MA_SUCCESS) {
        free(d);
        return NULL;
    }
    return d;
}

void mss_decoder_get_info(mss_decoder* d, mss_decoder_info* info) {
    ma_format fmt;
    ma_uint32 ch, rate;
    ma_uint64 len = 0;
    ma_decoder_get_data_format(&d->dec, &fmt, &ch, &rate, NULL, 0);
    if (ma_decoder_get_length_in_pcm_frames(&d->dec, &len) != MA_SUCCESS) {
        len = 0;
    }
    info->sample_rate = rate;
    info->length_frames = len;
}

int mss_decoder_read(mss_decoder* d, float* out, uint64_t frames, uint64_t* frames_read) {
    ma_uint64 got = 0;
    ma_result r = ma_decoder_read_pcm_frames(&d->dec, out, frames, &got);
    *frames_read = got;
    return r;
}

int mss_decoder_seek(mss_decoder* d, uint64_t frame) {
    return ma_decoder_seek_to_pcm_frame(&d->dec, frame);
}

void mss_decoder_close(mss_decoder* d) {
    if (d == NULL) {
        return;
    }
    ma_decoder_uninit(&d->dec);
    free(d);
}

/* ---------- resampler ---------- */

struct mss_resampler {
    SpeexResamplerState* st;
};

mss_resampler* mss_resampler_new(uint32_t in_rate, uint32_t out_rate, int quality, int* err) {
    mss_resampler* r = (mss_resampler*)calloc(1, sizeof(*r));
    if (r == NULL) {
        *err = -1;
        return NULL;
    }
    r->st = speex_resampler_init(2, in_rate, out_rate, quality, err);
    if (r->st == NULL) {
        free(r);
        return NULL;
    }
    speex_resampler_skip_zeros(r->st);
    return r;
}

int mss_resampler_process(mss_resampler* r, const float* in, uint32_t* in_frames, float* out, uint32_t* out_frames) {
    return speex_resampler_process_interleaved_float(r->st, in, in_frames, out, out_frames);
}

uint32_t mss_resampler_input_latency(mss_resampler* r) {
    return (uint32_t)speex_resampler_get_input_latency(r->st);
}

void mss_resampler_free(mss_resampler* r) {
    if (r == NULL) {
        return;
    }
    speex_resampler_destroy(r->st);
    free(r);
}
