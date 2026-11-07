package audio

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"
)

func TestFloat32RingPreservesOrderAcrossWrapAndBackpressure(t *testing.T) {
	ring, err := newFloat32Ring(7)
	if err != nil {
		t.Fatal(err)
	}
	input := make([]float32, 73)
	for i := range input {
		input[i] = float32(i) / 10
	}
	writeDone := make(chan error, 1)
	go func() { writeDone <- ring.Write(context.Background(), input) }()
	var output []float32
	for len(output) < len(input) {
		buf := make([]byte, 3*4)
		n := ring.ReadToBytes(buf)
		for i := 0; i < n; i++ {
			output = append(output, math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:i*4+4])))
		}
		if n == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	if len(output) != len(input) {
		t.Fatalf("read %d samples, want %d", len(output), len(input))
	}
	for i := range input {
		if output[i] != input[i] {
			t.Fatalf("sample %d=%v, want %v", i, output[i], input[i])
		}
	}
}

func TestFloat32RingWriterStopsWhenConsumerIsCancelled(t *testing.T) {
	ring, err := newFloat32Ring(4)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	writeDone := make(chan error, 1)
	go func() { writeDone <- ring.Write(ctx, make([]float32, 32)) }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-writeDone:
		if err != context.Canceled {
			t.Fatalf("writer error=%v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked PCM ring writer did not observe cancellation")
	}
}
