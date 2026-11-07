package audio

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/radio/ptt"
)

var (
	ErrClosed = errors.New("audio duplex controller is closed")
	ErrTXBusy = errors.New("a transmission is already active")
	ErrRXBusy = errors.New("receive stream is already active")
)

type CaptureStream interface {
	StartCapture(context.Context, func([]float32)) error
	StopCapture() error
}

// PlaybackStream.Play is synchronous. Implementations may read directly from
// the supplied sample slice until Play returns; callers must keep the slice
// alive and must not mutate it during playback. This contract avoids a second
// full-duration copy for long image/GIF transmissions.
type PlaybackStream interface {
	Play(context.Context, []float32, int) error
	StopPlayback() error
}

// StreamProducer emits bounded PCM blocks. A producer must stop when ctx is
// cancelled and must not retain or mutate a block after emit returns.
type StreamProducer func(ctx context.Context, emit func([]float32) error) error

// StreamingPlaybackSession separates PCM preparation from device playback.
// PrepareStream fills a bounded queue before PTT is asserted; Play starts the
// device callback and waits until the producer and queued PCM are drained.
type StreamingPlaybackSession interface {
	Play(context.Context) error
	Stop() error
}

// StreamingPlaybackStream prepares PCM through a bounded queue rather than
// requiring one full-duration waveform allocation.
type StreamingPlaybackStream interface {
	PrepareStream(context.Context, int, int64, StreamProducer, func(float64)) (StreamingPlaybackSession, error)
}
type State struct {
	RXActive bool `json:"rxActive"`
	TXActive bool `json:"txActive"`
	Closed   bool `json:"closed"`
}

type Duplex struct {
	capture    CaptureStream
	playback   PlaybackStream
	ptt        ptt.Controller
	sampleRate int
	lead       time.Duration
	tail       time.Duration
	mu         sync.Mutex
	rxMu       sync.Mutex
	txMu       sync.Mutex
	closed     bool
	rxActive   bool
	rxCancel   context.CancelFunc
	txActive   bool
	txCancel   context.CancelFunc
}

func NewDuplex(capture CaptureStream, playback PlaybackStream, keyer ptt.Controller, sampleRate int, lead, tail time.Duration) (*Duplex, error) {
	if capture == nil || playback == nil || keyer == nil {
		return nil, errors.New("capture, playback and PTT controllers are required")
	}
	if sampleRate < 8000 || sampleRate > 192000 {
		return nil, fmt.Errorf("invalid sample rate %d", sampleRate)
	}
	if lead < 0 || lead > 10*time.Second || tail < 0 || tail > 10*time.Second {
		return nil, errors.New("PTT lead/tail must be within 0..10 seconds")
	}
	return &Duplex{capture: capture, playback: playback, ptt: keyer, sampleRate: sampleRate, lead: lead, tail: tail}, nil
}

func (d *Duplex) StartRX(ctx context.Context, onSamples func([]float32)) error {
	if d == nil {
		return ErrClosed
	}
	if onSamples == nil {
		return errors.New("RX sample callback is required")
	}
	d.rxMu.Lock()
	defer d.rxMu.Unlock()
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return ErrClosed
	}
	if d.rxActive {
		d.mu.Unlock()
		return ErrRXBusy
	}
	rxCtx, cancel := context.WithCancel(context.Background())
	d.rxCancel = cancel
	d.rxActive = true
	d.mu.Unlock()
	if err := d.capture.StartCapture(rxCtx, onSamples); err != nil {
		cancel()
		d.mu.Lock()
		d.rxActive = false
		d.rxCancel = nil
		d.mu.Unlock()
		return fmt.Errorf("start RX capture: %w", err)
	}
	if ctx != nil {
		go func() {
			select {
			case <-ctx.Done():
				_ = d.StopRX()
			case <-rxCtx.Done():
			}
		}()
	}
	return nil
}
func (d *Duplex) StopRX() error {
	if d == nil {
		return nil
	}
	d.rxMu.Lock()
	defer d.rxMu.Unlock()
	d.mu.Lock()
	active, cancel := d.rxActive, d.rxCancel
	d.rxActive = false
	d.rxCancel = nil
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if !active {
		return nil
	}
	return d.capture.StopCapture()
}

func (d *Duplex) Transmit(ctx context.Context, samples []float32, onProgress func(float64)) error {
	return d.TransmitPackets(ctx, [][]float32{samples}, onProgress)
}

