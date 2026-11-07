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

type fakeStreamPlayback struct {
	calls     int
	count     int
	key       *historyPTT
	blockPlay bool
	started   chan struct{}
}

type fakeStreamSession struct {
	playback *fakeStreamPlayback
	ctx      context.Context
}

func (f *fakeStreamPlayback) Play(_ context.Context, _ []float32, _ int) error { return nil }
func (f *fakeStreamPlayback) StopPlayback() error                              { return nil }
func (f *fakeStreamPlayback) PrepareStream(ctx context.Context, sampleRate int, estimated int64, producer StreamProducer, progress func(float64)) (StreamingPlaybackSession, error) {
	if sampleRate != 48000 || estimated != 5 {
		return nil, errors.New("unexpected stream parameters")
	}
	if f.key != nil {
		f.key.mu.Lock()
		keyed := len(f.key.states) > 0 && f.key.states[len(f.key.states)-1]
		f.key.mu.Unlock()
		if keyed {
			return nil, errors.New("PTT was asserted before stream prefill")
		}
	}
	err := producer(ctx, func(block []float32) error { f.count += len(block); return nil })
	if err != nil {
		return nil, err
	}
	_ = progress // Playback, not prefill, advances the UI progress indicator.
	return &fakeStreamSession{playback: f, ctx: ctx}, nil
}

func (s *fakeStreamSession) Play(ctx context.Context) error {
	if s.playback.key != nil {
		s.playback.key.mu.Lock()
		keyed := len(s.playback.key.states) > 0 && s.playback.key.states[len(s.playback.key.states)-1]
		s.playback.key.mu.Unlock()
		if !keyed {
			return errors.New("PTT was not asserted before playback")
		}
	}
	s.playback.calls++
	if s.playback.started != nil {
		close(s.playback.started)
	}
	if s.playback.blockPlay {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}
	return nil
}
func (s *fakeStreamSession) Stop() error { return nil }

func TestTransmitStreamHoldsPTTAcrossProducerAndPlayback(t *testing.T) {
	cap := &fakeCapture{}
	key := &historyPTT{}
	play := &fakeStreamPlayback{key: key}
	d, err := NewDuplex(cap, play, key, 48000, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var gotProgress float64
	playbackStarted := false
	err = d.TransmitStreamWithStart(context.Background(), 5, func(ctx context.Context, emit func([]float32) error) error {
		if !d.State().TXActive {
			return errors.New("TX state was not active while producing")
		}
		return emit([]float32{1, 2, 3, 4, 5})
	}, func() { playbackStarted = true }, func(value float64) { gotProgress = value })
	if err != nil {
		t.Fatal(err)
	}
	if play.calls != 1 || play.count != 5 {
		t.Fatalf("stream playback calls=%d samples=%d", play.calls, play.count)
	}
	if !playbackStarted {
		t.Fatal("playback-start callback was not invoked")
	}
	key.mu.Lock()
	states := append([]bool(nil), key.states...)
	key.mu.Unlock()
	if len(states) != 2 || !states[0] || states[1] {
		t.Fatalf("PTT transitions=%v", states)
	}
	if gotProgress != 1 {
		t.Fatalf("final progress=%v", gotProgress)
	}
	if d.State().TXActive {
		t.Fatal("TX remained active after stream completed")
	}
	_ = d.Close()
}

func TestTransmitStreamPlaybackStartCallbackRunsAfterPTTAndPrefill(t *testing.T) {
	cap := &fakeCapture{}
	key := &historyPTT{}
	play := &fakeStreamPlayback{key: key}
	d, err := NewDuplex(cap, play, key, 48000, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	err = d.TransmitStreamWithStart(context.Background(), 5, func(_ context.Context, emit func([]float32) error) error {
		return emit([]float32{1, 2, 3, 4, 5})
	}, func() {
		key.mu.Lock()
		keyed := len(key.states) > 0 && key.states[len(key.states)-1]
		key.mu.Unlock()
		if !keyed {
			t.Error("playback-start callback ran before PTT was asserted")
		}
		if play.calls != 0 {
			t.Error("playback-start callback ran after playback entered")
		}
		called = true
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("playback-start callback was not invoked")
	}
	_ = d.Close()
}

func TestTransmitStreamProducerErrorDoesNotAssertPTT(t *testing.T) {
	cap := &fakeCapture{}
	key := &historyPTT{}
	play := &fakeStreamPlayback{key: key}
	d, err := NewDuplex(cap, play, key, 48000, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("modulator failed")
	err = d.TransmitStream(context.Background(), 5, func(context.Context, func([]float32) error) error {
		return wantErr
	}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("TransmitStream error=%v, want %v", err, wantErr)
	}
	key.mu.Lock()
	states := append([]bool(nil), key.states...)
	key.mu.Unlock()
	if len(states) != 0 {
		t.Fatalf("PTT should not be keyed when prefill fails: %v", states)
	}
	_ = d.Close()
}

func TestTransmitStreamCancellationReleasesPTT(t *testing.T) {
	cap := &fakeCapture{}
	key := &historyPTT{}
	play := &fakeStreamPlayback{key: key, blockPlay: true, started: make(chan struct{})}
	d, err := NewDuplex(cap, play, key, 48000, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- d.TransmitStream(context.Background(), 5, func(_ context.Context, emit func([]float32) error) error {
			return emit([]float32{1, 2, 3, 4, 5})
		}, nil)
	}()
	select {
	case <-play.started:
	case <-time.After(time.Second):
		t.Fatal("stream playback did not start")
	}
	d.StopTX()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("TX error=%v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stream TX did not stop after cancellation")
	}
	key.mu.Lock()
	states := append([]bool(nil), key.states...)
	key.mu.Unlock()
	if len(states) != 2 || !states[0] || states[1] {
		t.Fatalf("PTT transitions after cancellation=%v", states)
	}
	_ = d.Close()
}

func TestTransmitStreamRejectsExcessiveDurationBeforePTT(t *testing.T) {
	cap := &fakeCapture{}
	key := &historyPTT{}
	play := &fakeStreamPlayback{key: key}
	d, err := NewDuplex(cap, play, key, 48000, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	err = d.TransmitStream(context.Background(), int64(48000)*int64(24*60*60+1), func(context.Context, func([]float32) error) error { return nil }, nil)
	if err == nil {
		t.Fatal("expected 24-hour safety limit error")
	}
	key.mu.Lock()
	states := append([]bool(nil), key.states...)
	key.mu.Unlock()
	if len(states) != 0 {
		t.Fatalf("PTT should remain off for rejected stream: %v", states)
	}
	_ = d.Close()
}

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
