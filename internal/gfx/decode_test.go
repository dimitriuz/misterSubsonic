package gfx

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"runtime"
	"strings"
	"testing"
)

// slowFromImage is the reference conversion: every pixel through At.
func slowFromImage(m image.Image) *Image {
	b := m.Bounds()
	out := NewImage(b.Dx(), b.Dy())
	for y := 0; y < out.H; y++ {
		for x := 0; x < out.W; x++ {
			c := color.NRGBAModel.Convert(m.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			out.Pix[y*out.W+x] = uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
		}
	}
	return out
}

func near(a, b uint32) bool {
	for s := 0; s < 32; s += 8 {
		d := int(a>>s&0xFF) - int(b>>s&0xFF)
		if d < -1 || d > 1 {
			return false
		}
	}
	return true
}

func samePixels(t *testing.T, name string, got, want *Image) {
	t.Helper()
	if got.W != want.W || got.H != want.H {
		t.Fatalf("%s: %dx%d, want %dx%d", name, got.W, got.H, want.W, want.H)
	}
	for i := range want.Pix {
		if !near(got.Pix[i], want.Pix[i]) {
			t.Fatalf("%s: pixel %d = %08x, want %08x", name, i, got.Pix[i], want.Pix[i])
		}
	}
}

// testImages covers every fast path and the At fallback, each with a
// non-zero origin (sub-images) and translucent pixels where the type has alpha.
func testImages() map[string]image.Image {
	const w, h = 37, 23
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	gray := image.NewGray(image.Rect(0, 0, w, h))
	pal := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White, color.NRGBA{200, 10, 90, 128}})
	for y := range h {
		for x := range w {
			c := color.NRGBA{uint8(x * 7), uint8(y * 11), uint8(x*y + 3), uint8(40 + x*5)}
			nrgba.SetNRGBA(x, y, c)
			rgba.Set(x, y, c)
			gray.SetGray(x, y, color.Gray{uint8(x*6 + y)})
			pal.SetColorIndex(x, y, uint8((x+y)%3))
		}
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, nrgba, &jpeg.Options{Quality: 90})
	ycc, _ := jpeg.Decode(&buf)
	sub := func(m image.Image) image.Image {
		return m.(interface {
			SubImage(image.Rectangle) image.Image
		}).SubImage(image.Rect(3, 2, w, h))
	}
	return map[string]image.Image{"ycbcr": ycc, "nrgba": nrgba, "rgba": rgba, "gray": gray, "paletted": pal,
		"ycbcr sub": sub(ycc), "nrgba sub": sub(nrgba), "rgba sub": sub(rgba), "gray sub": sub(gray)}
}

func TestFromImageMatchesAt(t *testing.T) {
	for name, m := range testImages() {
		samePixels(t, name, FromImage(m), slowFromImage(m))
	}
}

func TestDecodeScalesStraightFromTheDecoder(t *testing.T) {
	for name, m := range testImages() {
		var buf bytes.Buffer
		if err := png.Encode(&buf, m); err != nil {
			t.Fatal(err)
		}
		got, err := DecodeImage(buf.Bytes(), 10, 10)
		if err != nil {
			t.Fatal(name, err)
		}
		decoded, _ := png.Decode(bytes.NewReader(buf.Bytes()))
		samePixels(t, name, got, Fit(slowFromImage(decoded), 10, 10))
	}
}

// A big cover is scaled without converting it at full size first.
func TestDecodeDoesNotConvertAtFullSize(t *testing.T) {
	big := image.NewNRGBA(image.Rect(0, 0, 2000, 2000))
	for i := range big.Pix {
		big.Pix[i] = uint8(i)
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, big, &jpeg.Options{Quality: 80})
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	img, err := DecodeImage(buf.Bytes(), 100, 100)
	runtime.ReadMemStats(&after)
	if err != nil || img.W != 100 || img.H != 100 {
		t.Fatalf("%v %v", img, err)
	}
	// The JPEG decoder's own YCbCr (4:2:0) is 6 MB; a full-size Image would add 16 MB more.
	if got := after.TotalAlloc - before.TotalAlloc; got > 10<<20 {
		t.Fatalf("decoding allocated %d MB", got>>20)
	}
}

// pngHeader is a PNG with only its signature and IHDR, which is all
// DecodeConfig reads: a picture of any size without its pixels.
func pngHeader(w, h uint32, colorType byte) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, colorType // bit depth 8
	chunk := append([]byte("IHDR"), ihdr...)
	out := []byte("\x89PNG\r\n\x1a\n")
	out = binary.BigEndian.AppendUint32(out, 13)
	out = append(out, chunk...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
}

