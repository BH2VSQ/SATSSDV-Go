package txqueue

import (
	"context"
	"errors"
	"fmt"
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

func TestRepeatIntervalEmitsCountdownAndCompletes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var phases []string
	var countdown []int
	sent := 0
	err := Run(ctx, []Job{{ID: "one", Profile: "ssdv-ax25"}}, Config{Repeat: true, MaxCycles: 2, Interval: 1100 * time.Millisecond}, func(context.Context, Job) error {
		sent++
		return nil
	}, func(event Event) {
		phases = append(phases, event.Phase)
		if event.Phase == "countdown" {
			var n int
			_, _ = fmt.Sscanf(event.Error, "%d", &n)
			countdown = append(countdown, n)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if sent != 2 {
		t.Fatalf("sent %d cycles, want 2", sent)
	}
	if len(countdown) < 2 || countdown[0] != 1 || countdown[len(countdown)-1] != 0 {
		t.Fatalf("countdown values = %v, want start at 1 and end at 0", countdown)
	}
}

func TestRunCancellationDuringCountdownEmitsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	countdownSeen := make(chan struct{}, 1)
	cancelledSeen := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, []Job{{ID: "repeat"}}, Config{Repeat: true, Interval: 5 * time.Second},
			func(context.Context, Job) error { return nil }, func(event Event) {
				switch event.Phase {
				case "countdown":
					select {
					case countdownSeen <- struct{}{}:
					default:
					}
				case "cancelled":
					select {
					case cancelledSeen <- struct{}{}:
					default:
					}
				}
			})
	}()
	select {
	case <-countdownSeen:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("countdown did not start")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	select {
	case <-cancelledSeen:
	case <-time.After(time.Second):
		t.Fatal("cancellation event was not emitted")
	}
}
