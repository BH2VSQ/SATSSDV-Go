package audio

import (
	"sync/atomic"
	"testing"
)

func TestEnqueueCapturePCMDoesNotDrainAcceptedAudio(t *testing.T) {
	queue := make(chan []float32, 2)
	var dropped atomic.Uint64
	gapPending := false
	first := []float32{1}
	second := []float32{2}
	third := []float32{3}

	if !enqueueCapturePCM(queue, first, &gapPending, &dropped) || !enqueueCapturePCM(queue, second, &gapPending, &dropped) {
		t.Fatal("initial PCM blocks were not accepted")
	}
	if enqueueCapturePCM(queue, third, &gapPending, &dropped) {
		t.Fatal("PCM block should be dropped while capture queue is full")
	}
	if !gapPending || dropped.Load() != 1 {
		t.Fatalf("overflow state gap=%v dropped=%d", gapPending, dropped.Load())
	}
	if got := <-queue; got[0] != 1 {
		t.Fatalf("oldest accepted block was discarded: %v", got)
	}
	if enqueueCapturePCM(queue, []float32{4}, &gapPending, &dropped) {
		t.Fatal("one free slot is insufficient for marker plus resumed PCM")
	}
	if got := <-queue; got[0] != 2 {
		t.Fatalf("second accepted block was discarded: %v", got)
	}
	if !enqueueCapturePCM(queue, []float32{5}, &gapPending, &dropped) {
		t.Fatal("capture queue did not resume after enough capacity became available")
	}
	if gapPending {
		t.Fatal("discontinuity marker should have been inserted before resumed PCM")
	}
	marker := <-queue
	if marker != nil {
		t.Fatalf("first resumed queue item should be a discontinuity marker, got %v", marker)
	}
	resumed := <-queue
	if len(resumed) != 1 || resumed[0] != 5 {
		t.Fatalf("unexpected resumed PCM block: %v", resumed)
	}
	if dropped.Load() != 2 {
		t.Fatalf("dropped callbacks=%d, want 2", dropped.Load())
	}
}
