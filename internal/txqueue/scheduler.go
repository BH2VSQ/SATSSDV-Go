package txqueue

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Job struct {
	ID              string `json:"id"`
	ImagePath       string `json:"imagePath,omitempty"`
	ImageName       string `json:"imageName,omitempty"`
	ImageDataBase64 string `json:"imageDataBase64,omitempty"`
	Profile         string `json:"profile"`
	Quality         string `json:"quality"`
	AX25SSID        int    `json:"ax25Ssid,omitempty"`
	AX25To          string `json:"ax25To,omitempty"`
}
type Config struct {
	Repeat    bool          `json:"repeat"`
	Interval  time.Duration `json:"interval"`
	MaxCycles int           `json:"maxCycles"`
}
type Event struct {
	Job   Job    `json:"job"`
	Index int    `json:"index"`
	Total int    `json:"total"`
	Cycle int    `json:"cycle"`
	Phase string `json:"phase"`
	Error string `json:"error,omitempty"`
}
type Sender func(context.Context, Job) error
type Observer func(Event)

func Run(ctx context.Context, jobs []Job, cfg Config, send Sender, observe Observer) error {
	if len(jobs) == 0 {
		return errors.New("transmit queue is empty")
	}
	if send == nil {
		return errors.New("transmit sender is required")
	}
	if cfg.Interval < 0 || cfg.Interval > 24*time.Hour {
		return errors.New("send interval must be between 0 and 24 hours")
	}
	if cfg.MaxCycles < 0 {
		return errors.New("maximum cycles cannot be negative")
	}
	if !cfg.Repeat {
		cfg.MaxCycles = 1
	} else if cfg.MaxCycles == 0 {
		cfg.MaxCycles = -1
	}
	cycle := 0
	waitWithCountdown := func(j Job, index, cycleNumber int) error {
		return sleepWithCountdown(ctx, cfg.Interval, func(remaining time.Duration) {
			seconds := 0
			if remaining > 0 {
				seconds = int(remaining / time.Second)
				if remaining%time.Second != 0 {
					seconds++
				}
			}
			emit(observe, Event{
				Job: j, Index: index, Total: len(jobs), Cycle: cycleNumber,
				Phase: "countdown", Error: fmt.Sprintf("%d", seconds),
			})
		})
	}
	for {
		cycle++
		for i, j := range jobs {
			if err := ctx.Err(); err != nil {
				emit(observe, Event{Job: j, Index: i + 1, Total: len(jobs), Cycle: cycle, Phase: "cancelled"})
				return err
			}
			emit(observe, Event{Job: j, Index: i + 1, Total: len(jobs), Cycle: cycle, Phase: "sending"})
			if err := send(ctx, j); err != nil {
				if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					emit(observe, Event{Job: j, Index: i + 1, Total: len(jobs), Cycle: cycle, Phase: "cancelled"})
					if ctx.Err() != nil {
						return ctx.Err()
					}
					return err
				}
				emit(observe, Event{Job: j, Index: i + 1, Total: len(jobs), Cycle: cycle, Phase: "error", Error: err.Error()})
				return fmt.Errorf("send queue item %q: %w", j.ID, err)
			}
			emit(observe, Event{Job: j, Index: i + 1, Total: len(jobs), Cycle: cycle, Phase: "completed"})

			// The operator's first Send action must transmit immediately. In repeat
			// mode, start the interval only after this item has completed, then
			// begin the next item/cycle when the countdown reaches zero.
			moreItems := i < len(jobs)-1
			moreCycles := cfg.Repeat && (cfg.MaxCycles < 0 || cycle < cfg.MaxCycles)
			if moreItems || moreCycles {
				if err := waitWithCountdown(j, i+1, cycle); err != nil {
					if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						emit(observe, Event{Job: j, Index: i + 1, Total: len(jobs), Cycle: cycle, Phase: "cancelled"})
					}
					return err
				}
			}
		}
		if !cfg.Repeat || (cfg.MaxCycles > 0 && cycle >= cfg.MaxCycles) {
			return nil
		}
	}
}
func emit(fn Observer, e Event) {
	if fn != nil {
		fn(e)
	}
}
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// sleepWithCountdown emits a one-second countdown while preserving prompt cancellation.
func sleepWithCountdown(ctx context.Context, d time.Duration, tick func(time.Duration)) error {
	end := time.Now().Add(d)
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	emitRemaining := func(remaining time.Duration) {
		if tick == nil {
			return
		}
		if remaining <= 0 {
			tick(0)
			return
		}
		// Round up so a slightly-late ticker callback does not skip a visible
		// second (for example, 119.999s must still display 120s).
		roundedSeconds := (remaining + time.Second - 1) / time.Second
		tick(roundedSeconds * time.Second)
	}
	emitRemaining(d)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			emitRemaining(0)
			return nil
		case <-ticker.C:
			if tick != nil {
				remaining := time.Until(end)
				if remaining < 0 {
					remaining = 0
				}
				tick(remaining)
			}
		}
	}
}