// TransmitPackets sends a sequence of already-modulated packet waveforms while
// holding PTT for the complete burst. This avoids keying the radio once per
// SSDV/PCSI packet. The output device may emit a short silence between chunks,
// while RX capture remains active for the entire transmission.
func (d *Duplex) TransmitPackets(ctx context.Context, packets [][]float32, onProgress func(float64)) (retErr error) {
	if d == nil {
		return ErrClosed
	}
	if len(packets) == 0 {
		return errors.New("TX packet sequence is empty")
	}
	totalSamples := 0
	for i, packet := range packets {
		if len(packet) == 0 {
			return fmt.Errorf("TX packet waveform %d is empty", i)
		}
		totalSamples += len(packet)
	}
	if !d.txMu.TryLock() {
		return ErrTXBusy
	}
	defer d.txMu.Unlock()
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return ErrClosed
	}
	txCtx, cancel := context.WithTimeout(context.Background(), time.Duration(totalSamples)*time.Second/time.Duration(d.sampleRate)+30*time.Second)
	d.txCancel = cancel
	d.txActive = true
	d.mu.Unlock()
	defer func() { cancel(); d.mu.Lock(); d.txActive = false; d.txCancel = nil; d.mu.Unlock() }()
	if ctx != nil {
		go func() {
			select {
			case <-ctx.Done():
				cancel()
			case <-txCtx.Done():
			}
		}()
	}
	if err := d.ptt.SetPTT(txCtx, true); err != nil {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 3*time.Second)
		releaseErr := d.ptt.SetPTT(releaseCtx, false)
		releaseCancel()
		if releaseErr != nil {
			return errors.Join(fmt.Errorf("assert PTT: %w", err), fmt.Errorf("emergency PTT release: %w", releaseErr))
		}
		return fmt.Errorf("assert PTT: %w", err)
	}
	keyed := true
	defer func() {
		if keyed {
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer releaseCancel()
			if e := d.ptt.SetPTT(releaseCtx, false); e != nil {
				retErr = errors.Join(retErr, fmt.Errorf("release PTT: %w", e))
			}
		}
	}()
	if policy, ok := d.ptt.(interface{ RequiresPTTLeadTime() bool }); !ok || policy.RequiresPTTLeadTime() {
		if err := wait(txCtx, d.lead); err != nil {
			return err
		}
	}
	completed := 0
	for index, samples := range packets {
		chunkProgress := func(progress float64) {
			if onProgress == nil {
				return
			}
			fraction := float64(completed) / float64(totalSamples)
			fraction += progress * float64(len(samples)) / float64(totalSamples)
			onProgress(fraction)
		}
		if p, ok := d.playback.(interface {
			PlayWithProgress(context.Context, []float32, int, func(float64)) error
		}); ok {
			retErr = p.PlayWithProgress(txCtx, samples, d.sampleRate, chunkProgress)
		} else {
			retErr = d.playback.Play(txCtx, samples, d.sampleRate)
			if retErr == nil {
				chunkProgress(1)
			}
		}
		if retErr != nil {
			return fmt.Errorf("play TX packet %d/%d: %w", index+1, len(packets), retErr)
		}
		completed += len(samples)
		if onProgress != nil {
			onProgress(float64(completed) / float64(totalSamples))
		}
	}
	if err := wait(txCtx, d.tail); err != nil {
		return err
	}
	releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 3*time.Second)
	err := d.ptt.SetPTT(releaseCtx, false)
	releaseCancel()
	if err != nil {
		return fmt.Errorf("release PTT: %w", err)
	}
	keyed = false
	return nil
}

// TransmitStream holds PTT for one continuous stream while the waveform is
// generated incrementally and played through a bounded PCM queue. estimatedSamples
// is used for the TX timeout and progress reporting; it must be positive.
func (d *Duplex) TransmitStream(ctx context.Context, estimatedSamples int64, producer StreamProducer, onProgress func(float64)) error {
	return d.TransmitStreamWithStart(ctx, estimatedSamples, producer, nil, onProgress)
}