func TestDecodeRefusesImagesTooBigToDecode(t *testing.T) {
	// 3600² RGBA is 52 MB decoded.
	if _, err := DecodeImage(pngHeader(3600, 3600, 6), 100, 100); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err %v", err)
	}
	// A 4096² JPEG (YCbCr, 48 MiB as 4:4:4) is still accepted, and so is 3400² RGBA.
	for _, cfg := range []image.Config{{ColorModel: color.YCbCrModel, Width: 4096, Height: 4096},
		{ColorModel: color.NRGBAModel, Width: 3400, Height: 3400}} {
		if decodedBytes(cfg) > MaxDecodeBytes {
			t.Errorf("%dx%d %T refused", cfg.Width, cfg.Height, cfg.ColorModel)
		}
	}
	if decodedBytes(image.Config{ColorModel: color.NRGBA64Model, Width: 2600, Height: 2600}) <= MaxDecodeBytes {
		t.Error("a 2600² 16-bit PNG (54 MB) accepted")
	}
}

// Box sums are 64-bit: shrinking 90000 opaque white pixels into one stays white.
func TestResizeBigBoxesDoNotOverflow(t *testing.T) {
	src := NewImage(300, 300)
	for i := range src.Pix {
		src.Pix[i] = 0xFFFFFFFF
	}
	if got := Resize(src, 1, 1).Pix[0]; got != 0xFFFFFFFF {
		t.Fatalf("got %08x", got)
	}
}

// jpegBytes encodes a w×h JPEG (4:2:0, three components).
func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// patchSOF sets the frame marker's type byte (0xC0 baseline, 0xC2 progressive)
// and its dimensions.
func patchSOF(t *testing.T, data []byte, marker byte, w, h int) []byte {
	t.Helper()
	out := append([]byte(nil), data...)
	i := bytes.Index(out, []byte{0xFF, 0xC0})
	if i < 0 {
		t.Fatal("no SOF0")
	}
	out[i+1] = marker
	binary.BigEndian.PutUint16(out[i+5:], uint16(h))
	binary.BigEndian.PutUint16(out[i+7:], uint16(w))
	return out
}

func TestJPEGDecodeBytes(t *testing.T) {
	const w, h = 64, 48
	base := jpegBytes(t, w, h)
	want := int64(w*h + 2*((w+1)/2)*((h+1)/2))
	got, ok := jpegDecodeBytes(base)
	if !ok || got != want {
		t.Fatalf("baseline 4:2:0: %d %v, want %d", got, ok, want)
	}
	got, ok = jpegDecodeBytes(patchSOF(t, base, 0xC2, w, h))
	if !ok || got != 5*want {
		t.Fatalf("progressive: %d %v, want %d", got, ok, 5*want)
	}
	// 3000² baseline is fine (13.5 MB), progressive is not (67.5 MB).
	got, ok = jpegDecodeBytes(patchSOF(t, base, 0xC0, 3000, 3000))
	if !ok || got > MaxDecodeBytes {
		t.Fatalf("3000² baseline: %d %v", got, ok)
	}
	prog := patchSOF(t, base, 0xC2, 3000, 3000)
	if got, ok = jpegDecodeBytes(prog); !ok || got <= MaxDecodeBytes {
		t.Fatalf("3000² progressive: %d %v", got, ok)
	}
	if _, err := DecodeImage(prog, 100, 100); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err %v", err)
	}
	// CMYK (4 components) and RGB-labelled JPEGs also pay for an RGBA copy.
	cmyk := append([]byte(nil), base...)
	i := bytes.Index(cmyk, []byte{0xFF, 0xC0})
	cmyk = append(cmyk[:i+9], append([]byte{4, 1, 0x11, 0, 2, 0x11, 0, 3, 0x11, 0, 4, 0x11, 0}, cmyk[i+9+10:]...)...)
	binary.BigEndian.PutUint16(cmyk[i+2:], 8+3*4)
	if got, ok = jpegDecodeBytes(cmyk); !ok || got != int64(4*w*h+4*w*h) {
		t.Fatalf("cmyk: %d %v", got, ok)
	}
}

func TestJPEGDecodeBytesTruncated(t *testing.T) {
	base := jpegBytes(t, 64, 48)
	i := bytes.Index(base, []byte{0xFF, 0xC0})
	for _, data := range [][]byte{nil, base[:2], base[:i], base[:i+6]} {
		if _, ok := jpegDecodeBytes(data); ok {
			t.Errorf("%d bytes accepted", len(data))
		}
	}
}

// withSegment inserts a marker segment right after SOI.
func withSegment(data []byte, marker byte, payload []byte) []byte {
	seg := append([]byte{0xFF, marker, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}, payload...)
	return append(append(append([]byte(nil), data[:2]...), seg...), data[2:]...)
}

