//go:build malgo && cgo

package audio

/*
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/gen2brain/malgo"
)

type playbackJob struct {
	done            chan struct{}
	samples         []float32
	offset          int
	streamRing      *float32Ring
	producerCancel  context.CancelFunc
	producerDone    bool
	producerErr     error
	played          int64
	estimated       int64
	playbackStarted bool
	finished        bool
	err             error
}

var ErrPCMUnderrun = errors.New("streaming PCM buffer underrun")

type malgoStreamSession struct {
	runtime      *MalgoRuntime
	job          *playbackJob
	ring         *float32Ring
	producerDone chan struct{}
	cancel       context.CancelFunc
	estimated    int64
	onProgress   func(float64)
	mu           sync.Mutex
	started      bool
	stopped      bool
}

type MalgoRuntime struct {
	ctxMu      sync.RWMutex
	ctx        *malgo.AllocatedContext
	rate       uint32
	captureID  string
	playbackID string
	closed     atomic.Bool

	captureMu     sync.Mutex
	capture       *malgo.Device
	captureCancel context.CancelFunc
	captureDone   chan struct{}
	dropped       atomic.Uint64

	playbackLifeMu sync.Mutex
	playbackMu     sync.Mutex
	playback       *malgo.Device
	job            *playbackJob
}

func NewRuntime(captureDeviceID, playbackDeviceID string, sampleRate int) (Runtime, error) {
	if sampleRate < 8000 || sampleRate > 192000 {
		return nil, fmt.Errorf("sample rate %d is outside 8000..192000 Hz", sampleRate)
	}
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("initialize miniaudio: %w", err)
	}
	return &MalgoRuntime{ctx: ctx, rate: uint32(sampleRate), captureID: captureDeviceID, playbackID: playbackDeviceID}, nil
}

func (m *MalgoRuntime) DeviceLists() (inputs, outputs []DeviceInfo, err error) {
	if m == nil || m.closed.Load() {
		return nil, nil, errors.New("audio runtime is closed")
	}
	m.ctxMu.RLock()
	defer m.ctxMu.RUnlock()
	if m.ctx == nil {
		return nil, nil, errors.New("audio runtime is closed")
	}
	inputs, err = listDevices(m.ctx.Context, malgo.Capture)
	if err != nil {
		return nil, nil, fmt.Errorf("enumerate audio inputs: %w", err)
	}
	outputs, err = listDevices(m.ctx.Context, malgo.Playback)
	if err != nil {
		return nil, nil, fmt.Errorf("enumerate audio outputs: %w", err)
	}
	return inputs, outputs, nil
}

func listDevices(ctx malgo.Context, kind malgo.DeviceType) ([]DeviceInfo, error) {
	infos, err := ctx.Devices(kind)
	if err != nil {
		return nil, err
	}
	out := make([]DeviceInfo, 0, len(infos))
	for i := range infos {
		out = append(out, DeviceInfo{ID: infos[i].ID.String(), Name: infos[i].Name(), IsDefault: infos[i].IsDefault != 0})
	}
	return out, nil
}

func (m *MalgoRuntime) deviceID(kind malgo.DeviceType, wanted string) (unsafe.Pointer, error) {
	if wanted == "" {
		return nil, nil
	}
	infos, err := m.ctx.Context.Devices(kind)
	if err != nil {
		return nil, err
	}
	for i := range infos {
		if infos[i].ID.String() == wanted {
			return infos[i].ID.Pointer(), nil
		}
	}
	return nil, fmt.Errorf("selected audio device %q is not available", wanted)
}

func (m *MalgoRuntime) StartCapture(ctx context.Context, callback func([]float32)) error {
	if m == nil || m.closed.Load() {
		return errors.New("audio runtime is closed")
	}
	if callback == nil {
		return errors.New("capture callback is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.ctxMu.RLock()
	defer m.ctxMu.RUnlock()
	if m.ctx == nil || m.closed.Load() {
		return errors.New("audio runtime is closed")
	}
	m.captureMu.Lock()
	defer m.captureMu.Unlock()
	if m.capture != nil {
		return errors.New("capture is already running")
	}
	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatF32
	cfg.Capture.Channels = 1
	cfg.Capture.ShareMode = malgo.Shared
	cfg.SampleRate = m.rate
	id, err := m.deviceID(malgo.Capture, m.captureID)
	if err != nil {
		return err
	}
	if id != nil {
		cfg.Capture.DeviceID = id
		defer C.free(id)
	}
	queue := make(chan []float32, 16)
	workerCtx, cancel := context.WithCancel(context.Background())
	dev, err := malgo.InitDevice(m.ctx.Context, cfg, malgo.DeviceCallbacks{Data: func(_, input []byte, _ uint32) {
		if workerCtx.Err() != nil || len(input) < 4 {
			return
		}
		block := make([]float32, len(input)/4)
		for i := range block {
			block[i] = math.Float32frombits(binary.LittleEndian.Uint32(input[i*4 : i*4+4]))
		}
		select {
		case queue <- block:
		default:
			select {
			case <-queue:
			default:
			}
			select {
			case queue <- nil:
			default:
			}
			select {
			case queue <- block:
			default:
				m.dropped.Add(1)
			}
		}
	}})
	if err != nil {
		cancel()
		return fmt.Errorf("initialize audio capture: %w", err)
	}
	if err = dev.Start(); err != nil {
		dev.Uninit()
		cancel()
		return fmt.Errorf("start audio capture: %w", err)
	}
	done := make(chan struct{})
	m.capture, m.captureCancel, m.captureDone = dev, cancel, done
	go func() {
		defer close(done)
		for {
			select {
			case <-workerCtx.Done():
				return
			case block := <-queue:
				callback(block)
			}
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = m.StopCapture()
		case <-workerCtx.Done():
		}
	}()
	return nil
}

func (m *MalgoRuntime) StopCapture() error {
	if m == nil {
		return nil
	}
	m.captureMu.Lock()
	dev, cancel, done := m.capture, m.captureCancel, m.captureDone
	m.capture, m.captureCancel, m.captureDone = nil, nil, nil
	if cancel != nil {
		cancel()
	}
	m.captureMu.Unlock()
	var err error
	if dev != nil {
		err = dev.Stop()
		dev.Uninit()
	}
	if done != nil {
		<-done
	}
	return err
}

func (m *MalgoRuntime) Play(ctx context.Context, samples []float32, sampleRate int) error {
	return m.PlayWithProgress(ctx, samples, sampleRate, nil)
}

func (m *MalgoRuntime) PlayWithProgress(ctx context.Context, samples []float32, sampleRate int, onProgress func(float64)) error {
	if m == nil || m.closed.Load() {
		return errors.New("audio runtime is closed")
	}
	if len(samples) == 0 {
		return errors.New("playback waveform is empty")
	}
	if sampleRate != int(m.rate) {
		return fmt.Errorf("waveform sample rate %d does not match device stream %d", sampleRate, m.rate)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.ctxMu.RLock()
	if m.ctx == nil || m.closed.Load() {
		m.ctxMu.RUnlock()
		return errors.New("audio runtime is closed")
	}
	m.playbackLifeMu.Lock()
	m.playbackMu.Lock()
	if m.job != nil {
		m.playbackMu.Unlock()
		m.playbackLifeMu.Unlock()
		m.ctxMu.RUnlock()
		return ErrTXBusy
	}
	if m.playback == nil {
		dev, err := m.newPlaybackLocked()
		if err != nil {
			m.playbackMu.Unlock()
			m.playbackLifeMu.Unlock()
			m.ctxMu.RUnlock()
			return err
		}
		m.playback = dev
	}
	// PlayWithProgress is synchronous: the caller keeps samples alive and must not
	// mutate them until this method returns. The playback callback only reads the
	// slice, so borrowing it avoids a second full-duration waveform allocation.
	job := &playbackJob{done: make(chan struct{}), samples: samples}
	m.job = job
	dev := m.playback
	m.playbackMu.Unlock()
	if !dev.IsStarted() {
		if err := dev.Start(); err != nil {
			m.finishJob(job, err)
			m.playbackLifeMu.Unlock()
			m.ctxMu.RUnlock()
			return fmt.Errorf("start audio playback: %w", err)
		}
	}
	m.playbackLifeMu.Unlock()
	m.ctxMu.RUnlock()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-job.done:
			m.playbackMu.Lock()
			err := job.err
			offset := job.offset
			m.playbackMu.Unlock()
			if onProgress != nil && err == nil {
				onProgress(1)
			} else if onProgress != nil && len(samples) > 0 {
				onProgress(float64(offset) / float64(len(samples)))
			}
			return err
		case <-ticker.C:
			if onProgress != nil {
				m.playbackMu.Lock()
				offset := job.offset
				m.playbackMu.Unlock()
				onProgress(math.Max(0, math.Min(1, float64(offset)/float64(len(samples)))))
			}
		case <-ctx.Done():
			m.finishJob(job, ctx.Err())
			return ctx.Err()
		}
	}
}

// PrepareStream produces and pre-fills a bounded PCM queue without starting the
// output device. Duplex can therefore assert PTT only after the first audio
// block is ready, then apply the configured lead time before playback begins.
func (m *MalgoRuntime) PrepareStream(ctx context.Context, sampleRate int, estimatedSamples int64, producer StreamProducer, onProgress func(float64)) (StreamingPlaybackSession, error) {
	if m == nil || m.closed.Load() {
		return nil, errors.New("audio runtime is closed")
	}
	if sampleRate != int(m.rate) {
		return nil, fmt.Errorf("stream sample rate %d does not match device stream %d", sampleRate, m.rate)
	}
	if estimatedSamples <= 0 || producer == nil {
		return nil, errors.New("positive estimated sample count and PCM producer are required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ringCapacity := max(sampleRate*2, 8192) // two seconds of bounded mono float32 PCM
	ring, err := newFloat32Ring(ringCapacity)
	if err != nil {
		return nil, err
	}
	producerCtx, producerCancel := context.WithCancel(ctx)
	producerDone := make(chan struct{})

	m.ctxMu.RLock()
	if m.ctx == nil || m.closed.Load() {
		m.ctxMu.RUnlock()
		producerCancel()
		return nil, errors.New("audio runtime is closed")
	}
	m.playbackLifeMu.Lock()
	m.playbackMu.Lock()
	if m.job != nil {
		m.playbackMu.Unlock()
		m.playbackLifeMu.Unlock()
		m.ctxMu.RUnlock()
		producerCancel()
		return nil, ErrTXBusy
	}
	job := &playbackJob{
		done: make(chan struct{}), streamRing: ring, producerCancel: producerCancel,
		estimated: estimatedSamples,
	}
	m.job = job
	m.playbackMu.Unlock()
	m.playbackLifeMu.Unlock()
	m.ctxMu.RUnlock()

	go func() {
		emit := func(block []float32) error { return ring.Write(producerCtx, block) }
		producerErr := producer(producerCtx, emit)
		m.playbackMu.Lock()
		if m.job == job && !job.finished {
			job.producerErr = producerErr
			job.producerDone = true
		}
		m.playbackMu.Unlock()
		close(producerDone)
	}()

	session := &malgoStreamSession{
		runtime: m, job: job, ring: ring, producerDone: producerDone,
		cancel: producerCancel, estimated: estimatedSamples, onProgress: onProgress,
	}
	abort := func(cause error) (StreamingPlaybackSession, error) {
		producerCancel()
		m.finishJob(job, cause)
		<-producerDone
		ring.Clear()
		return nil, cause
	}

	// Keep at least 250 ms queued before PTT is asserted. Short bursts that
	// finish sooner are accepted as soon as their producer has completed.
	prefill := max(sampleRate/4, 1)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		m.playbackMu.Lock()
		done, finished, producerErr := job.producerDone, job.finished, job.producerErr
		jobErr := job.err
		m.playbackMu.Unlock()
		if finished {
			if jobErr == nil {
				jobErr = context.Canceled
			}
			return abort(jobErr)
		}
		if done {
			if producerErr != nil {
				return abort(fmt.Errorf("produce PCM stream: %w", producerErr))
			}
			if ring.Len() == 0 {
				return abort(errors.New("PCM producer completed without audio samples"))
			}
			return session, nil
		}
		if ring.Len() >= prefill {
			return session, nil
		}
		select {
		case <-ctx.Done():
			return abort(ctx.Err())
		case <-job.done:
			continue
		case <-ticker.C:
		}
	}
}

// PlayStream is retained as a convenience wrapper for callers that do not
// need to coordinate PTT. The Duplex controller uses PrepareStream directly.
func (m *MalgoRuntime) PlayStream(ctx context.Context, sampleRate int, estimatedSamples int64, producer StreamProducer, onProgress func(float64)) error {
	session, err := m.PrepareStream(ctx, sampleRate, estimatedSamples, producer, onProgress)
	if err != nil {
		return err
	}
	defer session.Stop()
	return session.Play(ctx)
}

func (s *malgoStreamSession) Play(ctx context.Context) error {
	if s == nil || s.runtime == nil {
		return errors.New("stream playback session is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return context.Canceled
	}
	if s.started {
		s.mu.Unlock()
		return errors.New("stream playback session has already started")
	}
	s.started = true
	s.mu.Unlock()

	m := s.runtime
	m.ctxMu.RLock()
	if m.ctx == nil || m.closed.Load() {
		m.ctxMu.RUnlock()
		_ = s.Stop()
		return errors.New("audio runtime is closed")
	}
	m.playbackLifeMu.Lock()
	m.playbackMu.Lock()
	if m.job != s.job || s.job.finished {
		m.playbackMu.Unlock()
		m.playbackLifeMu.Unlock()
		m.ctxMu.RUnlock()
		_ = s.Stop()
		return context.Canceled
	}
	if m.playback == nil {
		dev, err := m.newPlaybackLocked()
		if err != nil {
			m.playbackMu.Unlock()
			m.playbackLifeMu.Unlock()
			m.ctxMu.RUnlock()
			_ = s.Stop()
			return err
		}
		m.playback = dev
	}
	dev := m.playback
	// A previous burst may have left the shared device running. Keep this job
	// gated until Play is called after PTT assertion and lead time have elapsed.
	s.job.playbackStarted = true
	m.playbackMu.Unlock()
	if !dev.IsStarted() {
		if err := dev.Start(); err != nil {
			m.playbackLifeMu.Unlock()
			m.ctxMu.RUnlock()
			_ = s.Stop()
			return fmt.Errorf("start audio playback: %w", err)
		}
	}
	m.playbackLifeMu.Unlock()
	m.ctxMu.RUnlock()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.job.done:
			<-s.producerDone
			m.playbackMu.Lock()
			playErr, played := s.job.err, s.job.played
			m.playbackMu.Unlock()
			s.ring.Clear()
			if playErr == nil && played != s.estimated {
				playErr = fmt.Errorf("PCM stream played %d samples, expected %d", played, s.estimated)
			}
			if s.onProgress != nil {
				if playErr == nil {
					s.onProgress(1)
				} else {
					s.onProgress(math.Max(0, math.Min(1, float64(played)/float64(s.estimated))))
				}
			}
			return playErr
		case <-ticker.C:
			if s.onProgress != nil {
				m.playbackMu.Lock()
				played := s.job.played
				m.playbackMu.Unlock()
				s.onProgress(math.Max(0, math.Min(0.999, float64(played)/float64(s.estimated))))
			}
		case <-ctx.Done():
			_ = s.Stop()
			return ctx.Err()
		}
	}
}

func (s *malgoStreamSession) Stop() error {
	if s == nil || s.runtime == nil {
		return nil
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	s.mu.Unlock()
	s.cancel()
	s.runtime.finishJob(s.job, context.Canceled)
	<-s.producerDone
	s.ring.Clear()
	return nil
}

func (m *MalgoRuntime) newPlaybackLocked() (*malgo.Device, error) {
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatF32
	cfg.Playback.Channels = 1
	cfg.Playback.ShareMode = malgo.Shared
	cfg.SampleRate = m.rate
	id, err := m.deviceID(malgo.Playback, m.playbackID)
	if err != nil {
		return nil, err
	}
	if id != nil {
		cfg.Playback.DeviceID = id
		defer C.free(id)
	}
	dev, err := malgo.InitDevice(m.ctx.Context, cfg, malgo.DeviceCallbacks{Data: func(output, _ []byte, frameCount uint32) {
		clear(output)
		m.playbackMu.Lock()
		defer m.playbackMu.Unlock()
		job := m.job
		frames := int(frameCount)
		if frames*4 > len(output) {
			frames = len(output) / 4
		}
		if job == nil || job.finished {
			return
		}
		if job.streamRing != nil {
			if !job.playbackStarted {
				return
			}
			if job.producerDone && job.producerErr != nil {
				job.finished = true
				job.err = job.producerErr
				job.streamRing = nil
				m.job = nil
				close(job.done)
				return
			}
			read := job.streamRing.ReadToBytes(output[:frames*4])
			job.played += int64(read)
			if read < frames && !job.producerDone {
				// Never insert silent gaps into a modulated burst. Abort immediately
				// and let Duplex release PTT through its normal cleanup path.
				job.finished = true
				job.err = ErrPCMUnderrun
				if job.producerCancel != nil {
					job.producerCancel()
				}
				job.streamRing = nil
				m.job = nil
				close(job.done)
				return
			}
			if job.producerDone && job.streamRing.Len() == 0 {
				job.finished = true
				job.err = job.producerErr
				job.streamRing = nil
				m.job = nil
				close(job.done)
			}
			return
		}
		for i := 0; i < frames; i++ {
			var value float32
			if job.offset < len(job.samples) {
				value = job.samples[job.offset]
				job.offset++
			}
			binary.LittleEndian.PutUint32(output[i*4:i*4+4], math.Float32bits(value))
		}
		if job.offset >= len(job.samples) {
			job.finished = true
			job.samples = nil
			m.job = nil
			close(job.done)
		}
	}})
	if err != nil {
		return nil, fmt.Errorf("initialize audio playback: %w", err)
	}
	return dev, nil
}

func (m *MalgoRuntime) finishJob(job *playbackJob, err error) {
	m.playbackMu.Lock()
	defer m.playbackMu.Unlock()
	if m.job != job || job.finished {
		return
	}
	job.finished = true
	job.err = err
	job.samples = nil
	if job.streamRing != nil {
		job.streamRing.Clear()
		job.streamRing = nil
	}
	if job.producerCancel != nil {
		job.producerCancel()
	}
	m.job = nil
	close(job.done)
}

func (m *MalgoRuntime) StopPlayback() error {
	if m == nil {
		return nil
	}
	m.playbackLifeMu.Lock()
	m.playbackMu.Lock()
	dev, job := m.playback, m.job
	m.playback, m.job = nil, nil
	if job != nil && !job.finished {
		job.finished = true
		job.err = context.Canceled
		job.samples = nil
		if job.streamRing != nil {
			job.streamRing.Clear()
			job.streamRing = nil
		}
		if job.producerCancel != nil {
			job.producerCancel()
		}
		close(job.done)
	}
	m.playbackMu.Unlock()
	m.playbackLifeMu.Unlock()
	if dev == nil {
		return nil
	}
	err := dev.Stop()
	dev.Uninit()
	return err
}

func (m *MalgoRuntime) Close() error {
	if m == nil {
		return nil
	}
	m.ctxMu.Lock()
	if !m.closed.CompareAndSwap(false, true) {
		m.ctxMu.Unlock()
		return nil
	}
	m.ctxMu.Unlock()
	err1 := m.StopCapture()
	err2 := m.StopPlayback()
	m.ctxMu.Lock()
	defer m.ctxMu.Unlock()
	var err3 error
	if m.ctx != nil {
		err3 = m.ctx.Uninit()
		m.ctx.Free()
		m.ctx = nil
	}
	return errors.Join(err1, err2, err3)
}
