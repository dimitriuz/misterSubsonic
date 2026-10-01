package ui

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// shoot presses the screenshot button and runs posted work until the save
// reports back.
func shoot(t *testing.T, ta *testApp) {
	t.Helper()
	ta.press(input.BtnScreenshot)
	if !ta.shooting {
		t.Fatal("no screenshot started")
	}
	deadline := time.After(5 * time.Second)
	for ta.shooting {
		select {
		case f := <-ta.post:
			f()
		case <-deadline:
			t.Fatal("the screenshot never finished")
		}
	}
}

// The screenshot button saves the frame on screen as a PNG, named by the
// time, without redrawing; a second one in the same second gets its own name.
func TestScreenshotSavesTheFrameOnScreen(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.o.ScreenshotDir = filepath.Join(t.TempDir(), "screenshots", "MiSTer_Subsonic")
	ta.Push(NewHomeScreen())
	frame := ta.settle(t).ToRGBA()
	frames := ta.disp.Frames()

	shoot(t, ta)
	if got := ta.toasts[len(ta.toasts)-1].text; got != "Screenshot saved" {
		t.Fatalf("toast %q", got)
	}
	if ta.disp.Frames() != frames {
		t.Fatal("the screenshot redrew the screen")
	}
	name := time.Unix(1_800_000_000, 0).Format("20060102_150405")
	f, err := os.Open(filepath.Join(ta.o.ScreenshotDir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	got := image.NewRGBA(img.Bounds())
	draw.Draw(got, got.Rect, img, image.Point{}, draw.Src)
	if !samePixels(frame, got) {
		t.Fatal("the PNG isn't the frame on screen")
	}

	shoot(t, ta)
	entries, _ := os.ReadDir(ta.o.ScreenshotDir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 2 || names[0] != name+"-2.png" || names[1] != name+".png" {
		t.Fatalf("files %v, want %s.png and %s-2.png (and no .tmp)", names, name, name)
	}
}

// A folder that can't be made shows a toast; with no folder the button does
// nothing.
func TestScreenshotFailureAndOff(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ta.o.ScreenshotDir = filepath.Join(file, "shots")
	ta.Push(NewHomeScreen())
	ta.settle(t)
	shoot(t, ta)
	if got := ta.toasts[len(ta.toasts)-1].text; got != "Screenshot failed" {
		t.Fatalf("toast %q", got)
	}

	ta.o.ScreenshotDir = ""
	ta.press(input.BtnScreenshot)
	if ta.shooting || len(ta.post) != 0 {
		t.Fatal("a screenshot started with no folder")
	}
}

// The screensaver can be captured too: the button doesn't wake it, and
// doesn't count as activity.
func TestScreenshotDoesNotWakeTheScreensaver(t *testing.T) {
	ta := saverApp(t, ProfileHDMI, 1)
	ta.o.ScreenshotDir = t.TempDir()
	ta.now = ta.now.Add(time.Minute)
	ta.onWake()
	if !ta.saver {
		t.Fatal("the screensaver didn't start")
	}
	idle := ta.lastInput
	ta.settle(t)
	shoot(t, ta)
	if !ta.saver || ta.lastInput != idle {
		t.Fatalf("saver %v, last input moved %v", ta.saver, ta.lastInput.Sub(idle))
	}
}

// A press while a save is still running is refused with a toast, not lost
// silently.
func TestScreenshotWhileSavingSaysSo(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.o.ScreenshotDir = t.TempDir()
	ta.Push(NewHomeScreen())
	ta.settle(t)
	ta.press(input.BtnScreenshot)
	ta.press(input.BtnScreenshot) // the first save hasn't reported back yet
	if got := lastToast(ta); got != "Still saving the last screenshot" {
		t.Fatalf("toast %q", got)
	}
	for ta.shooting {
		(<-ta.post)()
	}
	if entries, _ := os.ReadDir(ta.o.ScreenshotDir); len(entries) != 1 {
		t.Fatalf("%d files, want the one screenshot", len(entries))
	}
}

// Over the screensaver the toasts are silent too: a toast would wake it.
func TestScreenshotToastsStaySilentOverTheScreensaver(t *testing.T) {
	ta := saverApp(t, ProfileHDMI, 1)
	ta.o.ScreenshotDir = t.TempDir()
	ta.now = ta.now.Add(time.Minute)
	ta.onWake()
	if !ta.saver {
		t.Fatal("the screensaver didn't start")
	}
	ta.settle(t)
	ta.press(input.BtnScreenshot)
	ta.press(input.BtnScreenshot) // still saving
	if !ta.saver || len(ta.toasts) != 0 {
		t.Fatalf("saver %v, toasts %v after Still saving", ta.saver, ta.toasts)
	}
	for ta.shooting {
		(<-ta.post)()
	}
	if !ta.saver || len(ta.toasts) != 0 {
		t.Fatalf("saver %v, toasts %v after Screenshot saved", ta.saver, ta.toasts)
	}
}

// A name another process is writing (its .tmp exists) is left alone: the
// save takes the next free name.
func TestSaveScreenshotSkipsANameBeingWritten(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "shot.png.tmp")
	if err := os.WriteFile(other, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := saveScreenshot(dir, "shot", gfx.NewCanvas(8, 8))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "shot-2.png" {
		t.Fatalf("saved as %s, want shot-2.png", filepath.Base(path))
	}
	if b, _ := os.ReadFile(other); string(b) != "partial" {
		t.Fatalf("the other save's file was overwritten: %q", b)
	}
}
