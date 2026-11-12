package ui

import (
	"log"
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
	spinPeriod = 6 * time.Second // one turn of a disc on Now Playing

	spinMeasure = 2 * time.Second // the span a disc frame's average cost is taken over
	spinSlow    = 0.15            // step down above this share of the frame interval
)

// spinFrames are how often a spinning disc is redrawn, fastest first. The
// valve steps down them when the draw costs too much of the interval: the
// music must never wait for the picture.
var spinFrames = []time.Duration{200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond}

// spinState is what the spinning disc keeps between frames.
type spinState struct {
	level  int           // index into spinFrames
	size   int           // the disc's size when last drawn: a new size starts the valve again
	sum    time.Duration // draw costs since since
	n      int
	since  time.Time
	due    time.Time // when the disc is next redrawn (zero: now)
	logged bool
}

func (a *App) spinInterval() time.Duration { return spinFrames[a.spinSt.level] }

// spinCost takes in what drawing the disc cost and slows the disc down when
// the last spinMeasure averaged over spinSlow of the frame interval.
func (a *App) spinCost(cost time.Duration, size int) {
	s := &a.spinSt
	now := a.o.Now()
	if size != s.size {
		*s = spinState{size: size, due: s.due}
	}
	if s.since.IsZero() {
		s.since = now
	}
	s.sum += cost
	s.n++
	if now.Sub(s.since) < spinMeasure {
		return
	}
	avg := s.sum / time.Duration(s.n)
	s.sum, s.n, s.since = 0, 0, now
	if float64(avg) <= spinSlow*float64(spinFrames[s.level]) {
		return
	}
	if s.level < len(spinFrames)-1 {
		s.level++
		log.Printf("disc: a frame costs %v, turning every %v now", avg, spinFrames[s.level])
	} else if !s.logged {
		s.logged = true
		log.Printf("disc: a frame costs %v even at the slowest rate", avg)
	}
}

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

// discMark draws a small disc icon at the right end of row, as tall as f's
// letters, when the album has a disc image to switch to and the cover is
// shown. It returns the width it takes (0 when it draws nothing), so the
// text on that row can be shortened to leave it room. The row is redrawn
// when the answer arrives, since the text changes with it.
func (a *App) discMark(c *gfx.Canvas, so subsonic.Song, row gfx.Rect, f *gfx.Font, col gfx.Color) int {
	if id := discID(so); id != "" {
		a.markArt(id, row)
	}
	if !a.hasDisc(so) || a.discArt() {
		return 0
	}
	s := f.Ascent()
	drawIcon(c, iconDisc, gfx.R(row.Right()-s, row.Y+(row.H-s)/2, s, s), col)
	return s + a.P.Margin/2
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
	t := time.Now()
	c.BlitRotatedFast(img, r, angle)
	a.spinCost(time.Since(t), r.W)
}
