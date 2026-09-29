package ui

import (
	"bytes"
	"context"
	"image"
	"testing"
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
)

// probe is a test screen: it records what it is given and types into text.
type probe struct {
	area    gfx.Rect
	text    string
	handled []input.Event
	draw    func(a *App, c *gfx.Canvas, area gfx.Rect)
}

func (s *probe) Title() string { return "Probe" }
func (s *probe) Enter(*App)    {}
func (s *probe) Handle(a *App, e input.Event) bool {
	s.handled = append(s.handled, e)
	return false
}
func (s *probe) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	s.area = area
	if s.draw != nil {
		s.draw(a, c, area)
	}
}

// typer is a probe that takes text; Backspace in an empty field isn't text.
type typer struct{ probe }

func (s *typer) Text(a *App, r rune) bool {
	if r == '\b' {
		if s.text == "" {
			return false
		}
		r := []rune(s.text)
		s.text = string(r[:len(r)-1])
		return true
	}
	s.text += string(r)
	return true
}

func TestAfterRunsAndIsDroppedWithItsScreen(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	root, top := &probe{}, &probe{}
	ta.Push(root)
	ta.Push(top)
	var ran []string
	ta.After(root, 300*time.Millisecond, func() { ran = append(ran, "root") })
	ta.After(top, 300*time.Millisecond, func() { ran = append(ran, "top") })
	if d := ta.untilWake(); d != 300*time.Millisecond {
		t.Fatalf("next wake in %v, want 300ms", d)
	}
	ta.Pop()
	ta.now = ta.now.Add(299 * time.Millisecond)
	ta.onWake()
	if len(ran) != 0 {
		t.Fatalf("ran early: %v", ran)
	}
	ta.now = ta.now.Add(time.Millisecond)
	ta.onWake()
	if len(ran) != 1 || ran[0] != "root" {
		t.Fatalf("ran %v, want only root (top was popped)", ran)
	}
	if len(ta.timers) != 0 {
		t.Fatalf("%d timers left", len(ta.timers))
	}
}

func TestLoadCancelDropsTheResult(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	s := &probe{}
	ta.Push(s)
	started := make(chan struct{})
	var sawCancel bool
	delivered := false
	cancel := ta.LoadCancel(s, func(ctx context.Context) (any, error) {
		close(started)
		<-ctx.Done()
		sawCancel = true
		return nil, ctx.Err()
	}, func(any, error) { delivered = true })
	<-started
	cancel()
	ta.settle(t)
	if delivered || !sawCancel {
		t.Fatalf("delivered %v, fn saw cancel %v", delivered, sawCancel)
	}
}

func TestTextGoesToTextScreensAndSwallowsItsButtons(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.pl.st = player.State{Queue: ta.lib.tracks["al-1"], Index: 0, NextIndex: -1}
	root := &probe{}
	s := &typer{}
	ta.Push(root)
	ta.Push(s)
	ta.onInput(input.Event{Button: input.BtnY, Kind: input.Press, Rune: 'n'}) // N is also Y
	ta.onInput(input.Event{Button: input.BtnY, Kind: input.Release, Rune: 'n'})
	ta.onInput(input.Event{Kind: input.Press, Rune: 'ж'})
	ta.onInput(input.Event{Button: input.BtnStart, Kind: input.Press, Rune: ' '})
	ta.onInput(input.Event{Button: input.BtnStart, Kind: input.Release, Rune: ' '})
	if s.text != "nж " {
		t.Fatalf("typed %q", s.text)
	}
	if ta.Top() != s || len(ta.pl.calls) != 0 || len(s.handled) != 0 {
		t.Fatalf("typing acted as buttons: top %T, player %v, handled %v", ta.Top(), ta.pl.calls, s.handled)
	}
	for range 3 {
		ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press, Rune: '\b'})
		ta.onInput(input.Event{Button: input.BtnB, Kind: input.Release, Rune: '\b'})
	}
	if s.text != "" || ta.Top() != s {
		t.Fatalf("after 3 backspaces: text %q, top %T", s.text, ta.Top())
	}
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press, Rune: '\b'}) // empty: acts as B
	if ta.Top() != root {
		t.Fatalf("Backspace in an empty field did not go back (top %T)", ta.Top())
	}
	// Screens without text input get the buttons as usual; text-only keys do nothing.
	ta.onInput(input.Event{Kind: input.Press, Rune: 'x'})
	ta.onInput(input.Event{Button: input.BtnY, Kind: input.Press, Rune: 'n'})
	if _, ok := ta.Top().(*NowPlayingScreen); !ok {
		t.Fatalf("N outside a text field: top %T, want Now Playing", ta.Top())
	}
}

