package audio

/*
#cgo CFLAGS: -O2 -I${SRCDIR}/../../third_party/miniaudio -I${SRCDIR}/../../third_party/speexdsp
#cgo CFLAGS: -DMA_NO_ENCODING -DMA_NO_GENERATION -DMA_NO_RESOURCE_MANAGER -DMA_NO_NODE_GRAPH -DMA_NO_ENGINE
#cgo CFLAGS: -DOUTSIDE_SPEEX -DFLOATING_POINT -DRANDOM_PREFIX=spx_mss -DEXPORT=
#cgo arm CFLAGS: -DMA_ENABLE_ONLY_SPECIFIC_BACKENDS -DMA_ENABLE_ALSA -DMA_ENABLE_NULL
#cgo linux LDFLAGS: -ldl -lpthread -lm
#include "shim.h"
*/
import "C"
