package ui

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/player"
)

const e30 = "hw:CARD=E30,DEV=0"

// fakeAudioOutput is an output with cards; Switch records and may fail.
type fakeAudioOutput struct {
	mu       sync.Mutex
	cards    []audio.Card
	active   string
	switches []string
	err      error // the next Switch fails with it
}

func (f *fakeAudioOutput) Cards() []audio.Card { return f.cards }

func (f *fakeAudioOutput) Active() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active
}

func (f *fakeAudioOutput) Switch(device string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.switches = append(f.switches, device)
	if err := f.err; err != nil {
		f.err = nil
		return err
	}
	f.active = device
	return nil
}

func (f *fakeAudioOutput) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.switches...)
}

func withE30() *fakeAudioOutput {
	return &fakeAudioOutput{cards: []audio.Card{{Index: 1, ID: "E30", Name: "Topping E30"}}}
}

// waitSwitch runs posted work until the output switch has finished.
func waitSwitch(t *testing.T, ta *testApp) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for ta.outBusy {
		select {
		case f := <-ta.post:
			f()
		case <-deadline:
			t.Fatal("the output switch did not finish")
		}
	}
}

func toastTexts(ta *testApp) []string {
	var out []string
	for _, t := range ta.toasts {
		out = append(out, t.text)
	}
	return out
}

// outputRow is the Output row of Settings → Playback.
func outputRow(t *testing.T, ta *testApp) setting {
	t.Helper()
	for _, r := range playbackSettings(ta.App) {
		if r.label == "Output" {
			return r
		}
	}
	t.Fatal("no Output row in Settings → Playback")
	return setting{}
}

func TestOutputSwitchesToTheDACAndSaves(t *testing.T) {
	ta, _ := connectedApp(t)
	out := withE30()
	ta.o.AudioOutput = out
	row := outputRow(t, ta)
	if got := row.value(ta.App); got != "MiSTer audio" {
		t.Fatalf("Output reads %q, want MiSTer audio", got)
	}
	row.change(ta.App, 1)
	if got := row.value(ta.App); got != "Switching…" {
		t.Fatalf("Output reads %q while switching", got)
	}
	waitSwitch(t, ta)
	if got := out.calls(); !slices.Equal(got, []string{e30}) {
		t.Fatalf("switches %v, want the E30", got)
	}
	if got := row.value(ta.App); got != "Topping E30" {
		t.Fatalf("Output reads %q, want Topping E30", got)
	}
	if ta.cfg.Playback.ALSADevice != e30 {
		t.Fatalf("alsa_device = %q, want %q", ta.cfg.Playback.ALSADevice, e30)
	}
	row.change(ta.App, 1) // back round to MiSTer audio
	waitSwitch(t, ta)
	if ta.cfg.Playback.ALSADevice != "default" || row.value(ta.App) != "MiSTer audio" {
		t.Fatalf("alsa_device = %q, row %q", ta.cfg.Playback.ALSADevice, row.value(ta.App))
	}
}

func TestOutputFailedSwitchKeepsTheConfig(t *testing.T) {
	ta, _ := connectedApp(t)
	out := withE30()
	out.err = errors.New("device busy")
	ta.o.AudioOutput = out
	outputRow(t, ta).change(ta.App, 1)
	waitSwitch(t, ta)
	if ta.cfg.Playback.ALSADevice != "default" {
		t.Fatalf("alsa_device = %q after a failed switch", ta.cfg.Playback.ALSADevice)
	}
	if got := toastTexts(ta); len(got) != 1 || got[0] != "Can't use Topping E30: device busy" {
		t.Fatalf("toasts %q", got)
	}
}

func TestOutputShowsAMissingDAC(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.cfg.Playback.ALSADevice = e30
	ta.o.AudioOutput = &fakeAudioOutput{} // no cards: the E30 isn't plugged in
	if got := outputRow(t, ta).value(ta.App); got != "MiSTer audio (E30 missing)" {
		t.Fatalf("Output reads %q", got)
	}
}

func TestOutputShowsAPresentDACThatIsNotInUse(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.cfg.Playback.ALSADevice = e30
	ta.o.AudioOutput = withE30() // plugged in, but MiSTer audio is active
	if got := outputRow(t, ta).value(ta.App); got != "MiSTer audio (E30 not in use)" {
		t.Fatalf("Output reads %q", got)
	}
}

