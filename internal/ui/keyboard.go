package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// Keyboard is the on-screen keyboard (spec §8.2): letter rows for the
// current layout (Latin or Cyrillic), digits, and a row of Space, Del,
// the layout switch and Clear. Keys are measured in units (a letter is one
// unit, a row is ten); Up/Down move to the key nearest the same place in
// the next row.
type Keyboard struct {
	cyrillic bool
	row, col int
}

type keyAction int

const (
	keyChar keyAction = iota
	keySpace
	keyDel
	keyLayout
	keyClear
)

type kbKey struct {
	label  string
	r      rune
	action keyAction
	units  int
}

const kbUnits = 10 // units per row

var (
	latinRows    = []string{"1234567890", "abcdefghij", "klmnopqrst", "uvwxyz-'.&"}
	cyrillicRows = []string{"1234567890", "абвгдеёжзи", "йклмнопрст", "уфхцчшщъыь", "эюя-'.&"}
)

// The two layouts, built once (rows is called several times per frame).
var latinKeys, cyrillicKeys = buildKeys(latinRows, "АБВ"), buildKeys(cyrillicRows, "ABC")

// buildKeys makes the key rows of a layout; sw labels the layout switch key
// (it names the other layout).
func buildKeys(src []string, sw string) [][]kbKey {
	out := make([][]kbKey, 0, len(src)+1)
	for _, s := range src {
		var row []kbKey
		for _, r := range s {
			row = append(row, kbKey{label: string(r), r: r, units: 1})
		}
		out = append(out, row)
	}
	return append(out, []kbKey{
		{label: "Space", action: keySpace, units: 4},
		{label: "Del", action: keyDel, units: 2},
		{label: sw, action: keyLayout, units: 2},
		{label: "Clear", action: keyClear, units: 2},
	})
}

// rows is the current layout. Callers must not modify it.
func (k *Keyboard) rows() [][]kbKey {
	if k.cyrillic {
		return cyrillicKeys
	}
	return latinKeys
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

// ToggleLayout switches Latin/Cyrillic, keeping the cursor on the switch key.
func (k *Keyboard) ToggleLayout() {
	k.cyrillic = !k.cyrillic
	rows := k.rows()
	k.row, k.col = len(rows)-1, 2
}

// Height is the keyboard's height at key height h.
func (k *Keyboard) Height(h int) int { return len(k.rows()) * h }

// Draw draws the keys into r, unit = r.W/10 wide and h high each; the
// focused key is highlighted when the keyboard has the focus.
func (k *Keyboard) Draw(a *App, c *gfx.Canvas, r gfx.Rect, h int, focused bool) {
	unit := r.W / kbUnits
	gap := max(unit/12, 1)
	f := a.F.Body
	if unit < f.Measure("Space")/3 {
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
