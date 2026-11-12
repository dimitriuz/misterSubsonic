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
// there is none. With the setting on, Now Playing and the mini player ask
// for that ID, and the disc turns there.

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

// discID is the ID of the disc image of a song's album (Navidrome's),
// or "" for a song whose art isn't Navidrome's.
func discID(so subsonic.Song) subsonic.ID {
	if so.AlbumID == "" || !(strings.HasPrefix(string(so.CoverArt), "mf-") || strings.HasPrefix(string(so.CoverArt), "al-")) {
		return ""
	}
	return subsonic.ID("dc-" + string(so.AlbumID) + ":" + strconv.Itoa(max(so.DiscNumber, 1)))
}

// probePx is the size of the small copy of a disc image fetched to learn
// whether the album has one: the server answers with the cover when not.
const probePx = 32

// hasDisc reports whether the song's album has a disc image. Until the small
// copy has loaded it is false, and loading it redraws what was marked with
// markArt for it (drawDiscBadge marks the badge).
func (a *App) hasDisc(so subsonic.Song) bool {
	id := discID(so)
	if id == "" {
		return false
	}
	img, ok := a.Art(id, probePx)
	return ok && isDisc(img)
}

// drawDiscBadge marks the cover in art with a small disc when the album has
// a disc image to switch to (and it isn't what is shown).
func (a *App) drawDiscBadge(c *gfx.Canvas, so subsonic.Song, art gfx.Rect) {
	s := max(art.W/6, 12)
	pad := max(art.W/30, 2)
	badge := gfx.R(art.Right()-s-pad, art.Y+pad, s, s)
	if id := discID(so); id != "" {
		a.markArt(id, badge)
	}
	if a.hasDisc(so) && !a.discArt() {
		c.Fill(badge, colOverlay)
		drawIcon(c, iconDisc, badge.Inset(max(s/6, 1)), colText)
	}
}

// nowArt is the art of the song playing, for Now Playing and the mini player:
// its cover, or its disc's image. Lists and cards always show covers.
func (a *App) nowArt(so subsonic.Song) subsonic.ID {
	if id := discID(so); id != "" && a.discArt() {
		return id
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