func TestOutputNamesTheDACFromTheConfig(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.cfg.Playback.ALSADevice = e30
	out := withE30()
	out.active = e30
	ta.o.AudioOutput = out
	if got := outputRow(t, ta).value(ta.App); got != "Topping E30" {
		t.Fatalf("Output reads %q", got)
	}
}

func TestOutputKeepsAHandWrittenDevice(t *testing.T) {
	ta, _ := connectedApp(t)
	out := withE30()
	out.active = "E30, USB Audio"
	ta.cfg.Playback.ALSADevice = out.active
	ta.o.AudioOutput = out
	row := outputRow(t, ta)
	if got := row.value(ta.App); got != "E30, USB Audio" {
		t.Fatalf("Output reads %q", got)
	}
	row.change(ta.App, 1) // from the hand-written name, on to the first choice
	waitSwitch(t, ta)
	if got := out.calls(); !slices.Equal(got, []string{""}) {
		t.Fatalf("switches %v, want MiSTer audio", got)
	}
}

func TestOutputLostDACFallsBackToMiSTerAudio(t *testing.T) {
	ta, _ := connectedApp(t)
	out := withE30()
	ta.o.AudioOutput = out
	row := outputRow(t, ta)
	row.change(ta.App, 1) // learns the E30's name and switches to it
	waitSwitch(t, ta)

	out.cards = nil // unplugged
	ta.onPlayer(player.Event{Kind: player.DeviceLost})
	waitSwitch(t, ta)
	if got := out.calls(); !slices.Equal(got, []string{e30, ""}) {
		t.Fatalf("switches %v, want back to MiSTer audio", got)
	}
	if got := toastTexts(ta); !slices.Contains(got, "Topping E30 was disconnected: using MiSTer audio") {
		t.Fatalf("toasts %q", got)
	}
	if ta.cfg.Playback.ALSADevice != e30 {
		t.Fatalf("alsa_device = %q: the config keeps the DAC for the next start", ta.cfg.Playback.ALSADevice)
	}
	if got := row.value(ta.App); got != "MiSTer audio (E30 missing)" {
		t.Fatalf("Output reads %q", got)
	}
}

func TestOutputLostWhenTheAppStartedOnTheDACStillNamesIt(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.cfg.Playback.ALSADevice = e30
	out := withE30()
	out.active = e30 // the DAC came from the config; the row was never stepped
	ta.o.AudioOutput = out
	ta.start()
	out.cards = nil // unplugged
	ta.onPlayer(player.Event{Kind: player.DeviceLost})
	waitSwitch(t, ta)
	if got := toastTexts(ta); !slices.Contains(got, "Topping E30 was disconnected: using MiSTer audio") {
		t.Fatalf("toasts %q", got)
	}
}

func TestOutputLostWithTheCardStillThereSaysItStopped(t *testing.T) {
	ta, _ := connectedApp(t)
	out := withE30()
	out.active = e30
	ta.o.AudioOutput = out
	ta.onPlayer(player.Event{Kind: player.DeviceLost}) // a failed rate change: the card is listed
	waitSwitch(t, ta)
	if got := toastTexts(ta); !slices.Contains(got, "Topping E30 stopped working: using MiSTer audio") {
		t.Fatalf("toasts %q", got)
	}
}

func TestOutputLostOnMiSTerAudioOnlySaysSo(t *testing.T) {
	ta, _ := connectedApp(t)
	out := withE30()
	ta.o.AudioOutput = out
	ta.onPlayer(player.Event{Kind: player.DeviceLost})
	if len(out.calls()) != 0 || ta.outBusy {
		t.Fatalf("switches %v: nothing to fall back to", out.calls())
	}
	if got := toastTexts(ta); !slices.Contains(got, "Audio device stopped") {
		t.Fatalf("toasts %q", got)
	}
}

func TestAudioNoticeShowsAtStart(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.o.AudioNotice = "E30 isn't connected: using MiSTer audio"
	ta.start()
	if got := toastTexts(ta); !slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, "E30 isn't connected") }) {
		t.Fatalf("toasts %q", got)
	}
}
