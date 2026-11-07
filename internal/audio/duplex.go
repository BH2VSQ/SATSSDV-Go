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
