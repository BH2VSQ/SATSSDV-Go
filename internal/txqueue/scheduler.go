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
	for {
		cycle++
		for i, j := range jobs {
			if err := ctx.Err(); err != nil {
				return err
			}
			emit(observe, Event{Job: j, Index: i + 1, Total: len(jobs), Cycle: cycle, Phase: "sending"})
			if err := send(ctx, j); err != nil {
				emit(observe, Event{Job: j, Index: i + 1, Total: len(jobs), Cycle: cycle, Phase: "error", Error: err.Error()})
				return fmt.Errorf("send queue item %q: %w", j.ID, err)
			}
			emit(observe, Event{Job: j, Index: i + 1, Total: len(jobs), Cycle: cycle, Phase: "completed"})
			if i < len(jobs)-1 || cfg.Repeat {
				if err := sleep(ctx, cfg.Interval); err != nil {
					return err
				}
			}
		}
		if !cfg.Repeat {
			return nil
		}
		if cfg.MaxCycles > 0 && cycle >= cfg.MaxCycles {
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
