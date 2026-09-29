#!/bin/sh
# Re-downloads the vendored C sources at their pinned versions and verifies
# them against SHA256SUMS. The files are committed; run this only to audit
# them or when bumping a version (then regenerate SHA256SUMS).
set -eu
cd "$(dirname "$0")"

MINIAUDIO=0.11.25
SPEEXDSP=SpeexDSP-1.2.1
MA=https://raw.githubusercontent.com/mackron/miniaudio/$MINIAUDIO
SX=https://raw.githubusercontent.com/xiph/speexdsp/$SPEEXDSP

mkdir -p miniaudio speexdsp
curl -fsSL -o miniaudio/miniaudio.h        "$MA/miniaudio.h"
curl -fsSL -o speexdsp/resample.c          "$SX/libspeexdsp/resample.c"
curl -fsSL -o speexdsp/arch.h              "$SX/libspeexdsp/arch.h"
curl -fsSL -o speexdsp/speex_resampler.h   "$SX/include/speex/speex_resampler.h"
curl -fsSL -o speexdsp/COPYING             "$SX/COPYING"
sha256sum -c SHA256SUMS
