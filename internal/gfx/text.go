package gfx

import (
	"embed"
	"fmt"
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

//go:embed fonts/NotoSans-Regular.ttf fonts/NotoSans-Bold.ttf
var fontFS embed.FS

// Typeface is a list of fonts searched in order for each rune (the bundled
// Noto Sans first, then user fallbacks such as a CJK font).
type Typeface struct {
	fonts []*sfnt.Font
}

// LoadTypeface parses the bundled font (bold or regular) plus every .ttf /
// .otf in fallbackDir (which may be "" or missing).
func LoadTypeface(bold bool, fallbackDir string) (*Typeface, error) {
	name := "fonts/NotoSans-Regular.ttf"
	if bold {
		name = "fonts/NotoSans-Bold.ttf"
	}
	b, err := fontFS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	f, err := opentype.Parse(b)
	if err != nil {
		return nil, fmt.Errorf("gfx: parse %s: %w", name, err)
	}
	t := &Typeface{fonts: []*sfnt.Font{f}}
	if fallbackDir != "" {
		entries, _ := os.ReadDir(fallbackDir)
		for _, e := range entries {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".ttf" && ext != ".otf" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(fallbackDir, e.Name()))
			if err != nil {
				continue
			}
			if ff, err := opentype.Parse(data); err == nil {
				t.fonts = append(t.fonts, ff)
			}
		}
	}
	return t, nil
}

type glyph struct {
	mask    *image.Alpha
	off     image.Point // mask origin relative to the pen position on the baseline
	advance int
}

// Font is a Typeface at one pixel size, with a glyph cache. Not safe for
// concurrent use; the UI draws from one goroutine.
type Font struct {
	faces   []font.Face
	fonts   []*sfnt.Font
	cache   map[rune]*glyph
	ascent  int
	descent int
	buf     sfnt.Buffer
}

// Face returns t at size px (pixel em height).
func (t *Typeface) Face(px int) (*Font, error) {
	f := &Font{cache: map[rune]*glyph{}}
	for i, sf := range t.fonts {
		face, err := opentype.NewFace(sf, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			if i == 0 {
				return nil, err
			}
			continue // a broken fallback font must not disable all text
		}
		f.faces = append(f.faces, face)
		f.fonts = append(f.fonts, sf)
	}
	m := f.faces[0].Metrics()
	f.ascent, f.descent = m.Ascent.Ceil(), m.Descent.Ceil()
	return f, nil
}

// Ascent, Descent and Height are in pixels.
func (f *Font) Ascent() int  { return f.ascent }
func (f *Font) Descent() int { return f.descent }
func (f *Font) Height() int  { return f.ascent + f.descent }

// faceFor returns the first face that has r, or nil if none does.
func (f *Font) faceFor(r rune) font.Face {
	for i, sf := range f.fonts {
		if idx, err := sf.GlyphIndex(&f.buf, r); err == nil && idx != 0 {
			return f.faces[i]
		}
	}
	return nil
}

// tofu is the outlined box drawn for characters no font covers (Noto's own
// .notdef glyph is empty, which would make e.g. Japanese titles vanish).
func (f *Font) tofu() *glyph {
	w, h := max(f.ascent*5/9, 3), max(f.ascent*3/4, 4)
	m := image.NewAlpha(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x == 0 || y == 0 || x == w-1 || y == h-1 {
				m.Pix[y*m.Stride+x] = 0xFF
			}
		}
	}
	return &glyph{mask: m, off: image.Pt(1, -h), advance: w + 2}
}

func (f *Font) glyph(r rune) *glyph {
	if g, ok := f.cache[r]; ok {
		return g
	}
	face := f.faceFor(r)
	if face == nil {
		if r == ' ' || r < 0x20 {
			face = f.faces[0]
		} else {
			g := f.tofu()
			f.cache[r] = g
			return g
		}
	}
	g := &glyph{}
	dr, mask, maskp, adv, ok := face.Glyph(fixed.P(0, 0), r)
	g.advance = adv.Round()
	if ok && !dr.Empty() {
		m := image.NewAlpha(image.Rect(0, 0, dr.Dx(), dr.Dy()))
		draw.Draw(m, m.Bounds(), mask, maskp, draw.Src)
		g.mask, g.off = m, dr.Min
	}
	f.cache[r] = g
	return g
}

// Measure returns the advance width of s in pixels.
func (f *Font) Measure(s string) int {
	w := 0
	for _, r := range s {
		w += f.glyph(r).advance
	}
	return w
}

// Draw renders s with its baseline at y, starting at x, clipped to clip.
// It returns the x after the last glyph.
func (f *Font) Draw(c *Canvas, x, y int, s string, col Color, clip Rect) int {
	clip = clip.Intersect(c.Clip())
	v := uint32(col) & 0xFFFFFF
	alpha := col.A()
	for _, r := range s {
		g := f.glyph(r)
		if g.mask != nil {
			gx, gy := x+g.off.X, y+g.off.Y
			area := Rect{gx, gy, g.mask.Rect.Dx(), g.mask.Rect.Dy()}.Intersect(clip)
			for py := area.Y; py < area.Bottom(); py++ {
				mrow := g.mask.Pix[(py-gy)*g.mask.Stride:]
				crow := c.Pix[py*c.W:]
				for px := area.X; px < area.Right(); px++ {
					a := uint32(mrow[px-gx]) * alpha / 255
					switch a {
					case 0:
					case 255:
						crow[px] = v
					default:
						crow[px] = blend(crow[px], v, a)
					}
				}
			}
		}
		x += g.advance
	}
	return x
}

// Truncate shortens s with a trailing "…" so it fits in maxW pixels.
func (f *Font) Truncate(s string, maxW int) string {
	if f.Measure(s) <= maxW {
		return s
	}
	ell := f.Measure("…")
	w := 0
	for i, r := range s {
		adv := f.glyph(r).advance
		if w+adv+ell > maxW {
			return s[:i] + "…"
		}
		w += adv
	}
	return s
}

// Marquee returns the visible part of s scrolled by offset pixels, for
// focused rows whose text doesn't fit. The text loops with a gap.
func (f *Font) Marquee(s string, offset int) (string, int) {
	loop := s + "     " + s
	skip := 0
	for len(loop) > 0 {
		r, size := utf8.DecodeRuneInString(loop)
		adv := f.glyph(r).advance
		if skip+adv > offset {
			break
		}
		skip += adv
		loop = loop[size:]
	}
	return loop, skip - offset
}
