// Package ui is the TV interface: a single-goroutine event loop that owns
// a stack of screens and renders them into a logical canvas, scaled to the
// display. Screens talk to the library, player and cover art through small
// interfaces so they can be tested with fakes and golden screenshots.
package ui

import (
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
}

var (
	ProfileHDMI = Profile{Name: "hdmi", W: 1280, H: 720, Margin: 36, HeaderH: 64, RowH: 56, Row2H: 76, Thumb: 60,
		Title: 34, Body: 24, Small: 18, ArtNow: 400, ArtAlbum: 200, MiniBarH: 72, MarqueeSpeed: 60}
	ProfileCRT240 = Profile{Name: "crt", W: 320, H: 240, Margin: 16, HeaderH: 22, RowH: 18, Row2H: 32, Thumb: 26,
		Title: 15, Body: 12, Small: 10, ArtNow: 110, ArtAlbum: 56, MiniBarH: 24, SafeY: 12, MarqueeSpeed: 24}
)

// PickProfile chooses the layout for a framebuffer (spec §8.1): "auto" picks
// CRT at 288 lines or fewer, otherwise HDMI.
func PickProfile(fbW, fbH int, override string) Profile {
	crt := override == "crt" || (override != "hdmi" && fbH <= gfx.CRTMaxLines)
	if !crt {
		return ProfileHDMI
	}
	p := ProfileCRT240
	if fbH == 288 || fbH == 576 {
		p.H = 288
	}
	return p
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
