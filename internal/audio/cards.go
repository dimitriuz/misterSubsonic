package audio

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Card is a sound card that can play: a choice for the output.
type Card struct {
	Index int    // 1
	ID    string // the ALSA id: "E30"
	Name  string // what to show: "Topping E30"
}

// Device is the card's native device name (see Native). It names the card
// by its id, so it survives replugging and reboots.
func (c Card) Device() string { return "hw:CARD=" + c.ID + ",DEV=0" }

// CardID is the card a native device name points at: "E30" for
// "hw:CARD=E30,DEV=0" or "hw:E30,0".
func CardID(device string) string {
	card, _, _ := strings.Cut(strings.TrimPrefix(device, "hw:"), ",")
	return strings.TrimPrefix(card, "CARD=")
}

// Cards lists the sound cards that can play, from /proc/asound. MiSTer's
// own Dummy card (behind ALSA's default device) is left out. No /proc/asound
// (not Linux) gives none.
func Cards() []Card {
	b, err := os.ReadFile("/proc/asound/cards")
	if err != nil {
		return nil
	}
	return parseCards(string(b), func(index int) bool {
		m, _ := filepath.Glob("/proc/asound/card" + strconv.Itoa(index) + "/pcm*p")
		return len(m) > 0
	})
}

// parseCards reads /proc/asound/cards: each card is a line
// " 1 [E30            ]: USB-Audio - E30" followed by its long name,
// "Topping E30 at usb-ffb40000.usb-1.2, high speed".
func parseCards(text string, plays func(index int) bool) []Card {
	var out []Card
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		head := strings.TrimSpace(lines[i])
		num, rest, ok := strings.Cut(head, " [")
		if !ok {
			continue
		}
		index, err := strconv.Atoi(num)
		if err != nil {
			continue
		}
		id, rest, ok := strings.Cut(rest, "]: ")
		if !ok {
			continue
		}
		driver, short, _ := strings.Cut(rest, " - ")
		name := strings.TrimSpace(short)
		if i+1 < len(lines) {
			long := strings.TrimSpace(lines[i+1])
			if l, _, found := strings.Cut(long, " at "); found && l != "" {
				name = l
			} else if long != "" && !strings.Contains(long, "[") {
				name = long
			}
		}
		id = strings.TrimSpace(id)
		if id == "" || strings.TrimSpace(driver) == "Dummy" || !plays(index) {
			continue
		}
		out = append(out, Card{Index: index, ID: id, Name: name})
	}
	// Two cards of the same name: the id tells them apart.
	count := map[string]int{}
	for _, c := range out {
		count[c.Name]++
	}
	for i := range out {
		if count[out[i].Name] > 1 {
			out[i].Name += " (" + out[i].ID + ")"
		}
	}
	return out
}
