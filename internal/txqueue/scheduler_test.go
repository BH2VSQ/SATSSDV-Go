package txqueue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunRepeatLimitedCycles(t *testing.T) {
	var n atomic.Int32
	j := []Job{{ID: "a"}, {ID: "b"}}
	err := Run(context.Background(), j, Config{Repeat: true, Interval: 0, MaxCycles: 3}, func(context.Context, Job) error { n.Add(1); return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n.Load() != 6 {
		t.Fatalf("sent=%d", n.Load())
	}
}
func TestRunCancellationStopsLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var n atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, []Job{{ID: "a"}, {ID: "b"}}, Config{Repeat: true, Interval: time.Second}, func(context.Context, Job) error { n.Add(1); cancel(); return nil }, nil)
	}()
	if err := <-done; err == nil {
		t.Fatal("expected cancellation")
	}
	if n.Load() != 1 {
		t.Fatalf("sent=%d", n.Load())
	}
}
func TestRunRejectsEmptyQueue(t *testing.T) {
	if err := Run(context.Background(), nil, Config{}, func(context.Context, Job) error { return nil }, nil); err == nil {
		t.Fatal("expected error")
	}
}
