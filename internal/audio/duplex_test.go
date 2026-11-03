package audio

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeCapture struct {
	mu       sync.Mutex
	active   bool
	callback func([]float32)
}

func (f *fakeCapture) StartCapture(_ context.Context, cb func([]float32)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.active = true
	f.callback = cb
	return nil
}
func (f *fakeCapture) StopCapture() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.active = false
	return nil
}

type fakePlayback struct {
	started chan struct{}
	release chan struct{}
}

func (f *fakePlayback) Play(ctx context.Context, _ []float32, _ int) error {
	close(f.started)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.release:
		return nil
	}
}
func (f *fakePlayback) StopPlayback() error { return nil }

type fakePTT struct {
	mu sync.Mutex
	on bool
}

func (f *fakePTT) SetPTT(_ context.Context, on bool) error {
	f.mu.Lock()
	f.on = on
	f.mu.Unlock()
	return nil
}
func (f *fakePTT) Close() error { return nil }
func TestRXRemainsActiveDuringTX(t *testing.T) {
	cap := &fakeCapture{}
	play := &fakePlayback{started: make(chan struct{}), release: make(chan struct{})}
	key := &fakePTT{}
	d, e := NewDuplex(cap, play, key, 48000, 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	if e = d.StartRX(context.Background(), func([]float32) {}); e != nil {
		t.Fatal(e)
	}
	txDone := make(chan error, 1)
	go func() { txDone <- d.Transmit(context.Background(), []float32{.1, .2}, nil) }()
	select {
	case <-play.started:
	case <-time.After(time.Second):
		t.Fatal("playback did not start")
	}
	if st := d.State(); !st.RXActive || !st.TXActive {
		t.Fatalf("state during TX=%+v", st)
	}
	close(play.release)
	if e := <-txDone; e != nil {
		t.Fatal(e)
	}
	if !d.State().RXActive {
		t.Fatal("RX stopped when TX finished")
	}
	_ = d.Close()
}

type sequencePlayback struct{ calls int }

func (p *sequencePlayback) Play(_ context.Context, samples []float32, _ int) error {
	if len(samples) == 0 {
		return errors.New("empty")
	}
	p.calls++
	return nil
}
func (*sequencePlayback) StopPlayback() error { return nil }

type historyPTT struct {
	mu     sync.Mutex
	states []bool
}

func (p *historyPTT) SetPTT(_ context.Context, on bool) error {
	p.mu.Lock()
	p.states = append(p.states, on)
	p.mu.Unlock()
	return nil
}
func (*historyPTT) Close() error { return nil }
func TestTransmitPacketsHoldsPTTAcrossEntireBurst(t *testing.T) {
	cap := &fakeCapture{}
	play := &sequencePlayback{}
	key := &historyPTT{}
	d, err := NewDuplex(cap, play, key, 48000, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var progress float64
	err = d.TransmitPackets(context.Background(), [][]float32{{.1, .2}, {.3}, {.4, .5, .6}}, func(v float64) { progress = v })
	if err != nil {
		t.Fatal(err)
	}
	if play.calls != 3 {
		t.Fatalf("play calls=%d", play.calls)
	}
	key.mu.Lock()
	states := append([]bool(nil), key.states...)
	key.mu.Unlock()
	if len(states) != 2 || !states[0] || states[1] {
		t.Fatalf("PTT transitions=%v", states)
	}
	if progress != 1 {
		t.Fatalf("progress=%v", progress)
	}
}
