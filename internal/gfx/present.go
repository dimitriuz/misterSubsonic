package gfx

// Display shows frames. Implementations: fbdev (/dev/fb0), headless (tests),
// devview (browser viewer).
type Display interface {
	// Size is the physical pixel size.
	Size() (w, h int)
	// Present shows a full frame of the physical size.
	Present(c *Canvas) error
	Close() error
}

// PartialPresenter is a Display that can update parts of the screen.
type PartialPresenter interface {
	// PresentRects shows the rectangles rs of c, a full-size frame whose
	// other pixels are already on screen.
	PresentRects(c *Canvas, rs []Rect) error
}

// Checker is a Display that can tell whether something else drew over its
// last frame (the MiSTer framebuffer: Main_MiSTer or the console).
type Checker interface {
	Intact() bool
}

// Scaler maps the logical UI canvas onto the physical framebuffer.
// Framebuffers of 288 lines or fewer are CRT modes: the image fills the
// screen and pixels may be non-square by design (a 320x240 UI on a 640x240
// framebuffer). Taller framebuffers get a uniform nearest-neighbour scale,
// centred with black bars.
type Scaler struct {
	dst        *Canvas
	area       Rect
	xmap, ymap []int
}

// CRTMaxLines is the tallest framebuffer treated as a CRT mode.
const CRTMaxLines = 288

func NewScaler(lw, lh, pw, ph int) *Scaler {
	return NewScalerIn(lw, lh, pw, ph, Rect{0, 0, pw, ph})
}

// NewScalerIn is NewScaler for a picture that must stay inside in, a part of
// the pw×ph framebuffer (the overscan margins leave the rest black). Whether
// it is a CRT mode is still the framebuffer's height.
func NewScalerIn(lw, lh, pw, ph int, in Rect) *Scaler {
	s := &Scaler{dst: NewCanvas(pw, ph)}
	w, h := in.W, in.H
	if ph > CRTMaxLines {
		scale := min(float64(in.W)/float64(lw), float64(in.H)/float64(lh))
		w, h = max(int(float64(lw)*scale), 1), max(int(float64(lh)*scale), 1)
	}
	s.area = Rect{in.X + (in.W-w)/2, in.Y + (in.H-h)/2, w, h}
	s.xmap = make([]int, w)
	for i := range s.xmap {
		s.xmap[i] = i * lw / w
	}
	s.ymap = make([]int, h)
	for i := range s.ymap {
		s.ymap[i] = i * lh / h
	}
	return s
}

// Area is where the logical image lands on the physical canvas.
func (s *Scaler) Area() Rect { return s.area }

// Scale renders src (logical) into the physical canvas and returns it.
func (s *Scaler) Scale(src *Canvas) *Canvas {
	d := s.dst
	for y, sy := range s.ymap {
		srow := src.Pix[sy*src.W : (sy+1)*src.W]
		drow := d.Pix[(s.area.Y+y)*d.W+s.area.X:]
		if y > 0 && s.ymap[y-1] == sy {
			prev := d.Pix[(s.area.Y+y-1)*d.W+s.area.X:]
			copy(drow[:len(s.xmap)], prev[:len(s.xmap)])
			continue
		}
		for x, sx := range s.xmap {
			drow[x] = srow[sx]
		}
	}
	return d
}
