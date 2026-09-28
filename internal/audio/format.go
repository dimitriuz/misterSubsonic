package audio

import "strings"

// Format is the container/codec of a source stream. Values match MSS_FMT_* in shim.h.
type Format int

const (
	FormatUnknown Format = iota
	FormatFLAC
	FormatMP3
	FormatWAV
)

// FormatFromSuffix maps a file suffix ("flac", ".MP3") to a Format the
// device can decode natively. Anything else is FormatUnknown.
func FormatFromSuffix(suffix string) Format {
	switch strings.ToLower(strings.TrimPrefix(suffix, ".")) {
	case "flac":
		return FormatFLAC
	case "mp3":
		return FormatMP3
	case "wav", "wave":
		return FormatWAV
	}
	return FormatUnknown
}

func (f Format) String() string {
	switch f {
	case FormatFLAC:
		return "FLAC"
	case FormatMP3:
		return "MP3"
	case FormatWAV:
		return "WAV"
	}
	return "unknown"
}
