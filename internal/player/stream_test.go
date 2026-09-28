package player

import (
	"testing"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/subsonic"
)

func TestPlanStream(t *testing.T) {
	st := StreamSettings{TranscodeFormat: "mp3", TranscodeBitrate: 320}
	cases := []struct {
		song       subsonic.Song
		format     string
		bitrate    int
		f          audio.Format
		transcoded bool
	}{
		{subsonic.Song{Suffix: "flac"}, "raw", 0, audio.FormatFLAC, false},
		{subsonic.Song{Suffix: "MP3"}, "raw", 0, audio.FormatMP3, false},
		{subsonic.Song{Suffix: "wav"}, "raw", 0, audio.FormatWAV, false},
		{subsonic.Song{Suffix: "", ContentType: "audio/x-flac"}, "raw", 0, audio.FormatFLAC, false},
		{subsonic.Song{Suffix: "m4a", ContentType: "audio/mp4"}, "mp3", 320, audio.FormatMP3, true},
		{subsonic.Song{Suffix: "opus"}, "mp3", 320, audio.FormatMP3, true},
	}
	for _, c := range cases {
		o, f, tr := PlanStream(c.song, st)
		if o.Format != c.format || o.MaxBitRate != c.bitrate || f != c.f || tr != c.transcoded {
			t.Errorf("PlanStream(%+v) = %+v %v %v", c.song, o, f, tr)
		}
	}
	o, f, _ := PlanStream(subsonic.Song{Suffix: "m4a"}, StreamSettings{TranscodeFormat: "flac"})
	if o.Format != "flac" || o.MaxBitRate != 0 || f != audio.FormatFLAC {
		t.Errorf("flac transcode = %+v %v", o, f)
	}
}

func f64(v float64) *float64 { return &v }

func TestReplayGainFactor(t *testing.T) {
	both := subsonic.Song{ReplayGain: &subsonic.ReplayGain{TrackGain: f64(-6), AlbumGain: f64(-3)}}
	near := func(got, want float32) bool { return got > want-0.005 && got < want+0.005 }
	if g := ReplayGainFactor(both, "off"); g != 1 {
		t.Errorf("off = %v", g)
	}
	if g := ReplayGainFactor(both, "track"); !near(g, 0.501) {
		t.Errorf("track = %v", g)
	}
	if g := ReplayGainFactor(both, "album"); !near(g, 0.708) {
		t.Errorf("album = %v", g)
	}
	onlyTrack := subsonic.Song{ReplayGain: &subsonic.ReplayGain{TrackGain: f64(-6)}}
	if g := ReplayGainFactor(onlyTrack, "album"); !near(g, 0.501) {
		t.Errorf("album falls back to track: %v", g)
	}
	if g := ReplayGainFactor(subsonic.Song{}, "track"); g != 1 {
		t.Errorf("no data = %v", g)
	}
	loud := subsonic.Song{ReplayGain: &subsonic.ReplayGain{TrackGain: f64(6), TrackPeak: f64(0.9)}}
	if g := ReplayGainFactor(loud, "track"); !near(g, 1/0.9) {
		t.Errorf("peak-limited gain = %v, want %v", g, 1/0.9)
	}
}
