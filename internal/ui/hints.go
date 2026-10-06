package ui

import (
	"fmt"
	"strings"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// The hint bar (Plan 4b): one line along the bottom of every screen with
// the buttons that matter there, drawn as gamepad buttons or keyboard keys,
// whichever was used last.

// Hint is one entry: a button (and a second one for pairs such as L/R or
// Left/Right) and what it does on this screen. A hint for a typing key
// (Backspace, Enter) has Key, the cap drawn on a keyboard, and Rune, what
// that key types; only a keyboard shows it. A Hold hint is for holding the
// button, drawn "hold X Star".
type Hint struct {
	Button, Pair input.Button
	Label        string
	Key          string
	Rune         rune
	Hold         bool
}

// Hinter is a screen with its own hints, most important first. The app adds
// Back and Now Playing when the screen leaves B and Y to it.
type Hinter interface {
	Hints(a *App) []Hint
}

// modalHints is a screen that takes every button itself (a menu): the app
// adds no Back or Now Playing to its list, as it never sees those presses.
type modalHints interface {
	ownsAllButtons()
}

// textTaker is a screen where printable keys type text right now, so on a
// keyboard N is a letter and cannot open Now Playing.
type textTaker interface {
	Typing() bool
}

func hk(b input.Button, label string) Hint        { return Hint{Button: b, Label: label} }
func hkHold(b input.Button, label string) Hint    { return Hint{Button: b, Label: label, Hold: true} }
func hkPair(b, p input.Button, label string) Hint { return Hint{Button: b, Pair: p, Label: label} }

// commonHints are for screens without their own list.
var commonHints = []Hint{hk(input.BtnA, "Open"), hk(input.BtnX, "Menu")}

// padCap is a button's name on a gamepad ("" for the D-pad, drawn as arrows).
func padCap(b input.Button) string {
	switch b {
	case input.BtnA, input.BtnB, input.BtnX, input.BtnY, input.BtnL, input.BtnR:
		return b.String()
	case input.BtnStart:
		return "Start"
	case input.BtnSelect:
		return "Select"
	}
	return ""
}

// keyCap is the key for a button on a keyboard (the default key map); ok is
// false when no key does it.
func keyCap(b input.Button) (string, bool) {
	switch b {
	case input.BtnA:
		return "Enter", true
	case input.BtnB:
		return "Esc", true
	case input.BtnX:
		return "Tab", true
	case input.BtnY:
		return "N", true
	case input.BtnL:
		return "PgUp", true
	case input.BtnR:
		return "PgDn", true
	case input.BtnStart:
		return "Space", true
	case input.BtnQueue:
		return "Q", true
	case input.BtnMute:
		return "M", true
	case input.BtnSelect:
		return "V", true
	case input.BtnUp, input.BtnDown, input.BtnLeft, input.BtnRight:
		return "", true
	}
	return "", false
}

// capFor is b's cap for the current input, or false when it has none there.
func (a *App) capFor(b input.Button) (string, bool) {
	if a.pad {
		switch b {
		case input.BtnQueue, input.BtnMute:
			return "", false // keyboard only
		}
		return padCap(b), true
	}
	return keyCap(b)
}

func isArrow(b input.Button) bool {
	return b == input.BtnUp || b == input.BtnDown || b == input.BtnLeft || b == input.BtnRight
}

// hintsOn reports whether the bar is shown (Settings → Display → Hints).
func (a *App) hintsOn() bool { return a.cfg == nil || a.cfg.Display.Hints }

// hintH is the bar's height, 0 when it is off.
func (a *App) hintH() int {
	if !a.hintsOn() {
		return 0
	}
	return a.F.Small.Height() + a.P.Margin/2
}

// hintRect is the bar: the last line inside the title-safe area.
func (a *App) hintRect() gfx.Rect {
	h := a.hintH()
	return gfx.R(0, a.P.H-a.P.SafeY-h, a.P.W, h)
}

// withBack puts Back right after the first hint, so a narrow bar (CRT)
// keeps it: the bar drops what doesn't fit from the right.
func withBack(hs []Hint) []Hint {
	i := min(1, len(hs))
	return append(hs[:i:i], append([]Hint{hk(input.BtnB, "Back")}, hs[i:]...)...)
}

// screenHints is the top screen's list plus Back and Now Playing when the
// screen leaves B and Y to the app.
func (a *App) screenHints() []Hint {
	top := a.Top()
	var hs []Hint
	if h, ok := top.(Hinter); ok {
		hs = h.Hints(a)
	} else {
		hs = commonHints
	}
	if _, modal := top.(modalHints); modal {
		return hs
	}
	uses := func(b input.Button) bool {
		for _, h := range hs {
			if h.Button == b || h.Pair == b {
				return true
			}
		}
		return false
	}
	out := append([]Hint(nil), hs...)
	if len(a.stack) > 1 && !uses(input.BtnB) {
		out = withBack(out)
	}
	typing := false
	if t, ok := top.(textTaker); ok {
		typing = t.Typing()
	}
	if _, np := top.(*NowPlayingScreen); !np && a.hasQueue() && !uses(input.BtnY) && (a.pad || !typing) {
		out = append(out, hk(input.BtnY, "Now Playing"))
	}
	return out
}

// drawHints draws the bar: chips and labels from the left while they fit.
func (a *App) drawHints(c *gfx.Canvas) {
	r := a.hintRect()
	if r.Empty() {
		return
	}
	bar := gfx.R(r.X, r.Y, r.W, a.P.H-r.Y) // to the bottom edge, past the safe area
	if bar.Intersect(c.Clip()).Empty() {   // a partial frame elsewhere: no layout either
		return
	}
	c.Fill(bar, colPanel)
	f := a.F.Small
	p := a.P
	pad := max(p.Margin/4, 2)
	chipH := f.Height()
	chipY := r.Y + (r.H-chipH)/2
	base := chipY + (chipH+f.Ascent()-f.Descent())/2
	x, end := p.Margin, r.Right()-p.Margin
	for _, h := range a.screenHints() {
		caps := []input.Button{h.Button}
		if h.Pair != input.BtnNone {
			caps = append(caps, h.Pair)
		}
		var texts []string
		ok := true
		for _, b := range caps {
			t, has := a.capFor(b)
			if h.Key != "" {
				t, has = h.Key, true
			}
			ok = ok && has
			texts = append(texts, t)
		}
		if !ok {
			continue
		}
		// Width: one chip for a lone button or a pair of arrows, two for a
		// pair of named buttons (L/R).
		arrows := isArrow(caps[0])
		cw := 0
		if arrows {
			cw = 2*pad + len(caps)*chipH*3/5 + (len(caps)-1)*pad
		} else {
			for _, t := range texts {
				cw += 2*pad + max(f.Measure(t), chipH-2*pad)
			}
			cw += (len(caps) - 1) * pad
		}
		holdW := 0
		if h.Hold {
			holdW = f.Measure("hold") + pad
		}
		need := holdW + cw + pad + f.Measure(h.Label)
		if x+need > end {
			break
		}
		if h.Hold {
			f.Draw(c, x, base, "hold", colDim, c.Bounds())
			x += holdW
		}
		if arrows {
			chip := gfx.R(x, chipY, cw, chipH)
			fillRoundRect(c, chip, pad, colArtBg)
			ax := x + pad
			for _, b := range caps {
				drawArrow(c, gfx.R(ax, chipY+chipH/5, chipH*3/5, chipH*3/5), b, colText)
				ax += chipH*3/5 + pad
			}
			x += cw
		} else {
			for i, t := range texts {
				w := 2*pad + max(f.Measure(t), chipH-2*pad)
				fillRoundRect(c, gfx.R(x, chipY, w, chipH), pad, colArtBg)
				f.Draw(c, x+(w-f.Measure(t))/2, base, t, colText, c.Bounds())
				x += w
				if i < len(texts)-1 {
					x += pad
				}
			}
		}
		x += pad
		f.Draw(c, x, base, h.Label, colDim, c.Bounds())
		x += f.Measure(h.Label) + p.Margin*2/3
	}
}

// drawArrow draws a triangle pointing b's way (Up, Down, Left, Right) in r.
func drawArrow(c *gfx.Canvas, r gfx.Rect, b input.Button, col gfx.Color) {
	x0, y0 := float64(r.X), float64(r.Y)
	x1, y1 := float64(r.Right()), float64(r.Bottom())
	xm, ym := (x0+x1)/2, (y0+y1)/2
	var pts [][2]float64
	switch b {
	case input.BtnUp:
		pts = [][2]float64{{xm, y0}, {x1, y1}, {x0, y1}}
	case input.BtnDown:
		pts = [][2]float64{{x0, y0}, {x1, y0}, {xm, y1}}
	case input.BtnLeft:
		pts = [][2]float64{{x0, ym}, {x1, y0}, {x1, y1}}
	default:
		pts = [][2]float64{{x0, y0}, {x1, ym}, {x0, y1}}
	}
	fillPolygon(c, pts, col)
}

// noteSource follows the input: a press from the other kind of device
// switches the bar between gamepad buttons and keyboard keys.
func (a *App) noteSource(e input.Event) {
	if e.Kind != input.Press || e.Pad == a.pad {
		return
	}
	a.pad = e.Pad
	a.Damage(a.hintRect())
}

// hintLabels joins the labels ("Open, Menu").
func hintLabels(hs []Hint) string {
	var s []string
	for _, h := range hs {
		s = append(s, h.Label)
	}
	return strings.Join(s, ", ")
}

// hintKey identifies a hint list, to tell whether the bar changed.
func hintKey(hs []Hint) string {
	var b strings.Builder
	for _, h := range hs {
		fmt.Fprintf(&b, "%d/%d/%s/%s/%v;", h.Button, h.Pair, h.Label, h.Key, h.Hold)
	}
	return b.String()
}
