package ui

import (
	"slices"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/config"
)

// AudioOutput switches where the sound goes (Settings → Playback → Output).
// cmd/mistersubsonic implements it on the audio engine; it is nil without a
// sound device.
type AudioOutput interface {
	// Cards lists the sound cards that can play now.
	Cards() []audio.Card
	// Active is the device playing now: "" for MiSTer audio, a hw: name for
	// a card, or a name from the config as it was written.
	Active() string
	// Switch moves playback to device ("" for MiSTer audio). It takes up to
	// about half a second: call it off the UI goroutine.
	Switch(device string) error
}

const outputHelp = "Where the sound goes: MiSTer audio, or a USB DAC at each song's own sample rate and with its own volume."

// outputSetting is the Output row of Settings → Playback.
func outputSetting() setting {
	return setting{label: "Output", value: (*App).outputValue, change: (*App).changeOutput, help: outputHelp}
}

// outputLabel names a device for the screen.
func (a *App) outputLabel(device string) string {
	switch {
	case device == "" || device == "default":
		return "MiSTer audio"
	case !audio.Native(device):
		return device
	}
	if name, ok := a.outNames[device]; ok {
		return name
	}
	a.learnOutputNames()
	if name, ok := a.outNames[device]; ok {
		return name
	}
	return audio.CardID(device)
}

// learnOutputNames remembers the names of the cards present now, so that a
// card is still named after it was unplugged.
func (a *App) learnOutputNames() {
	if a.o.AudioOutput == nil {
		return
	}
	for _, c := range a.o.AudioOutput.Cards() {
		if a.outNames == nil {
			a.outNames = map[string]string{}
		}
		a.outNames[c.Device()] = c.Name
	}
}

// cardPresent is whether the card behind a native device is listed now.
func (a *App) cardPresent(device string) bool {
	out := a.o.AudioOutput
	if out == nil {
		return false
	}
	id := audio.CardID(device)
	return slices.ContainsFunc(out.Cards(), func(c audio.Card) bool { return c.ID == id })
}

// outputValue is what the Output row shows: what plays now, and the card
// the config asks for when it is missing.
func (a *App) outputValue() string {
	out := a.o.AudioOutput
	if out == nil {
		return "None"
	}
	if a.outBusy {
		return "Switching…"
	}
	act := out.Active()
	label := a.outputLabel(act)
	if want := a.wantedOutput(); audio.Native(want) && want != act {
		state := " not in use)"
		if !a.cardPresent(want) {
			state = " missing)"
		}
		label += " (" + audio.CardID(want) + state
	}
	return label
}

// wantedOutput is the device the config asks for ("" for MiSTer audio).
func (a *App) wantedOutput() string {
	if a.cfg == nil || a.cfg.Playback.ALSADevice == "default" {
		return ""
	}
	return a.cfg.Playback.ALSADevice
}

// changeOutput steps to the next output and switches to it; the config
// keeps the choice once the switch worked.
func (a *App) changeOutput(dir int) {
	out := a.o.AudioOutput
	if out == nil || a.outBusy {
		return
	}
	act := out.Active()
	choices := []string{""}
	a.learnOutputNames()
	for _, c := range out.Cards() {
		choices = append(choices, c.Device())
	}
	if !contains(choices, act) {
		choices = append(choices, act) // a name from the config, written by hand
	}
	next := cycle(choices, act, dir)
	if next == act {
		return
	}
	a.switchOutput(next, func(err error) {
		if err != nil {
			a.Toast("Can't use %s: %v", a.outputLabel(next), err)
			return
		}
		saved := next
		if saved == "" {
			saved = "default"
		}
		a.UpdateConfig(func(c *config.Config) { c.Playback.ALSADevice = saved }, false)
	})
}

// switchOutput runs out.Switch off the UI goroutine; done runs on it after.
func (a *App) switchOutput(device string, done func(error)) {
	a.outBusy, a.dirty = true, true
	out := a.o.AudioOutput
	go func() {
		err := out.Switch(device)
		a.Post(func() {
			a.outBusy, a.dirty = false, true
			done(err)
		})
	}()
}

// outputLost is the player's DeviceLost: a card that went away hands the
// sound to MiSTer audio. The config keeps the card for the next start.
func (a *App) outputLost() {
	out := a.o.AudioOutput
	if out == nil {
		return
	}
	act := out.Active()
	if !audio.Native(act) {
		a.Toast("Audio device stopped")
		return
	}
	if a.outBusy {
		return // a switch is under way; it reports for itself
	}
	name := a.outputLabel(act)
	// A card still listed didn't unplug: its device stopped (a failed
	// reopen at a new sample rate, say).
	what := "was disconnected"
	if a.cardPresent(act) {
		what = "stopped working"
	}
	a.switchOutput("", func(err error) {
		if err != nil {
			a.Toast("%s %s, and MiSTer audio didn't open: %v", name, what, err)
			return
		}
		a.Toast("%s %s: using MiSTer audio", name, what)
	})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
