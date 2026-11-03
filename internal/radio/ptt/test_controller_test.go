package ptt

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeController struct {
	mu         sync.Mutex
	states     []bool
	closeCount int
	setErr     error
}

func (f *fakeController) SetPTT(_ context.Context, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = append(f.states, on)
	return f.setErr
}
func (f *fakeController) Close() error { f.mu.Lock(); defer f.mu.Unlock(); f.closeCount++; return nil }
func (f *fakeController) snapshot() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool(nil), f.states...)
}
func newFastTestController(c Controller, d time.Duration) *TestController {
	t := &TestController{controller: c, duration: d}
	return t
}
func TestPTTTestManualStopReleasesExactlyOnce(t *testing.T) {
	f := &fakeController{}
	tc := newFastTestController(f, time.Second)
	if err := tc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !tc.State().Active {
		t.Fatal("test should be active")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := tc.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	got := f.snapshot()
	if len(got) != 2 || !got[0] || got[1] {
		t.Fatalf("PTT transitions=%v", got)
	}
	if tc.State().Active {
		t.Fatal("test should be inactive")
	}
}
func TestPTTTestAutomaticallyReleases(t *testing.T) {
	f := &fakeController{}
	tc := newFastTestController(f, 35*time.Millisecond)
	if err := tc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for tc.State().Active && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if tc.State().Active {
		t.Fatal("timer did not release PTT")
	}
	got := f.snapshot()
	if len(got) != 2 || got[1] {
		t.Fatalf("PTT transitions=%v", got)
	}
}
func TestPTTTestRejectsDuplicateStart(t *testing.T) {
	f := &fakeController{}
	tc := newFastTestController(f, time.Second)
	if err := tc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := tc.Start(context.Background()); !errors.Is(err, ErrTestAlreadyActive) {
		t.Fatalf("err=%v", err)
	}
	_ = tc.Stop(context.Background())
}
func TestPTTTestMaxDurationIs15Seconds(t *testing.T) {
	if MaxTestDuration != 15*time.Second {
		t.Fatalf("max test duration=%s", MaxTestDuration)
	}
}
func TestPTTTestStartFailureDoesNotEnterActiveState(t *testing.T) {
	f := &fakeController{setErr: errors.New("radio unavailable")}
	tc := newFastTestController(f, time.Second)
	if err := tc.Start(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if tc.State().Active {
		t.Fatal("failed test became active")
	}
}

type assertFailsButReleaseWorks struct {
	mu    sync.Mutex
	calls []bool
}

func (f *assertFailsButReleaseWorks) SetPTT(_ context.Context, on bool) error {
	f.mu.Lock()
	f.calls = append(f.calls, on)
	f.mu.Unlock()
	if on {
		return errors.New("partial PTT assertion failure")
	}
	return nil
}
func (*assertFailsButReleaseWorks) Close() error { return nil }

func TestPTTTestStartFailureAttemptsRelease(t *testing.T) {
	f := &assertFailsButReleaseWorks{}
	tc := newFastTestController(f, time.Second)
	if err := tc.Start(context.Background()); err == nil {
		t.Fatal("expected assertion failure")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 2 || !f.calls[0] || f.calls[1] {
		t.Fatalf("PTT transitions after failed start=%v", f.calls)
	}
}
