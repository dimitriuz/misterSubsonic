package config

import (
	"bytes"
	"log"
	"strings"

	"github.com/BurntSushi/toml"
)

// editConfig returns text (an existing config.toml) changed to hold cfg while
// keeping the user's comments, blank lines, key order and unknown keys: only
// the values that differ are touched. It reports false when the file has a
// layout it cannot edit line by line (inline tables, multi-line strings...) or
// when the edited text does not decode back to cfg; Save then rewrites the file.
func editConfig(text string, cfg *Config) (out string, ok bool) {
	defer func() {
		if r := recover(); r != nil { // never the file's text: it holds credentials
			log.Printf("config: editing the file in place failed (%v); rewriting it", r)
			out, ok = "", false
		}
	}()
	old := Default()
	if _, err := toml.Decode(text, old); err != nil {
		return "", false
	}
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	e := &editor{lines: strings.Split(text, "\n"), eol: eol}
	o, n := scalars(old), scalars(cfg)
	for i := range n {
		if o[i].val == n[i].val {
			continue
		}
		v, ok := tomlValue(n[i].key, n[i].val)
		if !ok || !e.set(n[i].sec, n[i].key, v) {
			return "", false
		}
	}
	if !e.servers(old.Servers, cfg.Servers) {
		return "", false
	}
	out = strings.Join(e.lines, "\n")
	got := Default()
	if _, err := toml.Decode(out, got); err != nil || encodeAll(got) != encodeAll(cfg) {
		return "", false
	}
	return out, true
}

func encodeAll(c *Config) string {
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(c); err != nil {
		return err.Error()
	}
	return b.String()
}

// bom may start the file.
const bom = "\uFEFF"

type scalar struct {
	sec, key string
	val      any
}

// scalars lists every setting outside the [[server]] tables.
func scalars(c *Config) []scalar {
	return []scalar{
		{"", "default_server", c.DefaultServer},
		{"playback", "transcode_format", c.Playback.TranscodeFormat},
		{"playback", "transcode_bitrate", c.Playback.TranscodeBitrate},
		{"playback", "replaygain", c.Playback.ReplayGain},
		{"playback", "volume_db", c.Playback.VolumeDB},
		{"playback", "scrobble", c.Playback.Scrobble},
		{"playback", "buffer_mb", c.Playback.BufferMB},
		{"playback", "alsa_device", c.Playback.ALSADevice},
		{"display", "profile", c.Display.Profile},
		{"display", "screensaver_minutes", c.Display.ScreensaverMinutes},
		{"display", "full_resolution", c.Display.FullResolution},
		{"display", "hints", c.Display.Hints},
		{"display", "visualizer", c.Display.Visualizer},
		{"cache", "cover_art_mb", c.Cache.CoverArtMB},
		{"remote", "enabled", c.Remote.Enabled},
		{"remote", "port", c.Remote.Port},
	}
}

// tomlValue renders v the way the TOML encoder would.
func tomlValue(key string, v any) (string, bool) {
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(map[string]any{key: v}); err != nil {
		return "", false
	}
	s := strings.TrimSpace(b.String())
	_, val, ok := strings.Cut(s, "= ")
	return val, ok
}

type editor struct {
	lines []string // the file split at "\n"; a trailing "" stands for the final newline
	eol   string
}

// end is the index where lines can be appended (before the final newline).
func (e *editor) end() int {
	if n := len(e.lines); n > 0 && e.lines[n-1] == "" {
		return n - 1
	}
	return len(e.lines)
}

func (e *editor) insert(at int, add ...string) {
	for i := range add {
		if e.eol == "\r\n" {
			add[i] += "\r"
		}
	}
	e.lines = append(e.lines[:at:at], append(add, e.lines[at:]...)...)
}

// header returns the table a line opens ("" if it is not a table header).
func header(line string) string {
	t := strings.TrimSpace(strings.TrimPrefix(line, bom))
	if !strings.HasPrefix(t, "[") {
		return ""
	}
	t = strings.TrimSpace(strings.Trim(strings.SplitN(t, "#", 2)[0], "[] \t\r"))
	return t
}

func isHeader(line string) bool {
	t := strings.TrimSpace(strings.TrimPrefix(line, bom))
	return strings.HasPrefix(t, "[")
}

// keyLine reports whether line assigns key, and where its value starts.
func keyLine(line, key string) (int, bool) {
	skip := 0
	if strings.HasPrefix(line, bom) { // a BOM may start line 0; the index stays into the whole line
		skip = len(bom)
	}
	t := strings.TrimLeft(line[skip:], " \t")
	var rest string
	switch {
	case t != "" && (t[0] == '"' || t[0] == '\''): // a quoted key
		if !strings.HasPrefix(t[1:], key) || len(t) < len(key)+2 || t[len(key)+1] != t[0] {
			return 0, false
		}
		rest = strings.TrimLeft(t[len(key)+2:], " \t")
	case strings.HasPrefix(t, key):
		rest = strings.TrimLeft(t[len(key):], " \t")
	default:
		return 0, false
	}
	if !strings.HasPrefix(rest, "=") {
		return 0, false
	}
	i := len(line) - len(rest) + 1
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return i, true
}

// valueLen is the length of the scalar at the start of s, up to its trailing
// comment; 0 if it is a multi-line string or unterminated.
func valueLen(s string) int {
	switch {
	case strings.HasPrefix(s, `"""`), strings.HasPrefix(s, `'''`), s == "":
		return 0
	case s[0] == '"':
		for i := 1; i < len(s); i++ {
			switch s[i] {
			case '\\':
				i++
			case '"':
				return i + 1
			}
		}
		return 0
	case s[0] == '\'':
		if i := strings.IndexByte(s[1:], '\''); i >= 0 {
			return i + 2
		}
		return 0
	}
	return strings.IndexAny(s+" ", " \t#\r")
}