func TestQueueKeyOpensTheQueue(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(&probe{})
	ta.press(input.BtnQueue)
	if _, ok := ta.Top().(*probe); !ok {
		t.Fatal("Q with an empty queue opened something")
	}
	playingState(ta)
	ta.press(input.BtnY)
	ta.press(input.BtnQueue)
	ta.press(input.BtnQueue)
	if _, ok := ta.Top().(*QueueScreen); !ok || len(ta.stack) != 3 {
		t.Fatalf("top %T, %d screens; want the queue over Now Playing", ta.Top(), len(ta.stack))
	}
}

func TestSafeAreaAndMiniBarNeedACurrentSong(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	s := &probe{}
	ta.Push(s)
	ta.settle(t)
	p := ta.P
	if s.area.Y != p.SafeY+p.HeaderH || s.area.Bottom() != p.H-p.SafeY {
		t.Fatalf("body %+v: want from %d to %d", s.area, p.SafeY+p.HeaderH, p.H-p.SafeY)
	}
	ta.pl.st = player.State{Queue: ta.lib.tracks["al-1"], Index: -1, NextIndex: -1}
	ta.settle(t)
	if s.area.Bottom() != p.H-p.SafeY {
		t.Fatal("mini bar shown with nothing selected")
	}
	ta.pl.st.Index = 0
	ta.settle(t)
	if s.area.Bottom() != p.H-p.SafeY-p.MiniBarH {
		t.Fatalf("body bottom %d with a current song, want %d", s.area.Bottom(), p.H-p.SafeY-p.MiniBarH)
	}
}

func TestMarqueeScrollsFocusedLongText(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	long := "A Rather Long Album Title That Will Need Truncating Somewhere"
	focused := true
	ta.Push(&probe{draw: func(a *App, c *gfx.Canvas, area gfx.Rect) {
		f := a.F.Body
		a.drawFit(c, f, area.X, area.Y+f.Ascent(), 100, long, colText, area, focused)
	}})
	first := ta.settle(t).ToRGBA()
	if d := ta.untilWake(); d > marqueeFrame {
		t.Fatalf("next wake in %v while a marquee runs", d)
	}
	ta.now = ta.now.Add(marqueeDelay / 2)
	if !samePixels(first, ta.settle(t).ToRGBA()) {
		t.Fatal("marquee moved before its delay")
	}
	ta.now = ta.now.Add(2 * time.Second)
	if samePixels(first, ta.settle(t).ToRGBA()) {
		t.Fatal("focused long text did not scroll")
	}
	focused = false
	ta.settle(t)
	if ta.animate || ta.mq.text != "" {
		t.Fatal("marquee still running after focus moved")
	}
}

func TestInsecureBadge(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(&probe{})
	ta.SetInsecure(true)
	c := ta.settle(t)
	red := 0
	for y := 0; y < ta.P.HeaderH; y++ {
		for x := ta.P.W / 2; x < ta.P.W; x++ {
			if c.At(x, y) == colError {
				red++
			}
		}
	}
	if red == 0 {
		t.Fatal("no insecure badge in the header")
	}
}

func samePixels(a, b *image.RGBA) bool { return bytes.Equal(a.Pix, b.Pix) }

// Metadata can be in any script, with combining marks, emoji and ZWJ
// sequences; the marquee must step through it without breaking runes and
// never draw outside its width.
func TestMarqueeHandlesAnyScript(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	for _, s := range []string{
		"東京事変 — 群青日和 (Live at 日本武道館) 🎸🎸🎸",
		"Şarkı ğüşiöç ĞÜŞİÖÇ — İstanbul’da bir akşam, uzun bir başlık",
		"ééé combining marks, ZWJ 👨‍👩‍👧 and bidi ‮abc‬ end",
	} {
		const x, w = 20, 60
		ta.Replace(&probe{draw: func(a *App, c *gfx.Canvas, area gfx.Rect) {
			a.drawFit(c, a.F.Body, x, area.Y+a.F.Body.Ascent(), w, s, colText, area, true)
		}})
		for step := 0; step < 40; step++ {
			ta.now = ta.now.Add(170 * time.Millisecond)
			c := ta.settle(t)
			top := ta.P.SafeY + ta.P.HeaderH
			for y := top; y < top+ta.F.Body.Height(); y++ {
				for _, px := range []int{x - 1, x + w, x + w + 5} {
					if c.At(px, y) != colBg {
						t.Fatalf("%q step %d: drew outside its width at (%d,%d)", s, step, px, y)
					}
				}
			}
		}
	}
}
