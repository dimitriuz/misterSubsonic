#ifndef MSS_SHIM_H
#define MSS_SHIM_H

#include <stddef.h>
#include <stdint.h>

/* Encoding hints passed from Go (must match audio.Format). */
enum { MSS_FMT_UNKNOWN = 0, MSS_FMT_FLAC = 1, MSS_FMT_MP3 = 2, MSS_FMT_WAV = 3 };

/* Decoder: pulls bytes through the Go callbacks mssGoRead/mssGoSeek and
   always outputs interleaved stereo float32 at the source sample rate. */
typedef struct mss_decoder mss_decoder;

typedef struct {
    uint32_t sample_rate;
} mss_decoder_info;

mss_decoder* mss_decoder_open(uintptr_t handle, int format, int* result);
void mss_decoder_get_info(mss_decoder* d, mss_decoder_info* info);
/* Total length in frames, 0 when unknown. Not called at open: for an MP3
   without a Xing header miniaudio scans the whole stream to answer. */
uint64_t mss_decoder_length(mss_decoder* d);
/* Returns a miniaudio result code; MA_AT_END (-17) with *frames_read == 0 at end of stream. */
int mss_decoder_read(mss_decoder* d, float* out, uint64_t frames, uint64_t* frames_read);
int mss_decoder_seek(mss_decoder* d, uint64_t frame);
void mss_decoder_close(mss_decoder* d);

/* Resampler: interleaved stereo float32, speexdsp. */
typedef struct mss_resampler mss_resampler;

mss_resampler* mss_resampler_new(uint32_t in_rate, uint32_t out_rate, int quality, int* err);
int mss_resampler_process(mss_resampler* r, const float* in, uint32_t* in_frames, float* out, uint32_t* out_frames);
uint32_t mss_resampler_input_latency(mss_resampler* r);
void mss_resampler_free(mss_resampler* r);

/* Output device: one global 48 kHz stereo float32 device fed from a
   single-producer ring. The device callback never blocks. */
int mss_device_open(const char* device_name, int null_backend, uint32_t ring_frames);
void mss_device_close(void);
uint32_t mss_device_write(const float* frames, uint32_t frame_count);
uint32_t mss_device_space(void);
uint64_t mss_device_consumed(void);
void mss_device_set_paused(int paused);
void mss_device_set_volume(float volume);
void mss_device_flush(void);

#endif
