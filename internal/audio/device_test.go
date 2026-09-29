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
