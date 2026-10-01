package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// Keyboard is the on-screen keyboard (spec §8.2): rows of characters for
// the current layout and a bottom row of actions. Search uses Latin and
// Cyrillic layouts (the zero Keyboard); the setup wizard uses lower case,
// upper case and symbols (NewTextKeyboard), optionally with a row of
// presets on top. Keys are measured in units (a character is one unit, a
// row is ten); Up/Down move to the key nearest the same place in the next
// row.
type Keyboard struct {
	layouts  [][][]kbKey // nil: the search layouts
	cur      int         // current layout
	row, col int
}

type keyAction int

const (
	keyChar keyAction = iota
	keySpace
	keyDel
	keyLayout // switch to layout kbKey.to
	keyClear
	keyNext   // wizard: done with this field
	keyInsert // type kbKey.text (URL presets)
	keyShow   // wizard: show or hide the password
)

type kbKey struct {
	label  string
	r      rune
	action keyAction
	units  int
	to     int    // keyLayout: the layout to switch to
	text   string // keyInsert
}

const kbUnits = 10 // units per row

var (
	latinRows    = []string{"1234567890", "abcdefghij", "klmnopqrst", "uvwxyz-'.&"}
	cyrillicRows = []string{"1234567890", "абвгдеёжзи", "йклмнопрст", "уфхцчшщъыь", "эюя-'.&"}

	lowerRows  = []string{"1234567890", "abcdefghij", "klmnopqrst", "uvwxyz.-_@"}
	upperRows  = []string{"1234567890", "ABCDEFGHIJ", "KLMNOPQRST", "UVWXYZ.-_@"}
	symbolRows = []string{"!@#$%^&*()", "-_=+[]{}\\|", ";:'\",.<>/?", "`~"}
)

// The layouts are built once (rows is called several times per frame).
var searchLayouts = [][][]kbKey{
	buildKeys(latinRows, []kbKey{space(4), del(2), layoutKey("АБВ", 1), clearKey(2)}),
	buildKeys(cyrillicRows, []kbKey{space(4), del(2), layoutKey("ABC", 0), clearKey(2)}),
}

func space(u int) kbKey                { return kbKey{label: "Space", action: keySpace, units: u} }
func del(u int) kbKey                  { return kbKey{label: "Del", action: keyDel, units: u} }
func clearKey(u int) kbKey             { return kbKey{label: "Clear", action: keyClear, units: u} }
func layoutKey(l string, to int) kbKey { return kbKey{label: l, action: keyLayout, units: 2, to: to} }

// buildKeys makes a layout: one row of keys per string, then the bottom row.
func buildKeys(src []string, bottom []kbKey) [][]kbKey {
	out := make([][]kbKey, 0, len(src)+1)
	for _, s := range src {
		var row []kbKey
		for _, r := range s {
			row = append(row, kbKey{label: string(r), r: r, units: 1})
		}
		out = append(out, row)
	}
	return append(out, bottom)
}

// NewTextKeyboard is the wizard's keyboard: lower case, upper case and
// symbols, each with Space, Del, the two switches and Next. A non-empty
// extra row (presets like "https://") goes on top of every layout.
func NewTextKeyboard(extra ...kbKey) Keyboard {
	next := kbKey{label: "Next", action: keyNext, units: 2}
	layouts := [][][]kbKey{
		buildKeys(lowerRows, []kbKey{space(2), del(2), layoutKey("ABC", 1), layoutKey("#+=", 2), next}),
		buildKeys(upperRows, []kbKey{space(2), del(2), layoutKey("abc", 0), layoutKey("#+=", 2), next}),
		buildKeys(symbolRows, []kbKey{space(2), del(2), layoutKey("abc", 0), layoutKey("ABC", 1), next}),
	}
	if len(extra) > 0 {
		for i, l := range layouts {
			layouts[i] = append([][]kbKey{extra}, l...)
		}
	}
	return Keyboard{layouts: layouts}
}

