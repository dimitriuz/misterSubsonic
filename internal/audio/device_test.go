package audio

import (
	"testing"
	"time"
)

func TestNullDeviceConsumesAndFlushes(t *testing.T) {
	out, err := OpenDevice(DeviceOptions{Null: true, RingFrames: 4800})
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	buf := make([]float32, 9600*2)
	if n := out.Write(buf); n != 4800 {
		t.Fatalf("Write into a 4800-frame ring took %d frames", n)
	}
	deadline := time.Now().Add(2 * time.Second)
	for out.Consumed() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("null device never consumed any frames")
		}
		time.Sleep(5 * time.Millisecond)
	}

	out.SetPaused(true)
	time.Sleep(50 * time.Millisecond)
	c1 := out.Consumed()
	time.Sleep(100 * time.Millisecond)
	if c2 := out.Consumed(); c2 != c1 {
		t.Fatalf("consumed advanced while paused: %d -> %d", c1, c2)
	}

	out.Flush()
	if got := out.Consumed(); got != 4800 {
		t.Fatalf("after Flush Consumed = %d, want 4800 (everything written)", got)
	}
	out.SetPaused(false)
}

// I3: Flush must not spin forever when the device callback has stopped
// (device error, unplugged USB DAC); it drains the ring itself.
func TestFlushAfterDeviceStopped(t *testing.T) {
	out, err := OpenDevice(DeviceOptions{Null: true, RingFrames: 4800})
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if n := out.Write(make([]float32, 4800*2)); n != 4800 {
		t.Fatalf("Write took %d frames, want 4800", n)
	}
	stopDeviceForTest()
	done := make(chan struct{})
	go func() { out.Flush(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Flush did not return with the device callback stopped")
	}
	if got := out.Consumed(); got != 4800 {
		t.Fatalf("after Flush Consumed = %d, want 4800 (everything written)", got)
	}
}

// After Close, the device calls are harmless no-ops (a late Flush or Write
// from a shutting-down engine must not touch the freed ring).
func TestDeviceCallsAfterCloseAreSafe(t *testing.T) {
	out, err := OpenDevice(DeviceOptions{Null: true, RingFrames: 4800})
	if err != nil {
		t.Fatal(err)
	}
	out.Close()
	if n := out.Write(make([]float32, 100*2)); n != 0 {
		t.Fatalf("Write after Close took %d frames", n)
	}
	out.Flush()
	out.SetPaused(true)
	out.SetVolume(0.5)
	out.Close()
	stopDeviceForTest()
	again, err := OpenDevice(DeviceOptions{Null: true, RingFrames: 4800})
	if err != nil {
		t.Fatal("the device can't be opened again:", err)
	}
	again.SetPaused(false)
	again.SetVolume(1)
	again.Close()
}

func waitConsumed(t *testing.T, out Output, atLeast uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for out.Consumed() < atLeast {
		if time.Now().After(deadline) {
			t.Fatalf("Consumed = %d, never reached %d", out.Consumed(), atLeast)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDeviceReopenKeepsCountingAndPause(t *testing.T) {
	out, err := OpenDevice(DeviceOptions{Null: true, RingFrames: 4800})
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	out.Write(make([]float32, 2000*2))
	waitConsumed(t, out, 2000)
	out.Write(make([]float32, 1000*2)) // still in the ring at the reopen: it counts as consumed
	if err := out.Reopen("", OutputRate); err != nil {
		t.Fatal(err)
	}
	if got := out.Consumed(); got != 3000 {
		t.Fatalf("Consumed = %d after the reopen, want 3000 (everything written)", got)
	}
	out.Write(make([]float32, 500*2))
	waitConsumed(t, out, 3500)

	out.SetPaused(true)
	if err := out.Reopen("", OutputRate); err != nil {
		t.Fatal(err)
	}
	out.Write(make([]float32, 100*2))
	time.Sleep(60 * time.Millisecond)
	if got := out.Consumed(); got != 3500 {
		t.Fatalf("a paused device consumed after a reopen: %d", got)
	}
	out.SetPaused(false)
	waitConsumed(t, out, 3600)
}

func TestDeviceStoppedByItselfIsLostUntilReopened(t *testing.T) {
	out, err := OpenDevice(DeviceOptions{Null: true, RingFrames: 4800})
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if out.Lost() {
		t.Fatal("a fresh device reports lost")
	}
	stopDeviceForTest() // as an unplugged DAC would
	deadline := time.Now().Add(time.Second)
	for !out.Lost() {
		if time.Now().After(deadline) {
			t.Fatal("a stopped device never reported lost")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := out.Reopen("", OutputRate); err != nil {
		t.Fatal(err)
	}
	if out.Lost() {
		t.Fatal("still lost after a reopen")
	}
	out.Close()
	if out.Lost() {
		t.Fatal("closing the device counts as losing it")
	}
}

func TestFixedDeviceRunsAt48k(t *testing.T) {
	out, err := OpenDevice(DeviceOptions{Null: true, Rate: 96000})
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if out.Rate() != OutputRate || !out.Supports(OutputRate) || out.Supports(44100) {
		t.Fatalf("rate %d, supports 48k %v, 44.1k %v: a fixed device runs at 48 kHz only",
			out.Rate(), out.Supports(OutputRate), out.Supports(44100))
	}
	if err := out.Reopen("", 44100); err != nil || out.Rate() != OutputRate {
		t.Fatalf("reopen at 44.1 kHz: %v, rate %d; a fixed device stays at 48 kHz", err, out.Rate())
	}
}

func TestNativeDeviceIsLostWhenItsCardGoes(t *testing.T) {
	present := true
	var asked []string
	old := cardPresent
	cardPresent = func(device string) bool { asked = append(asked, device); return present }
	defer func() { cardPresent = old }()

	d := &device{name: "hw:CARD=E30,DEV=0"} // the card check only: no C device behind it
	if d.Lost() {
		t.Fatal("lost with the card there")
	}
	present = false
	if d.Lost() {
		t.Fatal("checked again within cardCheckEvery")
	}
	d.checked = time.Now().Add(-cardCheckEvery)
	if !d.Lost() {
		t.Fatal("not lost with the card gone")
	}
	if len(asked) != 2 || asked[0] != "hw:CARD=E30,DEV=0" {
		t.Fatalf("checked %q", asked)
	}
	(&device{name: "default"}).Lost() // not native: no check
	if len(asked) != 2 {
		t.Fatalf("checked a non-native device: %q", asked)
	}
}

func TestConvertSamplesIsExactAndClamps(t *testing.T) {
	rd32 := func(b []byte) int32 {
		return int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
	}
	rd24 := func(b []byte) int32 { return (int32(uint32(b[0])|uint32(b[1])<<8|uint32(b[2])<<16) << 8) >> 8 }
	rd16 := func(b []byte) int32 { return int32(int16(uint16(b[0]) | uint16(b[1])<<8)) }

	for _, n := range []int32{0, 1, -1, 12345, -12345, 32767, -32768} {
		if got := rd32(convertSamples([]float32{float32(n) / 32768}, 32)); got != n<<16 {
			t.Errorf("16-bit %d as s32 = %d, want %d", n, got, n<<16)
		}
		if got := rd16(convertSamples([]float32{float32(n) / 32768}, 16)); got != n {
			t.Errorf("s16 round trip %d = %d", n, got)
		}
	}
	for _, m := range []int32{0, 1, -1, 1234567, -1234567, 8388607, -8388608} {
		f := float32(m) / 8388608
		if got := rd32(convertSamples([]float32{f}, 32)); got != m<<8 {
			t.Errorf("24-bit %d as s32 = %d, want %d", m, got, m<<8)
		}
		if got := rd24(convertSamples([]float32{f}, 24)); got != m {
			t.Errorf("s24 round trip %d = %d", m, got)
		}
	}
	ends := []struct {
		bits     int
		min, max int32
		read     func([]byte) int32
	}{
		{16, -32768, 32767, rd16},
		{24, -8388608, 8388607, rd24},
		{32, -2147483648, 2147483647, rd32},
	}
	for _, e := range ends {
		per := e.bits / 8
		b := convertSamples([]float32{1, -1, 1.5, -3}, e.bits)
		want := []int32{e.max, e.min, e.max, e.min}
		for i, w := range want {
			if got := e.read(b[i*per:]); got != w {
				t.Errorf("%d-bit sample %d = %d, want %d", e.bits, i, got, w)
			}
		}
	}
}