// An Adobe APP14 segment with transform 0 makes the decoder treat the three
// components as RGB, which costs an RGBA copy; transform 1 (YCbCr) doesn't.
func TestJPEGDecodeBytesCountsAdobeRGB(t *testing.T) {
	const w, h = 64, 48
	base := jpegBytes(t, w, h)
	plain, _ := jpegDecodeBytes(base)
	adobe := func(transform byte) []byte {
		return withSegment(base, 0xEE, append([]byte("Adobe"), 0, 100, 0, 0, 0, 0, transform))
	}
	if got, ok := jpegDecodeBytes(adobe(0)); !ok || got != plain+4*w*h {
		t.Fatalf("transform 0: %d %v, want %d", got, ok, plain+4*w*h)
	}
	if got, ok := jpegDecodeBytes(adobe(1)); !ok || got != plain {
		t.Fatalf("transform 1: %d %v, want %d", got, ok, plain)
	}
}

// When the markers can't be walked the estimate is the worst case, 4 B/px,
// not YCbCr's 3: a 3800² picture is refused (it fits at 3 B/px).
func TestDecodeImageAssumes4BytesPerPixelWhenTheMarkersFail(t *testing.T) {
	base := patchSOF(t, jpegBytes(t, 64, 48), 0xC0, 3800, 3800)
	// Stray bytes before the frame header: Go's decoder skips them, the walk gives up.
	j := bytes.Index(base, []byte{0xFF, 0xC0})
	cut := append(append(append([]byte(nil), base[:j]...), 0, 0), base[j:]...)
	if _, ok := jpegDecodeBytes(cut); ok {
		t.Fatal("the walk was expected to fail")
	}
	if _, err := DecodeImage(cut, 100, 100); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err %v", err)
	}
}

// The RGBA fast path's three alpha branches (transparent, opaque, partial
// with the colour clamped when it exceeds alpha), and the types that go
// through At: CMYK, Gray16, NRGBA64, RGBA64. Straight through FromImage and,
// for the PNG-capable ones, through DecodeImage.
func TestFromImageAlphaBranchesAndFallbackTypes(t *testing.T) {
	rgba := image.NewRGBA(image.Rect(0, 0, 4, 1))
	copy(rgba.Pix, []byte{
		10, 20, 30, 0, // transparent: any colour reads as 0
		10, 20, 30, 255, // opaque
		50, 25, 5, 100, // partial, premultiplied
		200, 10, 10, 100, // colour above alpha (not premultiplied): clamps to 255
	})
	got := FromImage(rgba)
	if got.Pix[0] != 0 || got.Pix[1] != 0xFF0A141E {
		t.Errorf("transparent %08x, opaque %08x", got.Pix[0], got.Pix[1])
	}
	if p := got.Pix[2]; p>>24 != 100 || p>>16&0xFF != 128 || p>>8&0xFF != 64 || p&0xFF != 13 { // c*255/100, rounded
		t.Errorf("partial = %08x", p)
	}
	if p := got.Pix[3]; p>>24 != 100 || p>>16&0xFF != 0xFF {
		t.Errorf("clamped = %08x", p)
	}

	rect := image.Rect(2, 1, 7, 5) // a non-zero origin
	cmyk, g16, n64, r64 := image.NewCMYK(rect), image.NewGray16(rect), image.NewNRGBA64(rect), image.NewRGBA64(rect)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			cmyk.SetCMYK(x, y, color.CMYK{uint8(x * 40), uint8(y * 50), uint8(x * y * 9), uint8(x + y)})
			g16.SetGray16(x, y, color.Gray16{uint16(x*9000 + y*500)})
			n64.SetNRGBA64(x, y, color.NRGBA64{uint16(x * 9000), uint16(y * 11000), uint16(x*y*1000 + 7), uint16(20000 + x*7000)})
			a := uint16(30000 + x*5000)
			r64.SetRGBA64(x, y, color.RGBA64{a / 3, a / 2, a / 5, a})
		}
	}
	for name, m := range map[string]image.Image{"cmyk": cmyk, "gray16": g16, "nrgba64": n64, "rgba64": r64} {
		samePixels(t, name, FromImage(m), slowFromImage(m))
	}
	for name, m := range map[string]image.Image{"gray16": g16, "nrgba64": n64, "rgba64": r64} {
		var buf bytes.Buffer
		if err := png.Encode(&buf, m); err != nil {
			t.Fatal(name, err)
		}
		dec, err := DecodeImage(buf.Bytes(), 100, 100)
		if err != nil {
			t.Fatal(name, err)
		}
		pm, _ := png.Decode(bytes.NewReader(buf.Bytes()))
		samePixels(t, name+" png", dec, slowFromImage(pm))
	}
}

// Resize reads its rows in place and needs no row buffer: it allocates the
// result (2), the column bounds (2) and the sums (1), not a row more.
func TestResizeAllocatesNoRowBuffer(t *testing.T) {
	src := NewImage(512, 512)
	if n := testing.AllocsPerRun(5, func() { Resize(src, 64, 64) }); n > 5 {
		t.Fatalf("%v allocations", n)
	}
}
