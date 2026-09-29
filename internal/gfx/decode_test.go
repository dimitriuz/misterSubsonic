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
