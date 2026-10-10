package audio

import (
	"reflect"
	"testing"
)

func playsAll(int) bool { return true }

func TestParseCardsMiSTerWithAUSBDAC(t *testing.T) {
	const text = ` 0 [Dummy          ]: Dummy - Dummy
                      Dummy 1
 1 [E30            ]: USB-Audio - E30
                      Topping E30 at usb-ffb40000.usb-1.2, high speed
`
	got := parseCards(text, playsAll)
	want := []Card{{Index: 1, ID: "E30", Name: "Topping E30"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if d := got[0].Device(); d != "hw:CARD=E30,DEV=0" {
		t.Errorf("Device() = %q", d)
	}
}

func TestParseCardsDesktop(t *testing.T) {
	const text = ` 0 [HDMI           ]: HDA-Intel - HDA ATI HDMI
                      HDA ATI HDMI at 0xfcd60000 irq 106
 1 [Generic        ]: HDA-Intel - HD-Audio Generic
                      HD-Audio Generic at 0xfcd00000 irq 108
 2 [Mic            ]: USB-Audio - USB Microphone
                      USB Microphone at usb-0000:0c:00.3-2, full speed
`
	got := parseCards(text, func(i int) bool { return i != 2 }) // the mic doesn't play
	want := []Card{
		{Index: 0, ID: "HDMI", Name: "HDA ATI HDMI"},
		{Index: 1, ID: "Generic", Name: "HD-Audio Generic"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseCardsSameNameShowsTheID(t *testing.T) {
	const text = ` 1 [DAC            ]: USB-Audio - USB DAC
                      Generic USB DAC at usb-1, high speed
 2 [DAC_1          ]: USB-Audio - USB DAC
                      Generic USB DAC at usb-2, high speed
`
	got := parseCards(text, playsAll)
	if len(got) != 2 || got[0].Name != "Generic USB DAC (DAC)" || got[1].Name != "Generic USB DAC (DAC_1)" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseCardsEmptyAndJunk(t *testing.T) {
	for _, text := range []string{"", "--- no soundcards ---\n", "garbage [x]: y\n"} {
		if got := parseCards(text, playsAll); len(got) != 0 {
			t.Errorf("%q: got %+v, want none", text, got)
		}
	}
}