// set gives key in table sec the value v: in place if the key is there, else
// as a new line in the table (created at the end of the file if need be).
func (e *editor) set(sec, key, v string) bool {
	cur, last, first := "", -1, -1 // last: the table's last key line, or its header
	for i, l := range e.lines {
		if isHeader(l) {
			cur = header(l)
			if cur == sec {
				last = i
			}
			continue
		}
		if cur != sec {
			continue
		}
		if j, ok := keyLine(l, key); ok {
			n := valueLen(l[j:])
			if n == 0 {
				return false
			}
			e.lines[i] = l[:j] + v + l[j+n:]
			return true
		}
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "#") {
			last = i
		}
	}
	if sec == "" { // before the first table: after the leading comments if there are no keys yet
		if last < 0 {
			first = 0
			for first < len(e.lines) && strings.HasPrefix(strings.TrimSpace(e.lines[first]), "#") {
				first++
			}
			e.insert(first, key+" = "+v)
			if first+1 < len(e.lines) && strings.TrimSpace(e.lines[first+1]) != "" {
				e.insert(first+1, "")
			}
			return true
		}
		e.insert(last+1, key+" = "+v)
		return true
	}
	if last >= 0 {
		e.insert(last+1, key+" = "+v)
		return true
	}
	at := e.end()
	add := []string{"[" + sec + "]", key + " = " + v}
	if at > 0 && strings.TrimSpace(e.lines[at-1]) != "" {
		add = append([]string{""}, add...)
	}
	e.insert(at, add...)
	return true
}

type block struct{ start, sub, end int } // lines [start, end) of one [[server]] table; its [server.*] subtables start at sub

// serverBlocks finds the [[server]] tables, each with the [server.*] tables
// that follow it. Comments and blank lines right
// before the next table belong to it, not to this one.
func (e *editor) serverBlocks() []block {
	var bs []block
	for i, l := range e.lines {
		if !isHeader(l) {
			continue
		}
		if n := len(bs); n > 0 && bs[n-1].end < 0 {
			if h := header(l); strings.HasPrefix(h, "server.") {
				if bs[n-1].sub < 0 {
					bs[n-1].sub = i
				}
				continue // a subtable of this server
			}
			bs[n-1].end = i
		}
		if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(l, bom)), "[[") && header(l) == "server" {
			bs = append(bs, block{i, -1, -1})
		}
	}
	for i := range bs {
		if bs[i].end < 0 {
			bs[i].end = e.end()
		}
		for bs[i].end > bs[i].start+1 {
			t := strings.TrimSpace(e.lines[bs[i].end-1])
			if t != "" && !strings.HasPrefix(t, "#") {
				break
			}
			bs[i].end--
		}
		if bs[i].sub < 0 || bs[i].sub > bs[i].end {
			bs[i].sub = bs[i].end
		}
	}
	return bs
}

func encodeServer(s Server, eol string) []string {
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(struct {
		Server []Server `toml:"server"`
	}{[]Server{s}}); err != nil {
		return nil
	}
	ls := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if eol == "\r\n" {
		for i := range ls {
			ls[i] += "\r"
		}
	}
	return ls
}

// servers brings the [[server]] tables from old to now: an unchanged server
// keeps its lines, a changed one is rewritten, a removed one is dropped and
// a new one is added after the last.
func (e *editor) servers(old, now []Server) bool {
	same := len(old) == len(now)
	for i := 0; same && i < len(old); i++ {
		same = old[i] == now[i]
	}
	if same {
		return true
	}
	bs := e.serverBlocks()
	if len(bs) != len(old) {
		return false
	}
	byName := map[string]Server{}
	for _, s := range now {
		byName[s.Name] = s
	}
	kept := map[string]bool{}
	var out []string
	pos := 0
	var added []string
	addNew := func() {
		for _, s := range now {
			if !kept[s.Name] {
				ls := encodeServer(s, e.eol)
				if ls == nil {
					added = nil
					return
				}
				blank := ""
				if e.eol == "\r\n" {
					blank = "\r"
				}
				added = append(added, append([]string{blank}, ls...)...)
			}
		}
	}
	for i, b := range bs {
		out = append(out, e.lines[pos:b.start]...)
		pos = b.end
		s, ok := byName[old[i].Name]
		switch {
		case !ok: // removed: drop the blank line that separated it, too
			if n := len(out); n > 0 && strings.TrimSpace(out[n-1]) == "" && n > 1 {
				out = out[:n-1]
			}
		case s == old[i]:
			kept[s.Name] = true
			out = append(out, e.lines[b.start:b.end]...)
		default:
			kept[s.Name] = true
			ls := encodeServer(s, e.eol)
			if ls == nil {
				return false
			}
			out = append(out, ls...)
			if b.sub < b.end { // its subtables are not ours to rewrite
				// the blank and comment lines after its last key go with the subtable
				k := b.sub
				for k > b.start+1 {
					t := strings.TrimSpace(e.lines[k-1])
					if t != "" && !strings.HasPrefix(t, "#") {
						break
					}
					k--
				}
				out = append(out, e.lines[k:b.sub]...)
				out = append(out, e.lines[b.sub:b.end]...)
			}
		}
		if i == len(bs)-1 {
			addNew()
			out = append(out, added...)
			added = nil
		}
	}
	out = append(out, e.lines[pos:]...)
	e.lines = out
	if len(bs) == 0 {
		addNew()
		if len(added) == 0 {
			return len(now) == 0
		}
		at := e.end()
		if at > 0 && strings.TrimSpace(e.lines[at-1]) == "" {
			added = added[1:] // already a blank line before
		}
		e.insert(at, added...)
	}
	return true
}
