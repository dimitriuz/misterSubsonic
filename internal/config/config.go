// Package config loads and saves config.toml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultPath is where the app looks on a MiSTer.
const DefaultPath = "/media/fat/mistersubsonic/config.toml"

type Config struct {
	DefaultServer string   `toml:"default_server"`
	Servers       []Server `toml:"server"`
	Playback      Playback `toml:"playback"`
	Display       Display  `toml:"display"`
	Cache         Cache    `toml:"cache"`
	Remote        Remote   `toml:"remote"`
}

type Server struct {
	Name                   string `toml:"name"`
	URL                    string `toml:"url"`
	Username               string `toml:"username"`
	Token                  string `toml:"token,omitempty"`
	Salt                   string `toml:"salt,omitempty"`
	Password               string `toml:"password,omitempty"`
	APIKey                 string `toml:"api_key,omitempty"`
	CAFile                 string `toml:"ca_file,omitempty"`
	InsecureSkipVerify     bool   `toml:"insecure_skip_verify,omitempty"`
	AllowPlaintextPassword bool   `toml:"allow_plaintext_password,omitempty"`
}

type Playback struct {
	TranscodeFormat  string  `toml:"transcode_format"`
	TranscodeBitrate int     `toml:"transcode_bitrate"`
	ReplayGain       string  `toml:"replaygain"`
	VolumeDB         float64 `toml:"volume_db"`
	Scrobble         bool    `toml:"scrobble"`
	BufferMB         int     `toml:"buffer_mb"`
	ALSADevice       string  `toml:"alsa_device"`
}

type Display struct {
	Profile            string `toml:"profile"`
	ScreensaverMinutes int    `toml:"screensaver_minutes"`
	FullResolution     bool   `toml:"full_resolution"`
	Hints              bool   `toml:"hints"`
	Visualizer         string `toml:"visualizer"` // off, bars, scope, vu or waterfall
	// Overscan margins, whole percents 0..MaxOverscan: the picture is drawn
	// inside them, for TVs that cut the edges. Left and right are of the
	// width; Y is of the height, for the top and the bottom each.
	OverscanLeft  int `toml:"overscan_left"`
	OverscanRight int `toml:"overscan_right"`
	OverscanY     int `toml:"overscan_y"`
}

// MaxOverscan is the largest overscan margin, in percent.
const MaxOverscan = 10

type Cache struct {
	CoverArtMB int `toml:"cover_art_mb"`
}

// Remote is the web remote: a page on the home network that controls playback.
type Remote struct {
	Enabled bool `toml:"enabled"`
	Port    int  `toml:"port"`
}

// Default returns a config with every default filled in and no servers.
func Default() *Config {
	return &Config{
		Playback: Playback{TranscodeFormat: "mp3", TranscodeBitrate: 320, ReplayGain: "off", Scrobble: true, BufferMB: 32, ALSADevice: "default"},
		Display:  Display{Profile: "auto", ScreensaverMinutes: 5, FullResolution: true, Hints: true, Visualizer: "off"},
		Cache:    Cache{CoverArtMB: 200},
		Remote:   Remote{Port: 8080},
	}
}

// ErrNotFound means there is no config file yet (first run).
var ErrNotFound = errors.New("config: file not found")

// Load reads path over the defaults. Unknown keys are returned as warnings,
// not errors. A malformed or invalid file is an error; the file is left alone.
func Load(path string) (cfg *Config, warnings []string, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("config: %w", err)
	}
	cfg = Default()
	md, err := toml.Decode(string(b), cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("config: %s: %w", path, redactParseError(err))
	}
	for _, k := range md.Undecoded() {
		warnings = append(warnings, "unknown key "+k.String())
	}
	switch cfg.Display.Visualizer {
	case "off", "bars", "scope", "vu", "waterfall":
	default: // a typo must not stop the app: show nothing and say so
		warnings = append(warnings, fmt.Sprintf("display.visualizer: unknown value %q, using off", cfg.Display.Visualizer))
		cfg.Display.Visualizer = "off"
	}
	for _, o := range []struct {
		key string
		v   *int
	}{{"overscan_left", &cfg.Display.OverscanLeft}, {"overscan_right", &cfg.Display.OverscanRight}, {"overscan_y", &cfg.Display.OverscanY}} {
		if c := min(max(*o.v, 0), MaxOverscan); c != *o.v { // as above: carry on with the nearest value
			warnings = append(warnings, fmt.Sprintf("display.%s: %d is not 0..%d, using %d", o.key, *o.v, MaxOverscan, c))
			*o.v = c
		}
	}
	if cfg.Remote.Port < 1 || cfg.Remote.Port > 65535 { // as a typo above: carry on with the default
		warnings = append(warnings, fmt.Sprintf("remote.port: %d is not 1..65535, using 8080", cfg.Remote.Port))
		cfg.Remote.Port = 8080
	}
	if err := cfg.Validate(); err != nil {
		return nil, warnings, fmt.Errorf("config: %s: %w", path, err)
	}
	return cfg, warnings, nil
}

