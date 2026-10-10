package main

import (
	"fmt"
	"slices"
	"sync"

	"mistersubsonic/internal/audio"
)

// openOutput opens the configured device (dev; "" for MiSTer audio). A USB
// DAC that won't open hands over to MiSTer audio, with a notice for the
// screen; the config is left as it is, so the next start tries it again.
// name is the device that opened.
func openOutput(dev string, null bool) (out audio.Output, name, notice string, err error) {
	out, err = openDevice(audio.DeviceOptions{Name: dev, Null: null})
	if err == nil || !audio.Native(dev) {
		return out, dev, "", err
	}
	fallback, ferr := openDevice(audio.DeviceOptions{Null: null})
	if ferr != nil {
		return nil, "", "", err
	}
	id := audio.CardID(dev)
	if i := slices.IndexFunc(audio.Cards(), func(c audio.Card) bool { return c.ID == id }); i >= 0 {
		notice = fmt.Sprintf("%s didn't open (%v): using MiSTer audio", audio.Cards()[i].Name, err)
	} else {
		notice = fmt.Sprintf("%s isn't connected: using MiSTer audio", id)
	}
	return fallback, "", notice, nil
}

// outputSwitcher is Settings → Playback → Output on the engine (ui.AudioOutput).
type outputSwitcher struct {
	eng   *audio.Engine
	cards func() []audio.Card

	mu     sync.Mutex
	active string // the device open now
}

func (o *outputSwitcher) Cards() []audio.Card { return o.cards() }

func (o *outputSwitcher) Active() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.active
}

func (o *outputSwitcher) Switch(device string) error {
	if err := o.eng.SwitchOutput(device); err != nil {
		return err
	}
	o.mu.Lock()
	o.active = device
	o.mu.Unlock()
	return nil
}
