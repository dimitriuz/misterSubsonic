// Package ui is the TV interface: a single-goroutine event loop that owns
// a stack of screens and renders them into a logical canvas, scaled to the
// display. Screens talk to the library, player and cover art through small
// interfaces so they can be tested with fakes and golden screenshots.
package ui

import (
	"math"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
)

// Profile holds the layout numbers for one kind of display (spec §8.1).
type Profile struct {
	Name     string
	W, H     int // logical canvas
	Margin   int // title-safe margin
	HeaderH  int
	RowH     int // single-line rows
	Row2H    int // rows with a second, dim line
	Thumb    int // list thumbnail size (0 = none)
	Title    int // font px
	Body     int
	Small    int
	ArtNow   int // Now Playing cover size
	ArtAlbum int // album header cover size
	MiniBarH int
	// SafeY keeps content off the top and bottom lines a CRT's overscan
	// hides (title-safe, spec §8.2); panels still run to the edge.
	SafeY        int
	MarqueeSpeed int // px per second
	Cover        int // cover size in grids; 0 = lists with thumbnails instead (CRT)
	SideW        int // sidebar width; 0 = no sidebar (CRT: sections are a list)
}

var (
	ProfileHDMI = Profile{Name: "hdmi", W: 1280, H: 720, Margin: 36, HeaderH: 64, RowH: 56, Row2H: 76, Thumb: 60,
		Title: 34, Body: 24, Small: 18, ArtNow: 400, ArtAlbum: 200, MiniBarH: 72, MarqueeSpeed: 60, Cover: 170, SideW: 250}
	ProfileCRT240 = Profile{Name: "crt", W: 320, H: 240, Margin: 16, HeaderH: 22, RowH: 18, Row2H: 32, Thumb: 26,
		Title: 15, Body: 12, Small: 10, ArtNow: 110, ArtAlbum: 56, MiniBarH: 24, SafeY: 12, MarqueeSpeed: 24}
)

// PickProfile chooses the layout for a framebuffer (spec §8.1): "auto" picks
// CRT at 288 lines or fewer, otherwise HDMI. The HDMI layout is scaled to
// the framebuffer, so the picture is drawn at its own resolution: sharp
// text and covers at 960x600 or 1920x1080 rather than a 1280x720 picture
// resampled. A CRT layout keeps its logical size (its pixels aren't square).
func PickProfile(fbW, fbH int, override string) Profile {
	crt := override == "crt" || (override != "hdmi" && fbH <= gfx.CRTMaxLines)
	if !crt {
		return scaleProfile(ProfileHDMI, fbW, fbH)
	}
	p := ProfileCRT240
	if fbH == 288 || fbH == 576 {
		p.H = 288
	}
	return p
}

// PickProfileIn is PickProfile for a picture drawn inside the overscan
// margins: the HDMI layout is scaled to the inner iw×ih rather than the
// whole framebuffer, so nothing is stretched. A CRT layout keeps its logical
// size (the scaler fits it into the inner area).
func PickProfileIn(fbW, fbH, iw, ih int, override string) Profile {
	p := PickProfile(fbW, fbH, override)
	if p.Name == ProfileHDMI.Name {
		return scaleProfile(ProfileHDMI, iw, ih)
	}
	return p
}

// overscanRect is the inner area of a pw×ph framebuffer inside the margins
// of d: percents of the width (left, right) and of the height (top and
// bottom each), rounded to whole pixels. The inner size is kept even: an odd
// width takes the extra pixel from the right margin, an odd height from the
// bottom one.
func overscanRect(pw, ph int, d config.Display) gfx.Rect {
	px := func(n, pct int) int {
		return int(math.Round(float64(n) * float64(min(max(pct, 0), config.MaxOverscan)) / 100))
	}
	l, r, y := px(pw, d.OverscanLeft), px(pw, d.OverscanRight), px(ph, d.OverscanY)
	w, h := pw-l-r, ph-2*y
	if w%2 != 0 {
		w--
	}
	if h%2 != 0 {
		h--
	}
	return gfx.R(l, y, w, h)
}

// Smallest font sizes a scaled layout uses, so a small HDMI framebuffer
// stays readable.
const minTitle, minBody, minSmall = 15, 12, 10

// scaleProfile is p resized to a w×h canvas: every length is scaled by the
// factor that fits p's canvas into w×h, and the canvas takes the whole of
// w×h (a 16:10 screen gets more rows, not black bars).
func scaleProfile(p Profile, w, h int) Profile {
	if w == p.W && h == p.H || w <= 0 || h <= 0 {
		return p
	}
	k := min(float64(w)/float64(p.W), float64(h)/float64(p.H))
	s := func(v int) int {
		if v == 0 {
			return 0
		}
		return max(int(math.Round(float64(v)*k)), 1)
	}
	q := p
	q.W, q.H = w, h
	q.Margin, q.HeaderH, q.RowH, q.Row2H, q.Thumb = s(p.Margin), s(p.HeaderH), s(p.RowH), s(p.Row2H), s(p.Thumb)
	q.Title, q.Body, q.Small = max(s(p.Title), minTitle), max(s(p.Body), minBody), max(s(p.Small), minSmall)
	q.ArtNow, q.ArtAlbum, q.MiniBarH, q.SafeY = s(p.ArtNow), s(p.ArtAlbum), s(p.MiniBarH), s(p.SafeY)
	q.MarqueeSpeed, q.Cover, q.SideW = s(p.MarqueeSpeed), s(p.Cover), s(p.SideW)
	return q
}

// Colors.
var (
	colBg      = gfx.RGB(0x10, 0x14, 0x18)
	colPanel   = gfx.RGB(0x1b, 0x21, 0x28)
	colText    = gfx.RGB(0xe8, 0xea, 0xed)
	colDim     = gfx.RGB(0x9a, 0xa0, 0xa6)
	colAccent  = gfx.RGB(0x4f, 0xc3, 0xf7)
	colFocus   = gfx.RGBA(0x4f, 0xc3, 0xf7, 0x40)
	colError   = gfx.RGB(0xef, 0x53, 0x50)
	colOverlay = gfx.RGBA(0, 0, 0, 0xB0)
	colArtBg   = gfx.RGB(0x2a, 0x31, 0x3a)
)
