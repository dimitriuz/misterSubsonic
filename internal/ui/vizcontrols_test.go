package ui

import (
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
)

func npApp(t *testing.T) *testApp {
	t.Helper()
	ta, _ := connectedApp(t)
	playingState(ta)
	ta.Push(NewNowPlayingScreen())
	ta.settle(t)
	return ta
}

func hold(ta *testApp, b input.Button, d time.Duration) {
	ta.onInput(input.Event{Button: b, Kind: input.Press})
	ta.now = ta.now.Add(d)
	ta.onWake()
	ta.onInput(input.Event{Button: b, Kind: input.Repeat})
	ta.onInput(input.Event{Button: b, Kind: input.Release})
}

func TestVizStyleNames(t *testing.T) {
	want := []struct {
		s           VizStyle
		name, label string
	}{{VizOff, "off", "Off"}, {VizBars, "bars", "Bars"}, {VizScope, "scope", "Scope"}, {VizVU, "vu", "VU meters"}, {VizWaterfall, "waterfall", "Waterfall"}}
	for _, w := range want {
		if w.s.String() != w.name || w.s.Label() != w.label || ParseVizStyle(w.name) != w.s {
			t.Errorf("%d: %q %q parse %v", w.s, w.s.String(), w.s.Label(), ParseVizStyle(w.name))
		}
	}
	if ParseVizStyle("disco") != VizOff || ParseVizStyle("") != VizOff {
		t.Error("an unknown name is not Off")
	}
}

func TestSelectCyclesTheVisualizerAndSaves(t *testing.T) {
	ta := npApp(t)
	if ta.VizStyle() != VizOff {
		t.Fatalf("starts at %v", ta.VizStyle())
	}
	for _, want := range []VizStyle{VizBars, VizScope, VizVU, VizWaterfall, VizOff} {
		ta.press(input.BtnSelect)
		if ta.VizStyle() != want || ta.cfg.Display.Visualizer != want.String() {
			t.Fatalf("after Select: %v / %q, want %v", ta.VizStyle(), ta.cfg.Display.Visualizer, want)
		}
		if got := ta.toasts[len(ta.toasts)-1].text; got != "Visualizer: "+want.Label() {
			t.Fatalf("toast %q", got)
		}
		ta.settle(t)
		ta.flushConfig()
		waitFile(t, ta.o.ConfigPath, `visualizer = "`+want.String()+`"`)
	}
	if ta.pl.st.Shuffle || ta.pl.st.Repeat != player.RepeatOff {
		t.Fatal("Select still changes the play mode")
	}
}

func TestSelectHoldMutesAndLeavesTheStyle(t *testing.T) {
	ta := npApp(t)
	ta.SetVizStyle(VizScope)
	hold(ta, input.BtnSelect, muteHold)
	if !ta.muted || ta.VizStyle() != VizScope {
		t.Fatalf("muted %v, style %v", ta.muted, ta.VizStyle())
	}
}

func menuLabels(m *MenuScreen) []string {
	var out []string
	for _, e := range m.entries {
		out = append(out, e.label)
	}
	return out
}

func TestShortXOpensTheNowPlayingMenu(t *testing.T) {
	ta := npApp(t)
	ta.press(input.BtnX)
	m, ok := ta.Top().(*MenuScreen)
	if !ok || m.title != "Now Playing" {
		t.Fatalf("X opened %T", ta.Top())
	}
	if got := menuLabels(m); !slices.Equal(got, []string{"Star", "Shuffle: Off", "Repeat: Off"}) {
		t.Fatalf("entries %q", got)
	}
	if len(ta.lib.stars) != 0 {
		t.Fatal("a short X starred")
	}
}

func TestNowPlayingMenuEntriesAct(t *testing.T) {
	ta := npApp(t)
	choose := func(i int) {
		t.Helper()
		ta.press(input.BtnX)
		for range i {
			ta.press(input.BtnDown)
		}
		ta.press(input.BtnA)
		ta.settle(t)
	}
	choose(0)
	if !slices.Equal(ta.lib.stars, []string{"star s1"}) {
		t.Fatalf("stars %v", ta.lib.stars)
	}
	ta.press(input.BtnX)
	if got := menuLabels(ta.Top().(*MenuScreen))[0]; got != "Unstar" {
		t.Fatalf("first entry %q, want Unstar", got)
	}
	ta.press(input.BtnB)
	choose(1)
	if !ta.pl.st.Shuffle {
		t.Fatal("Shuffle entry did not turn shuffle on")
	}
	ta.press(input.BtnX)
	if got := menuLabels(ta.Top().(*MenuScreen))[1]; got != "Shuffle: On" {
		t.Fatalf("second entry %q", got)
	}
	ta.press(input.BtnB)
	for _, want := range []player.Repeat{player.RepeatAll, player.RepeatOne, player.RepeatOff} {
		choose(2)
		if ta.pl.st.Repeat != want {
			t.Fatalf("repeat %v, want %v", ta.pl.st.Repeat, want)
		}
	}
	choose(1)
	if ta.pl.st.Shuffle {
		t.Fatal("Shuffle entry did not turn shuffle off")
	}
}