// TransmitStreamWithStart is TransmitStream with a callback invoked immediately
// before PCM playback starts (after stream prefill, PTT assertion, and lead time).
// It lets callers measure actual on-air audio elapsed time rather than counting
// preparation and PTT lead time as transmitted audio.
func (d *Duplex) TransmitStreamWithStart(ctx context.Context, estimatedSamples int64, producer StreamProducer, onPlaybackStart func(), onProgress func(float64)) (retErr error) {
	if d == nil {
		return ErrClosed
	}
	if estimatedSamples <= 0 || producer == nil {
		return errors.New("positive estimated sample count and stream producer are required")
	}
	playback, ok := d.playback.(StreamingPlaybackStream)
	if !ok {
		return errors.New("audio playback backend does not support bounded streaming")
	}
	if !d.txMu.TryLock() {
		return ErrTXBusy
	}
	defer d.txMu.Unlock()
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return ErrClosed
	}
	rate := int64(d.sampleRate)
	// Bound the duration before converting to time.Duration so a malformed
	// estimate cannot overflow the timeout calculation. The limit is shared by
	// all streaming transmissions; GIF bursts have no separate 10-minute cap.
	const maxStreamSeconds = int64(24 * 60 * 60)
	if estimatedSamples > rate*maxStreamSeconds {
		d.mu.Unlock()
		return errors.New("stream TX duration exceeds the 24-hour safety limit")
	}
	seconds, remainder := estimatedSamples/rate, estimatedSamples%rate
	timeout := time.Duration(seconds)*time.Second + time.Duration(remainder)*time.Second/time.Duration(rate) + 30*time.Second
	txCtx, cancel := context.WithTimeout(context.Background(), timeout)
	d.txCancel = cancel
	d.txActive = true
	d.mu.Unlock()
	defer func() {
		cancel()
		d.mu.Lock()
		d.txActive, d.txCancel = false, nil
		d.mu.Unlock()
	}()
	if ctx != nil {
		go func() {
			select {
			case <-ctx.Done():
				cancel()
			case <-txCtx.Done():
			}
		}()
	}
	// Prepare and prefill PCM before keying the radio. This keeps CPU-heavy
	// modulation out of the PTT lead interval and avoids a keyed silent start.
	session, err := playback.PrepareStream(txCtx, d.sampleRate, estimatedSamples, producer, onProgress)
	if err != nil {
		return fmt.Errorf("prepare stream TX audio: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, session.Stop()) }()
	if err := d.ptt.SetPTT(txCtx, true); err != nil {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 3*time.Second)
		releaseErr := d.ptt.SetPTT(releaseCtx, false)
		releaseCancel()
		if releaseErr != nil {
			return errors.Join(fmt.Errorf("assert PTT: %w", err), fmt.Errorf("emergency PTT release: %w", releaseErr))
		}
		return fmt.Errorf("assert PTT: %w", err)
	}
	keyed := true
	defer func() {
		if keyed {
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer releaseCancel()
			if err := d.ptt.SetPTT(releaseCtx, false); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("release PTT: %w", err))
			}
		}
	}()
	if policy, ok := d.ptt.(interface{ RequiresPTTLeadTime() bool }); !ok || policy.RequiresPTTLeadTime() {
		if err := wait(txCtx, d.lead); err != nil {
			return err
		}
	}
	if onPlaybackStart != nil {
		onPlaybackStart()
	}
	if err := session.Play(txCtx); err != nil {
		return fmt.Errorf("stream TX audio: %w", err)
	}
	if err := wait(txCtx, d.tail); err != nil {
		return err
	}
	releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 3*time.Second)
	err = d.ptt.SetPTT(releaseCtx, false)
	releaseCancel()
	if err != nil {
		return fmt.Errorf("release PTT: %w", err)
	}
	keyed = false
	if onProgress != nil {
		onProgress(1)
	}
	return nil
}

func (d *Duplex) StopTX() {
	if d == nil {
		return
	}
	d.mu.Lock()
	cancel := d.txCancel
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (d *Duplex) State() State {
	if d == nil {
		return State{Closed: true}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return State{RXActive: d.rxActive, TXActive: d.txActive, Closed: d.closed}
}
func (d *Duplex) Close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	if d.txCancel != nil {
		d.txCancel()
	}
	d.mu.Unlock()
	rxErr := d.StopRX()
	playErr := d.playback.StopPlayback()
	// Wait for the in-flight TX method to finish its own PTT release before
	// closing the shared keyer. This prevents a shutdown-time SetPTT race.
	d.txMu.Lock()
	defer d.txMu.Unlock()
	releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	releaseErr := d.ptt.SetPTT(releaseCtx, false)
	cancel()
	closeErr := d.ptt.Close()
	return errors.Join(rxErr, playErr, releaseErr, closeErr)
}
func wait(ctx context.Context, d time.Duration) error {
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
