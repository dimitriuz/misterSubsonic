package ui

import "mistersubsonic/internal/config"

// VizStyle is the visualizer's look (spec §3.2); VizOff draws nothing.
type VizStyle int

const (
	VizOff VizStyle = iota
	VizBars
	VizScope
	VizVU
	VizWaterfall
)

var vizNames = [...]string{"off", "bars", "scope", "vu", "waterfall"}
var vizLabels = [...]string{"Off", "Bars", "Scope", "VU meters", "Waterfall"}

// ParseVizStyle reads a config name; anything unknown is Off.
func ParseVizStyle(s string) VizStyle {
	for i, n := range vizNames {
		if n == s {
			return VizStyle(i)
		}
	}
	return VizOff
}

// String is the name in the config file.
func (v VizStyle) String() string { return vizNames[v] }

// Label is the name shown to the user.
func (v VizStyle) Label() string { return vizLabels[v] }

// VizStyle is the saved style (Off with no config).
func (a *App) VizStyle() VizStyle {
	if a.cfg == nil {
		return VizOff
	}
	return ParseVizStyle(a.cfg.Display.Visualizer)
}

// SetVizStyle changes the style and saves it, like the other settings.
func (a *App) SetVizStyle(v VizStyle) {
	a.UpdateConfig(func(c *config.Config) { c.Display.Visualizer = v.String() }, false)
	a.dirty = true
}
