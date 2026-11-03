package ptt

import (
	"context"
	"errors"
	"sync"
	"time"
)

const MaxTestDuration = 15 * time.Second
const releaseTimeout = 3 * time.Second

var (
	ErrTestAlreadyActive = errors.New("PTT test is already active")
	ErrTestClosed        = errors.New("PTT test controller is closed")
)

type TestState struct {
	Active    bool      `json:"active"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	EndsAt    time.Time `json:"endsAt,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// TestController owns a single bounded PTT test. Its timer runs in Go, not in
// the web UI, and exactly one goroutine is responsible for deasserting PTT.
type TestController struct {
	mu         sync.Mutex
	controller Controller
	active     bool
	closed     bool
	cancel     context.CancelFunc
	done       chan struct{}
	state      TestState
	onChange   func(TestState)
	duration   time.Duration
}

func NewTestController(controller Controller, onChange func(TestState)) (*TestController, error) {
	if controller == nil {
		return nil, errors.New("PTT controller is required")
	}
	return &TestController{controller: controller, onChange: onChange, duration: MaxTestDuration}, nil
}

func (t *TestController) Start(ctx context.Context) error {
	if t == nil {
		return ErrTestClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return ErrTestClosed
	}
	if t.active {
		t.mu.Unlock()
		return ErrTestAlreadyActive
	}
	if err := t.controller.SetPTT(ctx, true); err != nil {
		// A controller may have asserted the line before a transport error was
		// observed. Always make a bounded best-effort release on failed start.
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), releaseTimeout)
		releaseErr := t.controller.SetPTT(releaseCtx, false)
		releaseCancel()
		t.mu.Unlock()
		if releaseErr != nil {
			return errors.Join(err, errors.New("best-effort PTT release failed: "+releaseErr.Error()))
		}
		return err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	started := time.Now()
	t.active, t.cancel, t.done = true, cancel, done
	t.state = TestState{Active: true, StartedAt: started, EndsAt: started.Add(t.duration)}
	state := t.state
	t.mu.Unlock()
	t.notify(state)
	go t.watch(runCtx, done)
	return nil
}

func (t *TestController) watch(ctx context.Context, done chan struct{}) {
	duration := t.duration
	if duration <= 0 || duration > MaxTestDuration {
		duration = MaxTestDuration
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
	releaseCtx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
	err := t.controller.SetPTT(releaseCtx, false)
	cancel()
	t.mu.Lock()
	if t.done == done {
		t.active = false
		t.cancel = nil
		t.done = nil
		t.state.Active = false
		t.state.EndsAt = time.Time{}
		if err != nil {
			t.state.Error = err.Error()
		}
		state := t.state
		t.mu.Unlock()
		t.notify(state)
	} else {
		t.mu.Unlock()
	}
	close(done)
}

func (t *TestController) Stop(ctx context.Context) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	if !t.active {
		errText := t.state.Error
		t.mu.Unlock()
		if errText != "" {
			return errors.New(errText)
		}
		return nil
	}
	cancel, done := t.cancel, t.done
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state.Error != "" {
		return errors.New(t.state.Error)
	}
	return nil
}

func (t *TestController) State() TestState {
	if t == nil {
		return TestState{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state
}

func (t *TestController) notify(state TestState) {
	if t.onChange != nil {
		t.onChange(state)
	}
}

func (t *TestController) Close() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()
	stopCtx, cancel := context.WithTimeout(context.Background(), releaseTimeout+time.Second)
	defer cancel()
	err := t.Stop(stopCtx)
	closeErr := t.controller.Close()
	if err != nil && closeErr != nil {
		return errors.Join(err, closeErr)
	}
	if err != nil {
		return err
	}
	return closeErr
}
