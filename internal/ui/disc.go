package ui

import (
	"math"
	"strconv"
	"strings"
	"time"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/subsonic"
)

// Disc art (Navidrome): getCoverArt takes "dc-<albumId>:<disc>" for the
// disc image in an album's folder, and answers with the album's cover when
// there is none. With the setting on, albums and songs ask for that ID.

const (
	spinPeriod = 6 * time.Second        // one turn of a disc on Now Playing
	spinFrame  = 100 * time.Millisecond // how often a spinning disc is redrawn
)

func (a *App) discArt() bool { return a.cfg != nil && a.cfg.Display.DiscArt }

// setDiscArt switches between covers and disc images, and saves the choice.
func (a *App) setDiscArt(on bool) {
	a.UpdateConfig(func(c *config.Config) { c.Display.DiscArt = on }, false)
	a.dirty = true
}

// albumArtID is the disc image's ID for a Navidrome album cover ID
// ("al-<id>_<hash>"); anything else (other servers) has none.
func albumArtID(cover subsonic.ID, disc int) subsonic.ID {
	id, ok := strings.CutPrefix(string(cover), "al-")
	if !ok {
		return ""
	}
	id, _, _ = strings.Cut(id, "_")
	return subsonic.ID("dc-" + id + ":" + strconv.Itoa(max(disc, 1)))
}

// albumArt is the art to show for an album: its cover, or its disc.
func (a *App) albumArt(cover subsonic.ID) subsonic.ID {
	if a.discArt() {
		if id := albumArtID(cover, 1); id != "" {
			return id
		}
	}
	return cover
}

// songArt is the art to show for a song: its cover, or its disc's image.
func (a *App) songArt(so subsonic.Song) subsonic.ID {
	if a.discArt() && so.AlbumID != "" && (strings.HasPrefix(string(so.CoverArt), "mf-") || strings.HasPrefix(string(so.CoverArt), "al-")) {
		return subsonic.ID("dc-" + string(so.AlbumID) + ":" + strconv.Itoa(max(so.DiscNumber, 1)))
	}
	return so.CoverArt
}

// isDisc reports whether img is a disc: round, so its corners are
// transparent (the server answers with the plain cover for an album that
// has no disc image, and that one must not spin).
func isDisc(img *gfx.Image) bool {
	if img == nil || img.W < 2 || img.H < 2 {
		return false
	}
	for _, i := range []int{0, img.W - 1, (img.H - 1) * img.W, img.H*img.W - 1} {
		if img.Pix[i]>>24 > 0x40 {
			return false
		}
	}
	return true
}

// spinAngle is where a disc is after pos of playback: a turn every spinPeriod.
func spinAngle(pos time.Duration) float64 {
	return 2 * math.Pi * float64(pos%spinPeriod) / float64(spinPeriod)
}

// drawSpinning draws the cover id in r, turned by angle when it is a disc.
// A disc's region is remembered so it is redrawn as it turns.
func (a *App) drawSpinning(c *gfx.Canvas, id subsonic.ID, r gfx.Rect, angle float64) {
	img, ok := a.Art(id, r.W)
	if !ok || !isDisc(img) {
		a.drawArt(c, id, r)
		return
	}
	a.markArt(id, r)
	a.spin = r
	c.BlitRotated(img, r, angle)
}
