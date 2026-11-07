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
	done     chan struct{}
	samples  []float32
	offset   int
	finished bool
	err      error
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
		m.playbackMu.Lock()
		defer m.playbackMu.Unlock()
		job := m.job
		frames := int(frameCount)
		if frames*4 > len(output) {
			frames = len(output) / 4
		}
		for i := 0; i < frames; i++ {
			var value float32
			if job != nil && !job.finished && job.offset < len(job.samples) {
				value = job.samples[job.offset]
				job.offset++
			}
			binary.LittleEndian.PutUint32(output[i*4:i*4+4], math.Float32bits(value))
		}
		for i := frames * 4; i+4 <= len(output); i += 4 {
			binary.LittleEndian.PutUint32(output[i:i+4], 0)
		}
		if job != nil && !job.finished && job.offset >= len(job.samples) {
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
