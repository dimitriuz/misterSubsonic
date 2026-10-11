package audio

import (
	"errors"
	"math"
	"testing"
)

// fakeControl is a hardware volume with steps of step dB from min to max.
type fakeControl struct {
	min, max, step float64
	level          float64
	hasSwitch      bool
	on             bool
	sets           []float64
	switches       []bool
	err            error
}

func (f *fakeControl) Range() (float64, float64) { return f.min, f.max }

func (f *fakeControl) Set(db float64) (float64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.sets = append(f.sets, db)
	steps := math.Ceil((db-f.min)/f.step - 1e-9) // at or above
	f.level = math.Min(f.max, f.min+steps*f.step)
	return f.level, nil
}

func (f *fakeControl) HasSwitch() bool { return f.hasSwitch }

func (f *fakeControl) SetSwitch(on bool) error {
	f.switches = append(f.switches, on)
	f.on = on
	return nil
}

func linearDB(db float64) float32 { return float32(math.Pow(10, db/20)) }

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-5 }

func TestHWVolumeWholeDBStepsLeaveSoftwareAtUnity(t *testing.T) {
	c := &fakeControl{min: -127, max: 0, step: 1}
	h := &hwVolume{ctl: c}
	for _, db := range []float64{0, -1, -10, -37, -60} {
		soft, err := h.apply(linearDB(db))
		if err != nil {
			t.Fatal(err)
		}
		if !near(soft, 1) || c.level != db {
			t.Errorf("%v dB: hardware %v, software %v; want %v and 1", db, c.level, soft, db)
		}
	}
}

func TestHWVolumeCoarseStepsMakeUpTheRestInSoftware(t *testing.T) {
	c := &fakeControl{min: -60, max: 0, step: 3}
	h := &hwVolume{ctl: c}
	soft, err := h.apply(linearDB(-10)) // the hardware goes to -9, software takes -1
	if err != nil {
		t.Fatal(err)
	}
	if c.level != -9 || !near(soft, linearDB(-1)) {
		t.Errorf("hardware %v, software %v; want -9 and -1 dB", c.level, soft)
	}
}

func TestHWVolumeBelowTheRangeGoesToSoftware(t *testing.T) {
	c := &fakeControl{min: -40, max: 0, step: 0.5}
	h := &hwVolume{ctl: c}
	soft, err := h.apply(linearDB(-50))
	if err != nil {
		t.Fatal(err)
	}
	if c.level != -40 || !near(soft, linearDB(-10)) {
		t.Errorf("hardware %v, software %v; want -40 and -10 dB", c.level, soft)
	}
}

func TestHWVolumeNeverAmplifies(t *testing.T) {
	c := &fakeControl{min: -60, max: -6, step: 1} // a control that can't reach 0 dB
	h := &hwVolume{ctl: c}
	soft, err := h.apply(1)
	if err != nil {
		t.Fatal(err)
	}
	if c.level != -6 || !near(soft, 1) {
		t.Errorf("hardware %v, software %v; want -6 and 1", c.level, soft)
	}
}

func TestHWVolumeMuteUsesTheSwitchAndComesBack(t *testing.T) {
	c := &fakeControl{min: -127, max: 0, step: 1, hasSwitch: true, on: true}
	h := &hwVolume{ctl: c}
	if soft, _ := h.apply(0); soft != 0 || c.on {
		t.Fatalf("mute: switch on=%v, software %v", c.on, soft)
	}
	c.sets = nil
	if soft, _ := h.apply(linearDB(-20)); !near(soft, 1) || !c.on || c.level != -20 {
		t.Fatalf("unmute: switch on=%v, level %v, software %v", c.on, c.level, soft)
	}
	if len(c.sets) != 1 || len(c.switches) != 2 {
		t.Errorf("the level is set before the switch goes on: sets %v, switches %v", c.sets, c.switches)
	}
}

func TestHWVolumeMuteWithoutASwitchGoesToTheBottom(t *testing.T) {
	c := &fakeControl{min: -90, max: 0, step: 1}
	h := &hwVolume{ctl: c}
	if soft, _ := h.apply(0); soft != 0 || c.level != -90 {
		t.Errorf("hardware %v, software %v; want -90 and 0", c.level, soft)
	}
}

func TestHWVolumeTurnsOnASwitchLeftOff(t *testing.T) {
	c := &fakeControl{min: -127, max: 0, step: 1, hasSwitch: true, on: false}
	h := &hwVolume{ctl: c, muted: c.HasSwitch()} // as the device makes it
	if _, err := h.apply(1); err != nil || !c.on {
		t.Errorf("switch on=%v, err %v", c.on, err)
	}
}

func TestHWVolumeErrorsComeBack(t *testing.T) {
	boom := errors.New("boom")
	h := &hwVolume{ctl: &fakeControl{min: -60, max: 0, step: 1, err: boom}}
	if _, err := h.apply(0.5); !errors.Is(err, boom) {
		t.Errorf("err %v", err)
	}
}

func TestMixerCard(t *testing.T) {
	for in, want := range map[string]string{
		"hw:CARD=E30,DEV=0": "hw:E30",
		"hw:E30,0":          "hw:E30",
		"hw:1,0":            "hw:1",
		"hw:CARD=E30":       "hw:E30",
	} {
		if got := mixerCard(in); got != want {
			t.Errorf("mixerCard(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeviceVolumeMovesToTheDACsControl(t *testing.T) {
	present := false
	opened := 0
	ctl := &fakeControl{min: -127, max: 0, step: 1, hasSwitch: true}
	oldPresent, oldOpen := cardPresent, openMixer
	cardPresent = func(string) bool { return present }
	openMixer = func(card string) (hwControl, error) {
		opened++
		if card != "hw:E30" {
			t.Errorf("mixer opened for %q, want hw:E30", card)
		}
		return ctl, nil
	}
	defer func() { cardPresent, openMixer = oldPresent, oldOpen }()

	d := &device{linear: linearDB(-12)}
	d.useMixer("hw:CARD=E30,DEV=0") // unplugged: no mixer, and no message about one
	if opened != 0 || d.hw != nil {
		t.Fatalf("a missing card's mixer was opened (%d)", opened)
	}
	present = true
	d.useMixer("hw:CARD=E30,DEV=0") // plugged in now: tried again
	if opened != 1 || d.hw == nil || ctl.level != -12 || !ctl.on {
		t.Fatalf("opened %d, level %v, switch %v; want the DAC at -12 dB and on", opened, ctl.level, ctl.on)
	}
	d.SetVolume(linearDB(-30))
	if ctl.level != -30 {
		t.Fatalf("level %v after SetVolume, want -30", ctl.level)
	}
	d.useMixer("") // MiSTer audio: software volume, the DAC left as it is
	if d.hw != nil || ctl.level != -30 {
		t.Fatalf("hw %v, level %v", d.hw, ctl.level)
	}
}