func TestHoldXStarsAtOnceWithoutTheMenu(t *testing.T) {
	ta := npApp(t)
	np := ta.Top()
	hold(ta, input.BtnX, starHold)
	ta.settle(t)
	if ta.Top() != np {
		t.Fatalf("hold X left %T on top", ta.Top())
	}
	if !slices.Equal(ta.lib.stars, []string{"star s1"}) {
		t.Fatalf("stars %v", ta.lib.stars)
	}
	ta.now = ta.now.Add(3 * starHold)
	ta.onWake() // one hold stars once
	ta.settle(t)
	if len(ta.lib.stars) != 1 {
		t.Fatalf("stars %v", ta.lib.stars)
	}
	hold(ta, input.BtnX, starHold) // a second hold unstars
	ta.settle(t)
	if !slices.Equal(ta.lib.stars, []string{"star s1", "unstar s1"}) {
		t.Fatalf("stars %v", ta.lib.stars)
	}
}

func TestShortXTimerDoesNotStarLater(t *testing.T) {
	ta := npApp(t)
	ta.press(input.BtnX)
	ta.press(input.BtnB) // close the menu
	ta.now = ta.now.Add(2 * starHold)
	ta.onWake()
	ta.settle(t)
	if len(ta.lib.stars) != 0 {
		t.Fatalf("stars %v", ta.lib.stars)
	}
}

func TestXLostReleaseRecovers(t *testing.T) {
	ta := npApp(t)
	np := NewNowPlayingScreen()
	ta.Push(np)
	ta.onInput(input.Event{Button: input.BtnX, Kind: input.Press})
	ta.Push(NewQueueScreen())
	ta.now = ta.now.Add(2 * starHold)
	ta.onWake() // fires under the Queue: no star there
	ta.onInput(input.Event{Button: input.BtnX, Kind: input.Release})
	ta.Pop()
	ta.settle(t)
	if len(ta.lib.stars) != 0 {
		t.Fatalf("stars %v", ta.lib.stars)
	}
	hold(ta, input.BtnX, starHold)
	ta.settle(t)
	if len(ta.lib.stars) != 1 {
		t.Fatalf("hold after a lost release: stars %v", ta.lib.stars)
	}
}

func TestQueueMenuHasShuffleAndRepeat(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.Push(NewHomeScreen())
	ta.Push(NewQueueScreen())
	ta.settle(t)
	ta.press(input.BtnX)
	m := ta.Top().(*MenuScreen)
	if got := menuLabels(m); len(got) != 4 || got[2] != "Shuffle: Off" || got[3] != "Repeat: Off" {
		t.Fatalf("entries %q", got)
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	ta.press(input.BtnA)
	if !ta.pl.st.Shuffle {
		t.Fatal("the queue menu's Shuffle did nothing")
	}
	ta.press(input.BtnX)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	ta.press(input.BtnA)
	if ta.pl.st.Repeat != player.RepeatAll {
		t.Fatalf("repeat %v", ta.pl.st.Repeat)
	}
}

func TestSettingsVisualizerRowCyclesAndSaves(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.Push(newSettingsList("Display", displaySettings))
	s := ta.Top().(*SettingsListScreen)
	for i, r := range s.rows(ta.App) {
		if r.label == "Visualizer" {
			s.list.Focus = i
		}
	}
	rows := s.rows(ta.App)
	if r := rows[s.list.Focus]; r.label != "Visualizer" || r.value(ta.App) != "Off" || r.help == "" {
		t.Fatalf("row %q = %q, help %q", r.label, r.value(ta.App), r.help)
	}
	ta.press(input.BtnRight)
	ta.press(input.BtnRight)
	if ta.VizStyle() != VizScope || ta.cfg.Display.Visualizer != "scope" {
		t.Fatalf("style %v, config %q", ta.VizStyle(), ta.cfg.Display.Visualizer)
	}
	if v := rows[s.list.Focus].value(ta.App); v != "Scope" {
		t.Fatalf("value %q", v)
	}
	ta.press(input.BtnLeft)
	ta.press(input.BtnLeft)
	ta.press(input.BtnLeft) // wraps from Off to Waterfall
	if ta.VizStyle() != VizWaterfall {
		t.Fatalf("style %v", ta.VizStyle())
	}
	ta.flushConfig()
	waitFile(t, ta.o.ConfigPath, `visualizer = "waterfall"`)
}

func TestNowPlayingHints(t *testing.T) {
	ta := npApp(t)
	ta.pad = true
	var got []string
	for _, h := range ta.screenHints() {
		l := h.Label
		if h.Hold {
			l = "hold " + l
		}
		got = append(got, padCap(h.Button)+" "+l)
	}
	want := []string{"A Pause", " Seek", " Volume", "L Prev/Next", "X Menu", "X hold Star", "Y Queue", "Select Visualizer", "Select hold Mute", "B Back"}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("hints %q, want %q", got, want)
	}
	ta.pad = false // Select has no key, so the keyboard bar leaves its hints out
	if _, ok := ta.capFor(input.BtnSelect); ok {
		t.Fatal("Select has a key cap")
	}
}