// redactParseError keeps where a TOML syntax error is but drops the parser's
// message, which quotes the offending text: for an unquoted password or
// token that text is the secret. Other decode errors name only types.
func redactParseError(err error) error {
	var pe toml.ParseError
	if !errors.As(err, &pe) {
		return err
	}
	msg := fmt.Sprintf("invalid TOML syntax at line %d, column %d", pe.Position.Line, pe.Position.Col)
	if pe.LastKey != "" {
		msg += fmt.Sprintf(" near key %q", pe.LastKey)
	}
	return errors.New(msg + " (are text values in double quotes?)")
}

// Validate checks values a user could plausibly get wrong.
func (c *Config) Validate() error {
	var errs []error
	names := map[string]bool{}
	for i, s := range c.Servers {
		where := fmt.Sprintf("server %d (%q)", i+1, s.Name)
		if s.Name == "" {
			errs = append(errs, fmt.Errorf("%s: name is empty", where))
		}
		if names[s.Name] {
			errs = append(errs, fmt.Errorf("%s: duplicate name", where))
		}
		names[s.Name] = true
		u, err := url.Parse(s.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Errorf("%s: url must look like http://host:port or https://host", where))
		}
		hasToken := s.Token != "" && s.Salt != ""
		if (s.Token != "") != (s.Salt != "") {
			errs = append(errs, fmt.Errorf("%s: token and salt must be set together", where))
		}
		if !hasToken && s.Password == "" && s.APIKey == "" {
			errs = append(errs, fmt.Errorf("%s: set password, token+salt, or api_key", where))
		}
		if s.APIKey == "" && s.Username == "" {
			errs = append(errs, fmt.Errorf("%s: username is empty", where))
		}
	}
	if c.DefaultServer != "" && !names[c.DefaultServer] {
		errs = append(errs, fmt.Errorf("default_server %q is not one of the servers", c.DefaultServer))
	}
	switch c.Playback.ReplayGain {
	case "off", "track", "album":
	default:
		errs = append(errs, fmt.Errorf("playback.replaygain must be off, track or album (got %q)", c.Playback.ReplayGain))
	}
	switch c.Playback.TranscodeFormat {
	case "mp3", "flac", "wav":
	default:
		errs = append(errs, fmt.Errorf("playback.transcode_format must be mp3, flac or wav (got %q)", c.Playback.TranscodeFormat))
	}
	if c.Playback.BufferMB < 4 || c.Playback.BufferMB > 512 {
		errs = append(errs, fmt.Errorf("playback.buffer_mb must be 4..512 (got %d)", c.Playback.BufferMB))
	}
	if c.Playback.VolumeDB > 0 || c.Playback.VolumeDB < -60 {
		errs = append(errs, fmt.Errorf("playback.volume_db must be -60..0 (got %v)", c.Playback.VolumeDB))
	}
	switch c.Display.Profile {
	case "auto", "hdmi", "crt":
	default:
		errs = append(errs, fmt.Errorf("display.profile must be auto, hdmi or crt (got %q)", c.Display.Profile))
	}
	if c.Display.ScreensaverMinutes < 0 {
		errs = append(errs, errors.New("display.screensaver_minutes must be >= 0"))
	}
	for _, o := range []struct {
		key string
		v   int
	}{{"overscan_left", c.Display.OverscanLeft}, {"overscan_right", c.Display.OverscanRight}, {"overscan_y", c.Display.OverscanY}} {
		if o.v < 0 || o.v > MaxOverscan {
			errs = append(errs, fmt.Errorf("display.%s must be 0..%d (got %d)", o.key, MaxOverscan, o.v))
		}
	}
	if c.Cache.CoverArtMB < 0 {
		errs = append(errs, errors.New("cache.cover_art_mb must be >= 0"))
	}
	return errors.Join(errs...)
}

// ActiveServer is the default server, or the first one.
func (c *Config) ActiveServer() (*Server, bool) {
	for i := range c.Servers {
		if c.Servers[i].Name == c.DefaultServer {
			return &c.Servers[i], true
		}
	}
	if len(c.Servers) > 0 {
		return &c.Servers[0], true
	}
	return nil, false
}

// Save writes cfg atomically (temp file + rename) after validating it. An
// existing file is edited in place, so the user's comments survive; a missing
// or unusual one is written afresh.
func Save(path string, cfg *Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config: refusing to save invalid config: %w", err)
	}
	var out []byte
	if prev, err := os.ReadFile(path); err == nil {
		if s, ok := editConfig(string(prev), cfg); ok {
			out = []byte(s) // the user's comments and layout stay
		}
	}
	if out == nil {
		var buf bytes.Buffer
		buf.WriteString("# MiSTer Subsonic configuration, saved by the app (the setup wizard or Settings).\n# Every option is described in config.example.toml, next to this file.\n")
		if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
			return fmt.Errorf("config: encode: %w", err)
		}
		out = buf.Bytes()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		var le *os.LinkError // report it as a PathError: callers show its reason
		if errors.As(err, &le) {
			err = &fs.PathError{Op: le.Op, Path: path, Err: le.Err}
		}
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

// BackupInvalid renames a broken config out of the way before the wizard
// writes a new one, returning the new name.
func BackupInvalid(path string, now time.Time) (string, error) {
	dst := path + ".invalid-" + strings.ReplaceAll(now.UTC().Format("20060102T150405Z"), ":", "")
	if err := os.Rename(path, dst); err != nil {
		return "", fmt.Errorf("config: %w", err)
	}
	return dst, nil
}