// Presets for the URL field.
var urlPresets = []kbKey{
	{label: "http://", action: keyInsert, text: "http://", units: 3},
	{label: "https://", action: keyInsert, text: "https://", units: 3},
	{label: ":4533", action: keyInsert, text: ":4533", units: 2},
	{label: ":", r: ':', units: 1},
	{label: "/", r: '/', units: 1},
}

// rows is the current layout. Callers must not modify it.
func (k *Keyboard) rows() [][]kbKey {
	if k.layouts == nil {
		return searchLayouts[k.cur]
	}
	return k.layouts[k.cur]
}

// Switch shows layout i (a layout key's target). A cursor on the bottom
// row stays on it (layouts differ in height; the switch keys sit in the
// same place of every bottom row).
func (k *Keyboard) Switch(i int) {
	bottom := k.row == len(k.rows())-1
	k.cur = i
	if bottom {
		k.row = len(k.rows()) - 1
	}
	k.Focused()
}

// Focused is the key under the cursor.
func (k *Keyboard) Focused() kbKey {
	rows := k.rows()
	k.row = min(k.row, len(rows)-1)
	k.col = min(k.col, len(rows[k.row])-1)
	return rows[k.row][k.col]
}

// centre is the middle of key (row, col) in half-units, for Up/Down.
func centre(row []kbKey, col int) int {
	x := 0
	for _, key := range row[:col] {
		x += key.units
	}
	return 2*x + row[col].units
}

// Move handles the arrows; it returns false at an edge (Up on the top row,
// Down on the bottom one, Left or Right at a row's end) so the screen can
// move the focus elsewhere.
func (k *Keyboard) Move(e input.Event) bool {
	rows := k.rows()
	k.Focused() // clamp
	switch e.Button {
	case input.BtnLeft:
		if k.col == 0 {
			return false
		}
		k.col--
	case input.BtnRight:
		if k.col == len(rows[k.row])-1 {
			return false
		}
		k.col++
	case input.BtnUp, input.BtnDown:
		next := k.row - 1
		if e.Button == input.BtnDown {
			next = k.row + 1
		}
		if next < 0 || next >= len(rows) {
			return false
		}
		at := centre(rows[k.row], k.col)
		best, bestD := 0, 1<<30
		for i := range rows[next] {
			d := centre(rows[next], i) - at
			if d < 0 {
				d = -d
			}
			if d < bestD {
				best, bestD = i, d
			}
		}
		k.row, k.col = next, best
	default:
		return false
	}
	return true
}

// Height is the keyboard's height at key height h.
func (k *Keyboard) Height(h int) int { return len(k.rows()) * h }

// Draw draws the keys into r, unit = r.W/10 wide and h high each; the
// focused key is highlighted when the keyboard has the focus.
func (k *Keyboard) Draw(a *App, c *gfx.Canvas, r gfx.Rect, h int, focused bool) {
	unit := r.W / kbUnits
	gap := max(unit/12, 1)
	f := a.F.Body
	if unit < f.Measure("Space")/3 || h-2*gap < f.Ascent() { // narrow keys, or too short for the letters
		f = a.F.Small
	}
	k.Focused()
	for ri, row := range k.rows() {
		x := r.X
		for ci, key := range row {
			w := key.units * unit
			cell := gfx.R(x+gap, r.Y+ri*h+gap, w-2*gap, h-2*gap)
			bg, fg := colPanel, colText
			if focused && ri == k.row && ci == k.col {
				bg, fg = colAccent, colBg
			} else if key.action != keyChar {
				fg = colAccent
			}
			c.Fill(cell, bg)
			lw := f.Measure(key.label)
			f.Draw(c, cell.X+(cell.W-lw)/2, cell.Y+(cell.H+f.Ascent()-f.Descent())/2, key.label, fg, cell)
			x += w
		}
	}
}
